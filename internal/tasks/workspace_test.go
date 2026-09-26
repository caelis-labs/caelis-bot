package tasks

import (
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectedWorkspaceAndAutomaticPins(t *testing.T) {
	for _, provider := range []string{"codex", "caelis"} {
		t.Run(provider, func(t *testing.T) {
			f := &terminalFixture{fixtureRuntime: newRuntime()}
			root := t.TempDir()
			m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), provider, f, f, f.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(root, "existing-project")
			if err = os.Mkdir(selected, 0755); err != nil {
				t.Fatal(err)
			}
			_ = os.WriteFile(filepath.Join(selected, "sentinel"), []byte("keep"), 0644)
			canonical, _ := filepath.EvalSymlinks(selected)
			changes := 0
			m.ObserveWatchlist(func(v []api.TaskPreview) {
				changes++
				if len(v) > 8 {
					t.Fatal("pin overflow")
				}
			})
			in := input("selected-project")
			in.Workspace = selected
			v, err := m.StartTask(t.Context(), in)
			if err != nil || v.Workspace != canonical || f.lastStart.Workspace != canonical || len(m.TaskPreviews()) != 1 || changes != 1 {
				t.Fatal(v, err, changes)
			}
			info, _ := os.Stat(selected)
			if info.Mode().Perm() != 0755 {
				t.Fatal("changed project permissions")
			}
			if b, _ := os.ReadFile(filepath.Join(selected, "sentinel")); string(b) != "keep" {
				t.Fatal("changed project contents")
			}
			if _, err = os.Stat(m.root); !os.IsNotExist(err) {
				t.Fatal("allocated unnecessary private workspace")
			}
			if _, err = m.PinTask(v.ID, false); err != nil {
				t.Fatal(err)
			}
			reopened := openFixture(t, root, provider, f.fixtureRuntime)
			if _, err = reopened.StartTask(t.Context(), in); err != nil || f.starts != 1 {
				t.Fatal("retry redispatched", err)
			}
			page, _ := reopened.QueryTasks(api.TaskQuery{})
			if page.Tasks[0].Pinned {
				t.Fatal("retry repinned a manually removed task")
			}
			in.Workspace = t.TempDir()
			if _, err = m.StartTask(t.Context(), in); err == nil {
				t.Fatal("request retargeted another workspace")
			}
			f.complete(v.ID)
			for i := 0; i < 9; i++ {
				next := start(t, m, fmt.Sprintf("auto-pin-%d", i))
				f.complete(next.ID)
			}
			if len(m.TaskPreviews()) != 8 {
				t.Fatal("auto pin capacity not respected")
			}
			for _, bad := range []string{"relative", filepath.Join(root, "missing"), filepath.Join(selected, "sentinel")} {
				req := input("bad-workspace-" + hash(bad))
				req.Workspace = bad
				before := f.starts
				if _, err = m.StartTask(t.Context(), req); err == nil || f.starts != before {
					t.Fatal("invalid workspace dispatched", bad, err)
				}
			}
		})
	}
}
