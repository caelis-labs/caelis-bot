package tasks

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"path/filepath"
	"testing"
)

type remoteFixture struct {
	*fixtureRuntime
	prepares int
}

func (f *remoteFixture) PrepareRemoteWork(_ context.Context, _ api.TaskStart, _ string) (string, error) {
	f.prepares++
	return "/remote/not-a-local-directory", nil
}
func (f *remoteFixture) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	v, e := f.fixtureRuntime.StartWork(ctx, in)
	v.Machine = in.Machine
	v.MachineName = "Build machine"
	state := f.states[in.ID]
	state.Task = v
	f.states[in.ID] = state
	return v, e
}
func TestRemoteTaskKeepsMachineAcrossControllerRuntimeChange(t *testing.T) {
	root := t.TempDir()
	f := &remoteFixture{fixtureRuntime: newRuntime()}
	open := func(provider string) *Manager {
		m, e := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), provider, f, f, f.Snapshot)
		if e != nil {
			t.Fatal(e)
		}
		return m
	}
	m := open("codex")
	in := input("same-remote-user-request")
	in.Machine = "machine-owned"
	v, e := m.StartTask(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	if v.Workspace != "/remote/not-a-local-directory" || f.starts != 1 || f.prepares != 1 {
		t.Fatal("evaluated a remote path locally or submitted twice")
	}
	m = open("caelis")
	got, e := m.StartTask(t.Context(), in)
	if e != nil || got.ID != v.ID || f.starts != 1 || f.prepares != 1 {
		t.Fatal("runtime switch changed the original remote request", e)
	}
	got, e = m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "continue-original-node", Prompt: "Continue"})
	if e != nil || got.Machine != in.Machine {
		t.Fatal("lost original target", e)
	}
	// A remote observation cannot adopt an existing local task handle.
	state := f.states[v.ID]
	state.Task.Machine = "machine-foreign"
	f.states[v.ID] = state
	if e = m.refresh(); e == nil {
		t.Fatal("rebound a task to a different machine")
	}
}
