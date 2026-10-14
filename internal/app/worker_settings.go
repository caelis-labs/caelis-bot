package app

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"path/filepath"
)

func (w *localWorkers) SelectedLocalWorker() string { return w.DefaultRuntime() }

func (w *localWorkers) settingsOwner(ctx context.Context, runtime string) (api.WorkRuntime, error) {
	w.mu.Lock()
	if runtime == w.active {
		w.mu.Unlock()
		return w.resident, nil
	}
	owner := w.owners[runtime]
	if owner == nil {
		var err error
		owner, err = w.open(runtime)
		if err != nil {
			w.mu.Unlock()
			return nil, err
		}
		w.owners[runtime] = owner
	}
	w.mu.Unlock()
	if err := owner.Connect(ctx); err != nil {
		return nil, err
	}
	return owner, nil
}
func (w *localWorkers) inspect(ctx context.Context, runtime string) (api.LocalWorkerSettings, error) {
	v := api.LocalWorkerSettings{Runtime: runtime, Models: []api.ModelOption{}}
	if runtime != "codex" && runtime != "caelis" {
		return v, errors.New("unsupported_runtime")
	}
	owner, err := w.settingsOwner(ctx, runtime)
	if err != nil {
		return v, err
	}
	catalog, ok := owner.(interface {
		Models(context.Context) ([]api.ModelOption, error)
		RuntimeDefault(context.Context) (api.WorkExecutionSettings, error)
	})
	if !ok {
		return v, errors.New("worker models unavailable")
	}
	v.Models, err = catalog.Models(ctx)
	if err != nil {
		return v, err
	}
	dir, _ := providerDirectory(w.root, runtime)
	v.Work, err = backend.LoadWorkExecutionSettings(filepath.Join(dir, "work-execution.json"))
	if err != nil {
		return v, err
	}
	if d, e := catalog.RuntimeDefault(ctx); e == nil {
		v.RuntimeDefault = &d
	}
	v.Ready = len(v.Models) > 0
	if checker, ok := owner.(interface{ CheckFutureWorkExecutionMode(string) error }); ok {
		if issue := checker.CheckFutureWorkExecutionMode("auto"); issue != nil {
			v.Ready = false
			v.ExecutionModeIssue = issue.Error()
		} else {
			v.FullAccessAvailable = checker.CheckFutureWorkExecutionMode("full-access") == nil
		}
	}
	if runtime == w.active {
		if s, ok := owner.(api.Engine); ok {
			v.Ready = v.Ready && s.Snapshot().Connection == "ready"
		}
	} else if err = owner.WorkAdmission(ctx); err != nil {
		v.Ready = false
	}
	return v, nil
}
func (w *localWorkers) InspectLocalWorker(ctx context.Context, runtime string) (api.LocalWorkerSettings, error) {
	// Compatibility path for existing callers. New settings use the explicit
	// read-only inspection and selection methods below.
	if runtime == "" {
		return w.InspectWorkerRuntime(ctx, runtime)
	}
	return w.SelectLocalWorker(ctx, runtime)
}
func (w *localWorkers) InspectWorkerRuntime(ctx context.Context, runtime string) (api.LocalWorkerSettings, error) {
	w.settingsMu.Lock()
	defer w.settingsMu.Unlock()
	w.mu.Lock()
	current := w.defaultRuntime
	w.mu.Unlock()
	if runtime == "" {
		runtime = current
	}
	return w.inspect(ctx, runtime)
}
func (w *localWorkers) SelectLocalWorker(ctx context.Context, runtime string) (api.LocalWorkerSettings, error) {
	w.settingsMu.Lock()
	defer w.settingsMu.Unlock()
	w.mu.Lock()
	current := w.defaultRuntime
	w.mu.Unlock()
	if runtime == "" {
		return api.LocalWorkerSettings{}, errors.New("unsupported_runtime")
	}
	v, err := w.inspect(ctx, runtime)
	if err != nil {
		return v, err
	}
	if runtime != current {
		if !v.Ready {
			return v, errors.New("worker_not_ready")
		}
		w.mu.Lock()
		defer w.mu.Unlock()
		if err := localstate.Write(w.path, struct {
			Version int    `json:"version"`
			Runtime string `json:"runtime"`
		}{1, runtime}); err != nil {
			return v, err
		}
		w.defaultRuntime = runtime
	}
	return v, nil
}
func (w *localWorkers) SaveLocalWorkerModel(ctx context.Context, value api.WorkExecutionSettings) (api.LocalWorkerSettings, error) {
	if err := api.ValidateNewExecutionMode(value.ApprovalMode); err != nil {
		return api.LocalWorkerSettings{}, err
	}
	w.settingsMu.Lock()
	defer w.settingsMu.Unlock()
	w.mu.Lock()
	runtime := w.defaultRuntime
	w.mu.Unlock()
	dir, _ := providerDirectory(w.root, runtime)
	current, err := backend.LoadWorkExecutionSettings(filepath.Join(dir, "work-execution.json"))
	if err != nil {
		return api.LocalWorkerSettings{}, err
	}
	// A model picker never changes the permission choice.
	if value.ApprovalMode == "" {
		value.ApprovalMode = current.ApprovalMode
	}
	return w.saveWorkSettings(ctx, runtime, value)
}

func (w *localWorkers) SaveLocalWorkerExecutionMode(ctx context.Context, mode string) (api.LocalWorkerSettings, error) {
	if mode != "auto" && mode != "full-access" {
		return api.LocalWorkerSettings{}, errors.New("执行模式仅支持自动审查或完全访问")
	}
	w.settingsMu.Lock()
	defer w.settingsMu.Unlock()
	w.mu.Lock()
	runtime := w.defaultRuntime
	w.mu.Unlock()
	dir, _ := providerDirectory(w.root, runtime)
	value, err := backend.LoadWorkExecutionSettings(filepath.Join(dir, "work-execution.json"))
	if err != nil {
		return api.LocalWorkerSettings{}, err
	}
	value.ApprovalMode = mode
	return w.saveWorkSettings(ctx, runtime, value)
}

func (w *localWorkers) saveWorkSettings(ctx context.Context, runtime string, value api.WorkExecutionSettings) (api.LocalWorkerSettings, error) {
	owner, err := w.settingsOwner(ctx, runtime)
	if err != nil {
		return api.LocalWorkerSettings{}, err
	}
	dir, _ := providerDirectory(w.root, runtime)
	persist := func() error { return localstate.Write(filepath.Join(dir, "work-execution.json"), value) }
	if model, ok := owner.(interface {
		ChangeWorkExecution(context.Context, api.WorkExecutionSettings, func() error) error
	}); ok {
		err = model.ChangeWorkExecution(ctx, value, persist)
	} else if model, ok := owner.(interface {
		SetModel(context.Context, api.WorkExecutionSettings, func() error) error
	}); ok {
		err = model.SetModel(ctx, value, persist)
	} else {
		err = errors.New("worker models unavailable")
	}
	if err != nil {
		return api.LocalWorkerSettings{}, err
	}
	return w.inspect(ctx, runtime)
}
