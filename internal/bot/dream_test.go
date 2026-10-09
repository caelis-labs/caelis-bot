package bot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

type legacyDreamEngine struct {
	fakeEngine
	state                    api.ConversationState
	outcome                  string
	renewErr                 error
	calls, cancels, renewals int
}

func (e *legacyDreamEngine) ConversationState() api.ConversationState { return e.state }
func (e *legacyDreamEngine) SubmitDream(context.Context, api.Submission) (api.Receipt, error) {
	e.calls++
	return api.Receipt{Outcome: "accepted"}, nil
}
func (e *legacyDreamEngine) DreamResult(id string) (api.Receipt, api.ConversationState) {
	return api.Receipt{ID: id, Outcome: e.outcome}, e.state
}
func (e *legacyDreamEngine) CancelDream(context.Context, string) error { e.cancels++; return nil }
func (e *legacyDreamEngine) RenewConversation(context.Context, string, string) error {
	e.renewals++
	if e.renewErr != nil {
		return e.renewErr
	}
	e.state.Session = "new"
	return nil
}

func TestNoAutomaticDreamEvenAfterUpgradeAndHighUsage(t *testing.T) {
	r, _, now := fixture(t)
	v, err := notebook.OpenVault(filepath.Join(t.TempDir(), "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := r.ConfigureDream(v, ""); err != nil {
		t.Fatal(err)
	}
	e := &legacyDreamEngine{fakeEngine: fakeEngine{outcome: "accepted"}, outcome: "rejected", state: api.ConversationState{Session: "old", Turn: "user-turn", Status: "completed", Observed: true, Idle: true, RuntimeVersion: "old", DesiredRuntimeVersion: "new", Usage: api.ContextUsage{Used: 95000, Window: 100000, ModelAt: *now}}}
	r.engine = e
	for range 1000 {
		*now = now.Add(time.Second)
		if err := r.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if e.calls != 0 || e.renewals != 0 {
		t.Fatalf("host dispatched Dream or upgrade renewal: %+v", e)
	}
}

func TestLegacyUnknownReceiptNeverReplays(t *testing.T) {
	r, _, _ := fixture(t)
	v, err := notebook.OpenVault(filepath.Join(t.TempDir(), "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := r.ConfigureDream(v, ""); err != nil {
		t.Fatal(err)
	}
	e := &legacyDreamEngine{fakeEngine: fakeEngine{outcome: "accepted"}, outcome: "unknown", state: api.ConversationState{Session: "old", Turn: "user-turn", Status: "completed", Observed: true, Idle: true}}
	r.engine = e
	r.dream.state.Attempt = &dreamAttempt{ID: "old-call", Session: "old", Outcome: "unknown"}
	if err := r.dream.save(); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		_ = r.Tick(t.Context())
	}
	if e.calls != 0 || e.renewals != 0 {
		t.Fatal("replayed unknown maintenance receipt")
	}
	r2, err := New(r.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r2.ConfigureDream(v, ""); err != nil {
		t.Fatal(err)
	}
	if r2.dream.state.Attempt == nil || r2.dream.state.Attempt.ID != "old-call" {
		t.Fatal("original receipt lost")
	}
	if _, err := os.Stat(r.dream.path); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyCompletedHandoffMigratesPrivatelyWithoutDeletingNotebook(t *testing.T) {
	r, _, _ := fixture(t)
	v, err := notebook.OpenVault(filepath.Join(t.TempDir(), "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if err := r.ConfigureDream(v, ""); err != nil {
		t.Fatal(err)
	}
	path, err := v.PrepareDream()
	if err != nil {
		t.Fatal(err)
	}
	legacy := notebook.DreamMarker("old-call") + "\nIdentity, pending task handle, original uncertain receipt."
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	e := &legacyDreamEngine{fakeEngine: fakeEngine{outcome: "accepted"}, outcome: "accepted", state: api.ConversationState{Session: "old", Turn: "dream-turn", Status: "completed", Observed: true, Idle: true}}
	r.engine = e
	r.dream.state.Attempt = &dreamAttempt{ID: "old-call", Session: "old", Turn: "dream-turn", Outcome: "accepted", Done: true, Ready: true}
	if err := r.dream.save(); err != nil {
		t.Fatal(err)
	}
	receipt, err := r.SubmitUser(t.Context(), api.Submission{ID: "user-next", Text: "continue"}, nil)
	if err != nil || receipt.Outcome != "accepted" || e.renewals != 1 || len(e.submissions) != 1 {
		t.Fatal(receipt, err, e.renewals, e.submissions)
	}
	seed := r.PrepareHandoffContext()
	if !strings.Contains(seed.Text, "original uncertain receipt") || seed.HandoffDigest == "" {
		t.Fatal("private handoff missing", seed)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != legacy {
		t.Fatal("user Notebook handoff changed", err)
	}
}
