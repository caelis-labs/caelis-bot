package app

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
	"path/filepath"
)

func (w *localWorkers) settingsOwner(ctx context.Context, runtime string) (api.WorkRuntime, error) {
	w.mu.Lock()
	if runtime == w.switching && runtime != w.active {
		w.mu.Unlock()
		return nil, errors.New("worker observer handoff in progress")
	}
	if runtime == w.active {
		resident := w.resident
		w.mu.Unlock()
		return resident, nil
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
	w.settingsMu.Lock()
	defer w.settingsMu.Unlock()
	w.mu.Lock()
	current := w.defaultRuntime
	w.mu.Unlock()
	if runtime == "" {
		runtime = current
	}
	inspectionRuntime := runtime
	if runtime == "conversation" {
		w.mu.Lock()
		inspectionRuntime = w.active
		w.mu.Unlock()
	}
	v, err := w.inspect(ctx, inspectionRuntime)
	if runtime == "conversation" {
		v.Runtime = runtime
		v.Work = api.WorkExecutionSettings{}
	}
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
	w.settingsMu.Lock()
	defer w.settingsMu.Unlock()
	w.mu.Lock()
	runtime := w.defaultRuntime
	if runtime == "conversation" {
		w.mu.Unlock()
		return api.LocalWorkerSettings{}, errors.New("select an explicit worker runtime before changing its model")
	}
	w.mu.Unlock()
	return w.saveModel(ctx, runtime, value)
}
func (w *localWorkers) saveModel(ctx context.Context, runtime string, value api.WorkExecutionSettings) (api.LocalWorkerSettings, error) {
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

// Save the model before committing the default Runtime. Existing tasks retain their owners.
func (w *localWorkers) saveSelectedModel(ctx context.Context, runtime string, value api.WorkExecutionSettings) error {
	if err := lockwait.Lock(ctx, &w.settingsMu); err != nil {
		return err
	}
	defer w.settingsMu.Unlock()
	v, e := w.inspect(ctx, runtime)
	if e != nil {
		return e
	}
	if !v.Ready {
		return errors.New("worker_not_ready")
	}
	if _, e = w.saveModel(ctx, runtime, value); e != nil {
		return e
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if runtime == w.defaultRuntime {
		return nil
	}
	if e = localstate.Write(w.path, struct {
		Version int    `json:"version"`
		Runtime string `json:"runtime"`
	}{1, runtime}); e != nil {
		return e
	}
	w.defaultRuntime = runtime
	return nil
}

// LocalWorkerSelection reads the saved choice even when its Runtime is offline.
func (w *localWorkers) LocalWorkerSelection() (api.LocalWorkerSettings, error) {
	w.mu.Lock()
	runtime := w.defaultRuntime
	w.mu.Unlock()
	if runtime == "conversation" {
		return api.LocalWorkerSettings{Runtime: runtime, Models: []api.ModelOption{}}, nil
	}
	dir, err := providerDirectory(w.root, runtime)
	if err != nil {
		return api.LocalWorkerSettings{}, err
	}
	value, err := backend.LoadWorkExecutionSettings(filepath.Join(dir, "work-execution.json"))
	return api.LocalWorkerSettings{Runtime: runtime, Work: value, Models: []api.ModelOption{}}, err
}
