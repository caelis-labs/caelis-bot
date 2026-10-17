package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func storeWorkerModel(t *testing.T, root, runtime string, value api.WorkExecutionSettings) {
	t.Helper()
	dir, err := providerDirectory(root, runtime)
	if err == nil {
		err = localstate.Write(filepath.Join(dir, "work-execution.json"), value)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestNewAndLegacyEmptyWorkerDefaultsFollowConversation(t *testing.T) {
	a, _, _ := newSwitchFixture(t, false, nil)
	if got := a.localWork.DefaultRuntime(); got != "conversation" {
		t.Fatal(got)
	}
	if err := a.activateRuntime(t.Context(), api.RuntimeSettings{Runtime: "caelis"}, true); err != nil {
		t.Fatal(err)
	}
	if got := a.localWork.DefaultRuntime(); got != "conversation" {
		t.Fatal(got)
	}
	for _, runtime := range []string{"codex", "caelis"} {
		t.Run(runtime, func(t *testing.T) {
			root := t.TempDir()
			pool := workPool(root, runtime, nil)
			pool.open = func(string) (retainedWorker, error) {
				t.Fatal("empty preference launched a runtime probe")
				return nil, nil
			}
			storeWorkerModel(t, root, runtime, api.WorkExecutionSettings{Effort: "high"})
			selection, err := pool.LocalWorkerSelection()
			if err != nil || selection.Runtime != "conversation" || selection.Work != (api.WorkExecutionSettings{}) || pool.DefaultRuntime() != "conversation" {
				t.Fatal(selection, err)
			}
			pool.defaultRuntime, pool.loadErr = runtime, errors.New("force durable reload")
			pool.reloadSettingsLocked()
			if pool.DefaultRuntime() != "conversation" {
				t.Fatal("fallback was not persisted")
			}
		})
	}
}

type unavailableModelWorker struct {
	*workerFixture
	catalogErr error
}

func (w *unavailableModelWorker) Models(context.Context) ([]api.ModelOption, error) {
	if w.catalogErr != nil {
		return nil, w.catalogErr
	}
	return []api.ModelOption{}, nil
}

func TestUnavailableDefaultFallsBackBeforeBindingWithoutMovingOriginalTask(t *testing.T) {
	for _, runtime := range []string{"codex", "caelis"} {
		for _, local := range []bool{false, true} {
			t.Run(runtime, func(t *testing.T) {
				active := "caelis"
				if runtime == active {
					active = "codex"
				}
				root := t.TempDir()
				resident := &followingWorkerFixture{workerFixture: &workerFixture{runtime: active, states: map[string]api.WorkState{}}, current: api.WorkExecutionSettings{Model: "chat-current", Effort: "high"}}
				old := &unavailableModelWorker{workerFixture: &workerFixture{runtime: runtime, states: map[string]api.WorkState{"old": {Task: api.Task{ID: "old"}}}}}
				pool := workPool(root, active, map[string]*workerFixture{active: resident.workerFixture})
				pool.resident, pool.defaultRuntime = resident, runtime
				pool.owners[runtime], pool.routes["old"] = old, runtime
				storeWorkerModel(t, root, runtime, api.WorkExecutionSettings{Model: "removed-model"})
				if _, err := pool.StartWork(t.Context(), api.WorkStart{TaskStart: api.TaskStart{LocalRuntime: local}, ID: "new"}); err != nil {
					t.Fatal(err)
				}
				if pool.routes["new"] != active || pool.routes["old"] != runtime || old.starts != 0 || resident.starts != 1 || resident.execution == nil || *resident.execution != resident.current {
					t.Fatal("fallback changed an original owner or used stale model", pool.routes, resident.execution)
				}
				if _, err := pool.ReadWork(t.Context(), "old"); err != nil {
					t.Fatal(err)
				}
				dir, _ := providerDirectory(root, runtime)
				stored, err := backend.LoadWorkExecutionSettings(filepath.Join(dir, "work-execution.json"))
				if err != nil || stored.Model != "removed-model" {
					t.Fatal("repair erased original per-runtime settings", stored, err)
				}
			})
		}
	}
}

func TestCatalogFailureDoesNotEraseExplicitWorkerChoice(t *testing.T) {
	root := t.TempDir()
	owner := &unavailableModelWorker{workerFixture: &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}, catalogErr: errors.New("temporary read failure")}
	pool := workPool(root, "codex", nil)
	pool.resident = owner
	storeWorkerModel(t, root, "codex", api.WorkExecutionSettings{Model: "saved-model"})
	if _, err := pool.StartWork(t.Context(), api.WorkStart{ID: "new"}); err != nil {
		t.Fatal(err)
	}
	if pool.DefaultRuntime() != "codex" || pool.followRoutes["new"] || owner.starts != 1 {
		t.Fatal("temporary failure reset a saved choice")
	}
}

func TestCatalogRepairDoesNotOverrideNewerSelectionOrOriginalBinding(t *testing.T) {
	root := t.TempDir()
	owner := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": owner})
	storeWorkerModel(t, root, "codex", api.WorkExecutionSettings{Model: "codex-model"})
	if err := pool.reconcileModelCatalog("caelis", nil); err != nil {
		t.Fatal(err)
	}
	if pool.DefaultRuntime() != "codex" {
		t.Fatal("other runtime inspection erased explicit choice")
	}
	if _, err := pool.BindWork(t.Context(), api.TaskStart{}, "reserved", ""); err != nil {
		t.Fatal(err)
	}
	if err := pool.reconcileModelCatalog("codex", nil); err != nil {
		t.Fatal(err)
	}
	if pool.DefaultRuntime() != "conversation" {
		t.Fatal("successful empty catalog did not repair choice")
	}
	if _, err := pool.StartWork(t.Context(), api.WorkStart{ID: "reserved"}); err != nil {
		t.Fatal(err)
	}
	if pool.followRoutes["reserved"] {
		t.Fatal("already-bound request adopted a later default")
	}
}
