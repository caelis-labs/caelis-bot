package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type workerSwitchEngine struct {
	*switchEngine
	task     api.Task
	detaches atomic.Int32
	stops    atomic.Int32
}

func (e *workerSwitchEngine) WorkStates() []api.WorkState {
	return []api.WorkState{{Task: e.task, Runtime: e.id}}
}
func (e *workerSwitchEngine) WorkTerminal(context.Context, string) (api.TerminalTarget, error) {
	return api.TerminalTarget{}, nil
}
func (e *workerSwitchEngine) ReadWork(context.Context, string) (api.Task, error) { return e.task, nil }
func (e *workerSwitchEngine) StopWork(context.Context, string) (api.Task, error) {
	e.stops.Add(1)
	return e.task, nil
}
func (e *workerSwitchEngine) DetachForUpdate(context.Context) error { e.detaches.Add(1); return nil }

func TestRuntimeSwitchDoesNotWaitForOrStopWorkersInEitherProvider(t *testing.T) {
	for _, status := range []string{"working", "awaiting_approval", "unknown", "unavailable"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "runtime.json"), []byte(`{"version":1,"runtime":"codex"}`), 0600); err != nil {
				t.Fatal(err)
			}
			var original *workerSwitchEngine
			a, err := newApplication(root, Host{}, func(id string) (providerFactory, error) {
				return providerFactory{ID: id, Open: func(providerConfig) (api.Engine, error) {
					e := &workerSwitchEngine{switchEngine: &switchEngine{testEngine: newTestEngine(), id: id}, task: api.Task{ID: id + "-original-task", Status: status}}
					if original == nil {
						original = e
					}
					return e, nil
				}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if err = a.Start(); err != nil {
				t.Fatal(err)
			}
			// The target already owns active work. It must detach before a new
			// resident reads its ledger, and must never receive ordinary Close.
			target := &workerSwitchEngine{switchEngine: &switchEngine{testEngine: newTestEngine(), id: "caelis"}, task: api.Task{ID: "target-task", Status: status}}
			a.localWork.mu.Lock()
			a.localWork.owners["caelis"] = target
			a.localWork.routes[original.task.ID] = "codex"
			a.localWork.mu.Unlock()
			a.localWork.open = func(id string) (retainedWorker, error) {
				return &workerSwitchEngine{switchEngine: &switchEngine{testEngine: newTestEngine(), id: id}, task: original.task}, nil
			}
			if err = a.activateRuntime(t.Context(), api.RuntimeSettings{Runtime: "caelis"}, false); err != nil {
				t.Fatal(err)
			}
			if original.detaches.Load() != 1 || target.detaches.Load() != 1 || original.closed != 0 || target.closed != 0 || original.stops.Load() != 0 || target.stops.Load() != 0 {
				t.Fatal("switch stopped native work or did not hand off observers")
			}
			if a.localWork.routes[original.task.ID] != "codex" || a.Backend.ProviderInfo().ID != "caelis" {
				t.Fatal("original task moved")
			}
		})
	}
}

type slowDetachWorker struct {
	*workerSwitchEngine
	entered chan struct{}
	release chan struct{}
}

func (w *slowDetachWorker) DetachForUpdate(ctx context.Context) error {
	close(w.entered)
	select {
	case <-w.release:
		return errors.New("disk unavailable")
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestWorkerDetachFailureKeepsOwnerAndDoesNotBlockSettingsRead(t *testing.T) {
	owner := &slowDetachWorker{workerSwitchEngine: &workerSwitchEngine{switchEngine: &switchEngine{testEngine: newTestEngine(), id: "codex"}}, entered: make(chan struct{}), release: make(chan struct{})}
	pool := &localWorkers{active: "caelis", owners: map[string]retainedWorker{"codex": owner}}
	done := make(chan error, 1)
	go func() { done <- pool.RetireResident(t.Context(), "codex") }()
	<-owner.entered
	read := make(chan struct{})
	go func() { pool.RetainedApprovals(); close(read) }()
	select {
	case <-read:
	case <-time.After(time.Second):
		t.Fatal("observer detach blocked UI snapshot")
	}
	close(owner.release)
	if <-done == nil {
		t.Fatal("lost detach failure")
	}
	if pool.owners["codex"] != owner || !pool.retiring["codex"] {
		t.Fatal("failed detach lost original owner")
	}
}
