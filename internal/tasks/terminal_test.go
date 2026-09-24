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
	reopened, err := Open(path, filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	p := reopened.TaskPreviews()
	if len(p) != 1 || p[0].ID != v.ID || p[0].Prompt != in.Prompt {
		t.Fatal("original assignment changed", p)
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
