package tasks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func dockManager(t *testing.T) (*Manager, *terminalFixture) {
	t.Helper()
	f := &terminalFixture{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return m, f
}

func TestFailedDockLeavesDefaultWatchlistWithoutLosingHistoryOrReceipt(t *testing.T) {
	for _, status := range []string{"failed", "rejected"} {
		t.Run(status, func(t *testing.T) {
			m, f := dockManager(t)
			v := start(t, m, "will-fail-on-native")
			requests := append([]string(nil), m.state.Records[v.ID].Requests...)
			s := f.states[v.ID]
			s.Task.Status, s.Task.Outcome, s.Task.Result = status, "rejected", "Synthetic retained failure reason"
			s.ExecutionKey = "native-failed-new-generation"
			f.states[v.ID] = s
			if len(m.TaskPreviews()) != 0 {
				t.Fatal("live failed projection briefly repinned a new generation")
			}
			for range 3 {
				if err := m.RefreshWatchlist(); err != nil {
					t.Fatal(err)
				}
			}
			if len(m.TaskPreviews()) != 0 || *m.state.Records[v.ID].Pinned {
				t.Fatal("poll revived default failure")
			}
			reopened, err := Open(m.path, m.root, "codex", f, f, f.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if len(reopened.TaskPreviews()) != 0 {
				t.Fatal("restart revived failure")
			}
			page, err := reopened.QueryTasks(api.TaskQuery{Status: status})
			if err != nil || page.Total != 1 || page.Tasks[0].ID != v.ID {
				t.Fatal(page, err)
			}
			read, err := reopened.ReadTask(t.Context(), v.ID)
			if err != nil || read.Result != s.Task.Result || read.Status != status {
				t.Fatal("history altered", read, err)
			}
			r := reopened.state.Records[v.ID]
			if len(r.Requests) != len(requests) || r.Requests[0] != requests[0] || len(r.Operations) != 1 || f.starts != 1 || f.states[v.ID].StopRequested {
				t.Fatal("visibility changed execution/receipts")
			}
		})
	}
}

func TestWorkspaceRejectedWithoutNativeBindingDoesNotOccupyDock(t *testing.T) {
	m, f := dockManager(t)
	if err := os.WriteFile(m.root, []byte("not a task directory"), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := m.StartTask(t.Context(), input("rejected-before-native"))
	if err == nil || v.Status != "failed" || v.Outcome != "rejected" {
		t.Fatal(v, err)
	}
	if len(m.TaskPreviews()) != 0 || f.starts != 0 || len(f.states) != 0 || f.opened != "" {
		t.Fatal("no-native failure left a dock card")
	}
	r := m.state.Records[v.ID]
	if r == nil || len(r.Operations) != 1 || r.Execution != "" {
		t.Fatal("original no-native receipt lost")
	}
	if _, err = Open(m.path, m.root, "codex", f, f, f.Snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitPinAndLockSurviveFailureButAutomaticPinDoesNot(t *testing.T) {
	for _, intent := range []string{"automatic", "pin", "lock"} {
		t.Run(intent, func(t *testing.T) {
			m, f := dockManager(t)
			v := start(t, m, "pin-intent-fixture")
			var err error
			if intent == "pin" {
				_, err = m.PinTask(v.ID, true)
			}
			if intent == "lock" {
				_, err = m.LockTask(v.ID, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			s := f.states[v.ID]
			s.Task.Status, s.Task.Outcome = "failed", "rejected"
			f.states[v.ID] = s
			if err = m.RefreshWatchlist(); err != nil {
				t.Fatal(err)
			}
			want := 1
			if intent == "automatic" {
				want = 0
			}
			reopened, err := Open(m.path, m.root, "codex", f, f, f.Snapshot)
			if err != nil || len(reopened.TaskPreviews()) != want {
				t.Fatal("explicit intent not retained", err)
			}
			if intent == "lock" {
				if _, err = reopened.LockTask(v.ID, false); err != nil {
					t.Fatal(err)
				}
			}
			if err = reopened.ClearTasks(); err != nil {
				t.Fatal(err)
			}
			if err = reopened.RefreshWatchlist(); err != nil || len(reopened.TaskPreviews()) != 0 {
				t.Fatal("dismissed explicit pin revived", err)
			}
		})
	}
}

func TestUncertainDockReceiptsNeverAgeAsFailure(t *testing.T) {
	for _, values := range [][2]string{{"unknown", "unknown"}, {"unconfirmed", "unknown"}, {"failed", "unknown"}, {"rejected", "unconfirmed"}} {
		t.Run(values[0]+"-"+values[1], func(t *testing.T) {
			m, f := dockManager(t)
			v := start(t, m, "uncertain-original-op")
			s := f.states[v.ID]
			s.Task.Status, s.Task.Outcome = values[0], values[1]
			f.states[v.ID] = s
			if err := m.RefreshWatchlist(); err != nil {
				t.Fatal(err)
			}
			now := time.Now().Add(2 * CompletedRetention)
			m.now = func() time.Time { return now }
			if err := m.RefreshWatchlist(); err != nil || len(m.TaskPreviews()) != 1 {
				t.Fatal("uncertainty expired as failure", err)
			}
		})
	}
}

func TestDefaultDockProjectionEvidence(t *testing.T) {
	m, f := dockManager(t)
	for _, kind := range []string{"working", "completed", "unknown", "failed"} {
		in := input("dock-evidence-" + kind)
		in.Prompt = "Synthetic " + kind + " task — Manager projection, no real account or command."
		v, err := m.StartTask(t.Context(), in)
		if err != nil {
			t.Fatal(err)
		}
		s := f.states[v.ID]
		s.Task.Status = kind
		if kind == "unknown" {
			s.Task.Outcome = "unknown"
		}
		if kind == "failed" {
			s.Task.Outcome = "rejected"
		}
		f.states[v.ID] = s
	}
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	previews := m.TaskPreviews()
	if len(previews) != 3 {
		t.Fatal(previews)
	}
	for _, p := range previews {
		if p.Status == "failed" {
			t.Fatal("default failure visible")
		}
	}
	for _, p := range previews {
		if p.Status == "working" {
			if _, err := m.WorkTerminal(t.Context(), p.ID); err != nil || f.opened != p.ID {
				t.Fatal("running task no longer opens", err)
			}
		}
	}
	if target := os.Getenv("BOT_TASK_PREVIEW_PROJECTION"); target != "" {
		data, err := json.MarshalIndent(previews, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExplicitDockPinPersistenceFailureRestoresIntent(t *testing.T) {
	m, _ := dockManager(t)
	v := start(t, m, "original-pin-intent")
	originalWrite := m.write
	failMutation := func() {
		calls := 0
		m.write = func() error {
			calls++
			if calls == 1 {
				return originalWrite()
			} // preceding safe refresh
			return errors.New("injected persistence failure")
		}
	}
	failMutation()
	if _, err := m.PinTask(v.ID, true); err == nil || m.state.Records[v.ID].ExplicitPin {
		t.Fatal("failed pin published explicit intent", err)
	}
	m.write = originalWrite
	if _, err := m.PinTask(v.ID, true); err != nil {
		t.Fatal(err)
	}
	failMutation()
	if err := m.ClearTasks(); err == nil || !m.state.Records[v.ID].ExplicitPin || !*m.state.Records[v.ID].Pinned {
		t.Fatal("failed clear discarded prior intent", err)
	}
}
