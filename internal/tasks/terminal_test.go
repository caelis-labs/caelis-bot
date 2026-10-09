package tasks

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type terminalFixture struct {
	*fixtureRuntime
	opened string
}

func (f *terminalFixture) WorkTerminal(_ context.Context, id string) (api.TerminalTarget, error) {
	f.opened = id
	return api.TerminalTarget{Thread: "native-owned"}, nil
}

func TestCurrentUnknownProjectionBlocksContinuationBeforeOwnerDispatch(t *testing.T) {
	f := &terminalFixture{fixtureRuntime: newRuntime()}
	m := openFixture(t, t.TempDir(), "codex", f.fixtureRuntime)
	v := start(t, m, "projected-unknown")
	state := f.states[v.ID]
	state.Task.Status = "unknown"
	f.states[v.ID] = state
	if _, err := m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "unknown-followup", Prompt: "continue"}); err == nil {
		t.Fatal("unknown projected work was sent")
	}
	if got := f.states[v.ID].Task.Status; got != "unknown" {
		t.Fatal("owner received unknown continuation", got)
	}
	if len(m.state.Records[v.ID].Requests) != 1 {
		t.Fatal("unknown followup acquired a product receipt")
	}
	// WorkTerminal uses the same product projection even with a permissive
	// terminal provider. Reopen against that provider to keep the fixture local.
	root := t.TempDir()
	tm, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	tv := start(t, tm, "terminal-unknown")
	ts := f.states[tv.ID]
	ts.Task.Status = "unknown"
	f.states[tv.ID] = ts
	if err := tm.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	if _, err := tm.WorkTerminal(t.Context(), tv.ID); err == nil || f.opened != "" {
		t.Fatal("unknown projected work acquired a terminal", err)
	}
}

func TestTaskPreviewPersistsOriginalPromptAndFencesOwnership(t *testing.T) {
	f := &terminalFixture{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	path := filepath.Join(root, "tasks.json")
	m, err := Open(path, filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	in := input("original-task-prompt")
	in.Prompt = "Original prompt\nwith exact whitespace and <markup>"
	v, err := m.StartTask(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.TaskPreviews()) != 1 {
		t.Fatal("new task missing automatic pin")
	}
	if _, err = m.PinTask(v.ID, true); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	p := reopened.TaskPreviews()
	if len(p) != 1 || p[0].ID != v.ID || p[0].Prompt != in.Prompt {
		t.Fatal("original assignment changed", p)
	}
	if p[0].Status != "working" {
		t.Fatal("active preview lost native status", p)
	}
	f.complete(v.ID)
	if p = reopened.TaskPreviews(); p[0].Status != "completed" {
		t.Fatal("preview waited for ledger refresh", p)
	}
	if _, err = reopened.WorkTerminal(t.Context(), "foreign"); err == nil || f.opened != "" {
		t.Fatal("foreign task acquired")
	}
	if target, err := reopened.WorkTerminal(t.Context(), v.ID); err != nil || target.Thread != "native-owned" || f.opened != v.ID {
		t.Fatal("owned task unavailable", err)
	}
	other, err := Open(path, filepath.Join(root, "Tasks"), "other", newRuntime(), f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.TaskPreviews()) != 0 {
		t.Fatal("unsupported runtime exposed bubbles")
	}
}
