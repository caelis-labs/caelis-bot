package bot

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestIntroductionIsOneDurableOrdinaryUserMessage(t *testing.T) {
	for _, desc := range []string{"", "简洁、温和"} {
		t.Run(desc, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "intro.json")
			i, err := OpenInitializer(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = i.Initialize(t.Context(), api.BotIntroduction{Description: desc}); err == nil {
				t.Fatal("missing name accepted")
			}
			if _, err = i.Initialize(t.Context(), api.BotIntroduction{Name: " 星星 ", Description: desc}); err != nil {
				t.Fatal(err)
			}
			f := &fakeEngine{outcome: "accepted"}
			if err = i.Deliver(t.Context(), f, "codex"); err != nil || len(f.submissions) != 0 {
				t.Fatal("offline delivery", err)
			}
			i, err = OpenInitializer(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = i.Initialize(t.Context(), api.BotIntroduction{Name: "must not replace"})
			if err != nil {
				t.Fatal(err)
			}
			f.view.CanSend = true
			var wg sync.WaitGroup
			for range 4 {
				wg.Go(func() {
					if err := i.Deliver(t.Context(), f, "codex"); err != nil {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			want := "你的名字是星星。"
			if desc != "" {
				want += "\n描述是" + desc
			}
			if len(f.submissions) != 1 || f.submissions[0].Text != want {
				t.Fatal("changed or duplicated introduction", f.submissions)
			}
			b, _ := os.ReadFile(path)
			if strings.Contains(string(b), "星星") || strings.Contains(string(b), "description") {
				t.Fatal("second identity authority retained")
			}
			i, err = OpenInitializer(path)
			if err != nil {
				t.Fatal(err)
			}
			if i.Initialization().Required || i.Initialization().Status != "accepted" {
				t.Fatal("initialization repeated")
			}
			if err = i.Deliver(t.Context(), f, "caelis"); err != nil || len(f.submissions) != 1 {
				t.Fatal("provider switch resent introduction")
			}
		})
	}
}
func TestUncertainIntroductionReconcilesWithoutReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intro.json")
	i, _ := OpenInitializer(path)
	i.Initialize(t.Context(), api.BotIntroduction{Name: "Test"})
	f := &fakeEngine{view: api.Snapshot{CanSend: true}, outcome: "unknown"}
	if err := i.Deliver(t.Context(), f, "codex"); err != nil {
		t.Fatal(err)
	}
	if _, err := i.RetryInitialization(t.Context()); err == nil {
		t.Fatal("unknown outcome retried")
	}
	id := f.submissions[0].ID
	i, err := OpenInitializer(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = i.GuardRuntimeChange(); err == nil {
		t.Fatal("uncertainty permitted switch")
	}
	f.outcome = "accepted"
	f.view.LastReceipt = api.Receipt{ID: id, Outcome: "accepted"}
	i.Deliver(t.Context(), f, "caelis")
	if i.Initialization().Status != "unknown" || len(f.submissions) != 1 {
		t.Fatal("foreign receipt or replay")
	}
	i.Deliver(t.Context(), f, "codex")
	if i.Initialization().Status != "accepted" || len(f.submissions) != 1 {
		t.Fatal("reconciliation failed")
	}
}

func TestRejectedIntroductionRequiresExplicitRetryWithFreshID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "intro.json")
	i, _ := OpenInitializer(path)
	i.Initialize(t.Context(), api.BotIntroduction{Name: "Test"})
	f := &fakeEngine{view: api.Snapshot{CanSend: true}, outcome: "rejected"}
	if err := i.Deliver(t.Context(), f, "codex"); err != nil {
		t.Fatal(err)
	}
	i, err := OpenInitializer(path)
	if err != nil {
		t.Fatal(err)
	}
	i.Deliver(t.Context(), f, "codex")
	if i.Initialization().Status != "rejected" || len(f.submissions) != 1 {
		t.Fatal("automatic retry")
	}
	if _, err = i.RetryInitialization(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.outcome = "accepted"
	i.Deliver(t.Context(), f, "codex")
	if len(f.submissions) != 2 || f.submissions[0].ID == f.submissions[1].ID || f.submissions[0].Text != f.submissions[1].Text || i.Initialization().Status != "accepted" {
		t.Fatal("invalid explicit retry")
	}
	if _, err = i.RetryInitialization(t.Context()); err == nil {
		t.Fatal("accepted introduction replay")
	}
}
