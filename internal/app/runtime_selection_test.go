package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func TestMissingRuntimeCatalogDoesNotSelectOrExposeModels(t *testing.T) {
	a, err := New(t.TempDir(), Host{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, id := range []string{"codex", "caelis"} {
		profile := api.RuntimeSettings{Runtime: id, CLIPath: filepath.Join(t.TempDir(), "missing-runtime")}
		if err := a.setup.saveProfile(profile); err != nil {
			t.Fatal(err)
		}
		models, err := a.setup.SetupModels(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if len(models) != 0 || a.HasRuntimeChoice() {
			t.Fatal("inspection exposed unavailable models or selected a runtime", models)
		}
		if err := a.setup.SaveSelectedModel(t.Context(), "conversation", id, api.WorkExecutionSettings{Model: "not-available"}); err == nil {
			t.Fatal("unready model accepted")
		}
	}
	if a.HasRuntimeChoice() {
		t.Fatal("failed selection changed resident")
	}
	if _, err := os.Stat(filepath.Join(a.root, "runtime.json")); !os.IsNotExist(err) {
		t.Fatal("inspection wrote resident selection", err)
	}
}

type modelWorkerFixture struct {
	*workerFixture
	fail bool
}

func (w *modelWorkerFixture) SetModel(ctx context.Context, v api.WorkExecutionSettings, persist func() error) error {
	if w.fail {
		return errors.New("model rejected")
	}
	models, _ := w.Models(ctx)
	if err := api.ValidateWorkExecution(v, models); err != nil {
		return err
	}
	return persist()
}
func TestWorkerModelSelectionCommitsOnlyAfterModelSave(t *testing.T) {
	root := t.TempDir()
	original := &workerFixture{runtime: "codex", states: map[string]api.WorkState{"old": {Task: api.Task{ID: "old"}}}}
	next := &modelWorkerFixture{workerFixture: &workerFixture{runtime: "caelis", states: map[string]api.WorkState{}}, fail: true}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": original})
	pool.owners["caelis"] = next
	pool.routes["old"] = "codex"
	value := api.WorkExecutionSettings{Model: "caelis-model"}
	if err := pool.saveSelectedModel(t.Context(), "caelis", value); err == nil {
		t.Fatal("failed save accepted")
	}
	if pool.defaultRuntime != "codex" {
		t.Fatal("failed save changed default")
	}
	next.fail = false
	if err := pool.saveSelectedModel(t.Context(), "caelis", value); err != nil {
		t.Fatal(err)
	}
	if pool.defaultRuntime != "caelis" || pool.active != "codex" {
		t.Fatal("worker selection changed resident")
	}
	dir, _ := providerDirectory(root, "caelis")
	stored, err := backend.LoadWorkExecutionSettings(filepath.Join(dir, "work-execution.json"))
	if err != nil || stored != value {
		t.Fatal(stored, err)
	}
	if _, err := pool.ReadWork(t.Context(), "old"); err != nil {
		t.Fatal("old work lost its owner", err)
	}
	before := next.connections
	next.reject = true
	selection, err := pool.LocalWorkerSelection()
	if err != nil || selection.Runtime != "caelis" || selection.Work != value || next.connections != before {
		t.Fatal("reading offline choice opened runtime", selection, err)
	}
}

func TestWorkerSelectionReadPreservesOfflineSavedChoice(t *testing.T) {
	root := t.TempDir()
	pool := workPool(root, "codex", map[string]*workerFixture{})
	pool.open = func(string) (retainedWorker, error) { t.Fatal("read attempted to open runtime"); return nil, nil }
	dir, _ := providerDirectory(root, "codex")
	value := api.WorkExecutionSettings{Model: "saved-model", Effort: "high"}
	if err := localstate.Write(filepath.Join(dir, "work-execution.json"), value); err != nil {
		t.Fatal(err)
	}
	got, err := pool.LocalWorkerSelection()
	if err != nil || got.Runtime != "codex" || got.Work != value {
		t.Fatal(got, err)
	}
}

type followingWorkerFixture struct {
	*workerFixture
	execution  *api.WorkExecutionSettings
	current    api.WorkExecutionSettings
	currentErr error
}

func (w *followingWorkerFixture) ConversationExecution(context.Context) (api.WorkExecutionSettings, error) {
	return w.current, w.currentErr
}
func (w *followingWorkerFixture) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	w.execution = in.ExecutionOverride
	return w.workerFixture.StartWork(ctx, in)
}
func TestWorkerFollowConversationUsesCurrentModelAndKeepsOldRoutes(t *testing.T) {
	root := t.TempDir()
	original := &workerFixture{runtime: "codex", states: map[string]api.WorkState{"old": {Task: api.Task{ID: "old"}}}}
	next := &followingWorkerFixture{workerFixture: &workerFixture{runtime: "caelis", states: map[string]api.WorkState{}}}
	pool := workPool(root, "codex", map[string]*workerFixture{"codex": original})
	if err := pool.SelectFirstRuntime("conversation"); err != nil {
		t.Fatal(err)
	}
	pool.routes["old"] = "codex"
	pool.owners["codex"] = original
	pool.active, pool.resident = "caelis", next
	dir, _ := providerDirectory(root, "caelis")
	for i, model := range []string{"first-model", "changed-model"} {
		prefs := api.ExecutionSettings{Model: model, Effort: "medium"}
		next.current = api.WorkModel(prefs)
		if err := localstate.Write(filepath.Join(dir, "execution.json"), prefs); err != nil {
			t.Fatal(err)
		}
		id := []string{"new-one", "new-two"}[i]
		if _, err := pool.StartWork(t.Context(), api.WorkStart{ID: id}); err != nil {
			t.Fatal(err)
		}
		if next.execution == nil || next.execution.Model != model || next.execution.Effort != "medium" || pool.routes[id] != "caelis" {
			t.Fatal("did not follow conversation", next.execution, pool.routes)
		}
	}
	if _, err := pool.ReadWork(t.Context(), "old"); err != nil {
		t.Fatal(err)
	}
	selected, err := pool.LocalWorkerSelection()
	if err != nil || selected.Runtime != "conversation" || selected.Work.Model != "" {
		t.Fatal(selected, err)
	}
	pool.defaultRuntime = "caelis"
	if err := localstate.Write(filepath.Join(dir, "work-execution.json"), api.WorkExecutionSettings{Model: "caelis-model"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.StartWork(t.Context(), api.WorkStart{ID: "explicit"}); err != nil {
		t.Fatal(err)
	}
	if next.execution != nil {
		t.Fatal("explicit worker choice was overridden")
	}
}

func TestWorkerFollowConversationResolvesUnsavedDefault(t *testing.T) {
	for _, runtime := range []string{"codex", "caelis"} {
		t.Run(runtime, func(t *testing.T) {
			root := t.TempDir()
			worker := &followingWorkerFixture{workerFixture: &workerFixture{runtime: runtime, states: map[string]api.WorkState{}}, current: api.WorkExecutionSettings{Model: "native-default", Effort: "medium", ServiceTier: "priority"}}
			pool := workPool(root, runtime, map[string]*workerFixture{runtime: worker.workerFixture})
			pool.resident = worker
			if err := pool.SelectFirstRuntime("conversation"); err != nil {
				t.Fatal(err)
			}
			dir, _ := providerDirectory(root, runtime)
			if err := localstate.Write(filepath.Join(dir, "execution.json"), api.ExecutionSettings{}); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.StartWork(t.Context(), api.WorkStart{ID: "inherited"}); err != nil {
				t.Fatal(err)
			}
			if worker.execution == nil || *worker.execution != worker.current {
				t.Fatal(worker.execution)
			}
			worker.currentErr = errors.New("read failed")
			starts := worker.starts
			if _, err := pool.StartWork(t.Context(), api.WorkStart{ID: "failed"}); err == nil || worker.starts != starts {
				t.Fatal("failed read started work", err)
			}
		})
	}
}
