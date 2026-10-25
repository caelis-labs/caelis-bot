package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/lockwait"
	"os"
	"path/filepath"
)

func (w *localWorkers) settingsOwner(ctx context.Context, runtime string) (api.WorkRuntime, error) {
	w.mu.Lock()
	if runtime == w.switching && runtime != w.active {
		w.mu.Unlock()
		return nil, errors.New("worker observer handoff in progress")
	}
	if runtime == w.active {
		owner := w.resident
		w.mu.Unlock()
		return owner, nil
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
		return v, &workerModelCatalogError{err}
	}
	dir, _ := providerDirectory(w.root, runtime)
	v.Work, err = backend.LoadWorkExecutionSettings(filepath.Join(dir, "work-execution.json"))
	if err != nil {
		return v, &workerSettingsLoadError{err}
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

// Pairing inspects the configured local Worker. It neither selects a different
// runtime/model nor creates a separate, empty foreign execution owner.
func (w *localWorkers) workerReadiness(ctx context.Context) error {
	if err := lockwait.Lock(ctx, &w.settingsMu); err != nil {
		return err
	}
	defer w.settingsMu.Unlock()
	w.mu.Lock()
	runtime, active, resident, loadErr := w.defaultRuntime, w.active, w.resident, w.loadErr
	w.mu.Unlock()
	if loadErr != nil {
		return readinessProblem("worker_configuration_invalid", loadErr)
	}
	selected := runtime
	if selected == "conversation" {
		selected = active
	}
	if selected == active {
		if code := workerConnectionIssue(resident); code != "" {
			return readinessProblem(code, nil)
		}
	}
	v, err := w.inspect(ctx, selected)
	if err != nil {
		code := api.WorkerReadinessCode(err)
		if ctx.Err() != nil {
			code = api.WorkerReadinessCode(ctx.Err())
		}
		if code == "" {
			w.mu.Lock()
			owner := w.owners[selected]
			w.mu.Unlock()
			code = workerConnectionIssue(owner)
		}
		var settings *workerSettingsLoadError
		var catalog *workerModelCatalogError
		if code == "" && errors.As(err, &catalog) {
			code = "worker_model_catalog_unavailable"
		}
		if errors.As(err, &settings) {
			code = "worker_configuration_invalid"
		}
		if code == "" {
			code = "worker_runtime_unavailable"
		}
		return readinessProblem(code, err)
	}
	if !v.Ready {
		if len(v.Models) == 0 {
			return readinessProblem("worker_model_required", nil)
		}
		return readinessProblem("worker_runtime_unavailable", nil)
	}
	execution := v.Work
	if runtime == "conversation" {
		provider, ok := resident.(interface {
			ConversationExecution(context.Context) (api.WorkExecutionSettings, error)
		})
		if !ok {
			return readinessProblem("worker_runtime_incompatible", nil)
		}
		execution, err = provider.ConversationExecution(ctx)
		if err != nil {
			code := api.WorkerReadinessCode(err)
			if code == "" {
				code = "worker_runtime_unavailable"
			}
			return readinessProblem(code, err)
		}
	} else if execution.Model == "" && v.RuntimeDefault != nil {
		execution = *v.RuntimeDefault
	}
	if err := api.ValidateWorkExecution(execution, v.Models); err != nil {
		code := api.WorkerReadinessCode(err)
		if code == "" {
			code = "worker_configuration_invalid"
		}
		return readinessProblem(code, err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.defaultRuntime != runtime || w.active != active {
		return readinessProblem("worker_configuration_changed", nil)
	}
	return nil
}

type workerSettingsLoadError struct{ error }

func (e *workerSettingsLoadError) Unwrap() error { return e.error }

type workerModelCatalogError struct{ error }

func (e *workerModelCatalogError) Unwrap() error { return e.error }

func readinessProblem(code string, cause error) error {
	return errors.Join(&api.SetupBlockedError{Code: code, Message: code}, cause)
}
func workerConnectionIssue(owner any) string {
	p, ok := owner.(interface{ Snapshot() api.Snapshot })
	if !ok {
		return ""
	}
	v := p.Snapshot()
	if v.Connection == "" || v.Connection == "ready" {
		return ""
	}
	switch v.ConnectionIssue {
	case "authentication":
		return "worker_login_required"
	case "runtime_missing":
		return "worker_runtime_missing"
	case "runtime_protocol":
		return "worker_runtime_incompatible"
	default:
		return "worker_runtime_unavailable"
	}
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
	w.settingsMu.Lock()
	defer w.settingsMu.Unlock()
	return w.selectionLocked()
}

// No runtime probes on the settings fast path. Legacy empty model selections
// mean follow conversation, regardless of the old provider placeholder.
func (w *localWorkers) selectionLocked() (api.LocalWorkerSettings, error) {
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
	// Decode before validating so readable legacy preferences (for example
	// an empty model with leftover effort) can fall back. Unreadable or corrupt
	// files remain intact and are never mistaken for an empty preference.
	var value api.WorkExecutionSettings
	data, err := os.ReadFile(filepath.Join(dir, "work-execution.json"))
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	} else if err == nil {
		err = json.Unmarshal(data, &value)
	}
	if err == nil && (value.Model == "" || api.ValidateExecutionSettings(value.Execution()) != nil) {
		err = w.followConversationLocked()
		return api.LocalWorkerSettings{Runtime: "conversation", Models: []api.ModelOption{}}, err
	}
	return api.LocalWorkerSettings{Runtime: runtime, Work: value, Models: []api.ModelOption{}}, err
}

// Caller holds settingsMu. Keep per-provider preferences and all original task
// bindings; changing the default affects only requests that are not yet bound.
func (w *localWorkers) followConversationLocked() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := localstate.Write(w.path, struct {
		Version int    `json:"version"`
		Runtime string `json:"runtime"`
	}{1, "conversation"}); err != nil {
		return err
	}
	w.defaultRuntime, w.loadErr = "conversation", nil
	return nil
}

func (w *localWorkers) reconcileModelCatalog(runtime string, models []api.ModelOption) error {
	w.settingsMu.Lock()
	defer w.settingsMu.Unlock()
	return w.reconcileModelCatalogLocked(runtime, models)
}
func (w *localWorkers) reconcileModelCatalogLocked(runtime string, models []api.ModelOption) error {
	selection, err := w.selectionLocked()
	if err != nil || selection.Runtime != runtime {
		return err
	}
	if api.ValidateWorkExecution(selection.Work, models) != nil {
		return w.followConversationLocked()
	}
	return nil
}

// Validate only before a new default route is frozen, never after dispatch or
// while restoring an original route. Failed discovery is not proof of removal.
// Caller holds settingsMu; neither catalog reads nor Connect hold w.mu.
func (w *localWorkers) prepareDefaultModel(ctx context.Context) error {
	selection, err := w.selectionLocked()
	if err != nil || selection.Runtime == "conversation" {
		return err
	}
	owner, err := w.settingsOwner(ctx, selection.Runtime)
	if err != nil {
		return nil // Preserve an explicit choice during transient unavailability.
	}
	catalog, ok := owner.(interface {
		Models(context.Context) ([]api.ModelOption, error)
	})
	if !ok {
		return nil
	}
	models, err := catalog.Models(ctx)
	if err != nil {
		return nil
	}
	return w.reconcileModelCatalogLocked(selection.Runtime, models)
}
