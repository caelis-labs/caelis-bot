package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type retainedWorker interface {
	api.WorkRuntime
	api.WorkTerminalProvider
	Connect(context.Context) error
	Close(context.Context) error
}

// The resident remains the admission authority. Inactive providers expose only
// their owned Workers, with no Bot conversation or tool callbacks.
type localWorkers struct {
	settingsMu     sync.Mutex
	path, root     string
	defaultRuntime string
	loadErr        error
	mu             sync.Mutex
	active         string
	resident       api.WorkRuntime
	owners         map[string]retainedWorker
	routes         map[string]string
	open           func(string) (retainedWorker, error)
}

func (a *Application) configureWorkers() error {
	active := a.currentEngine().(api.Provider).ProviderInfo().ID
	a.localWork = &localWorkers{active: active, resident: a.currentEngine().(api.WorkRuntime), routes: map[string]string{}, owners: map[string]retainedWorker{}}
	a.localWork.root = a.root
	a.localWork.path = filepath.Join(a.root, "worker-runtime.json")
	a.localWork.defaultRuntime = active
	if b, err := os.ReadFile(a.localWork.path); err == nil {
		var v struct {
			Version int    `json:"version"`
			Runtime string `json:"runtime"`
		}
		if json.Unmarshal(b, &v) != nil || v.Version != 1 || !(v.Runtime == "conversation" || a.localWork.OwnsWork(v.Runtime)) {
			a.localWork.loadErr = errors.New("worker settings invalid")
		}
		if a.localWork.loadErr == nil {
			a.localWork.defaultRuntime = v.Runtime
		}
	} else {
		if !os.IsNotExist(err) {
			a.localWork.loadErr = err
		}
		if a.localWork.loadErr == nil {
			if err = localstate.Write(a.localWork.path, struct {
				Version int    `json:"version"`
				Runtime string `json:"runtime"`
			}{1, active}); err != nil {
				a.localWork.loadErr = err
			}
		}
	}
	a.localWork.open = func(id string) (retainedWorker, error) {
		dir, err := providerDirectory(a.root, id)
		if err != nil {
			return nil, err
		}
		settings, err := backend.LoadRuntimeSettings(filepath.Join(a.root, "runtime-profiles", id+".json"), id)
		if err != nil {
			return nil, err
		}
		work, err := backend.LoadWorkExecutionSettings(filepath.Join(dir, "work-execution.json"))
		if err != nil {
			return nil, err
		}
		switch id {
		case "codex":
			return codex.NewRetainedWorkOwner(codex.SessionOptions{Diagnostics: a.host.Diagnostics, Binary: settings.CLIPath, WorkExecution: work, Directory: filepath.Join(dir, "Work"), WorkRoot: filepath.Join(a.root, "Tasks"), StateFile: filepath.Join(dir, "conversation.json")})
		case "caelis":
			return caelis.NewRetainedWorkOwner(caelis.Options{Diagnostics: a.host.Diagnostics, Settings: settings, Directory: dir, WorkExecution: work})
		}
		return nil, errors.New("original_task_runtime_unavailable")
	}
	a.Backend.ConfigureLocalWorkers(a.localWork)
	return a.localWork.loadErr
}

func (w *localWorkers) OwnsWork(id string) bool {
	return id == w.active || id == "codex" || id == "caelis"
}

// SwitchResident keeps task routes and the user's independent Worker default.
// Historical work for the outgoing provider is reopened through its original
// retained adapter on demand; no task is moved to the incoming provider.
func (w *localWorkers) SwitchResident(id string, engine api.Engine) retainedWorker {
	w.mu.Lock()
	defer w.mu.Unlock()
	retained := w.owners[id]
	delete(w.owners, id)
	w.active = id
	w.resident = engine.(api.WorkRuntime)
	return retained
}

func (w *localWorkers) RestoreRetained(id string, owner retainedWorker) {
	if owner == nil {
		return
	}
	w.mu.Lock()
	w.owners[id] = owner
	w.mu.Unlock()
}

func (w *localWorkers) CanSwitchResident(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if owner := w.owners[id]; owner != nil {
		for _, state := range owner.WorkStates() {
			switch state.Task.Status {
			case "completed", "failed", "cancelled", "interrupted":
			default:
				return errors.New("该执行方式还有进行中的工作，请完成后再切换")
			}
		}
	}
	return nil
}

func (w *localWorkers) SelectFirstRuntime(id string) error {
	w.settingsMu.Lock()
	defer w.settingsMu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := localstate.Write(w.path, struct {
		Version int    `json:"version"`
		Runtime string `json:"runtime"`
	}{1, id}); err != nil {
		return err
	}
	w.defaultRuntime = id
	return nil
}

func (w *localWorkers) DefaultRuntime() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.defaultRuntime
}
func (w *localWorkers) BindWork(_ context.Context, in api.TaskStart, id, runtime string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reloadSettingsLocked()
	if runtime == "" && w.loadErr != nil {
		return "", w.loadErr
	}
	if old := w.routes[id]; old != "" {
		if runtime != "" && old != runtime {
			return "", errors.New("task_target_conflict")
		}
		return old, nil
	}
	if runtime == "" {
		runtime = w.defaultRuntime
		if in.LocalRuntime || runtime == "conversation" {
			// The model-facing local target is the Runtime hosting this Bot.
			runtime = w.active
		}
	}
	if !w.OwnsWork(runtime) {
		return "", errors.New("original_task_runtime_unavailable")
	}
	if runtime != w.active && w.owners[runtime] == nil {
		owner, err := w.open(runtime)
		// Missing/corrupt inactive owners cannot hide the ledger or prevent the
		// current Bot from starting. Access retries only this exact owner.
		if err == nil {
			w.owners[runtime] = owner
		}
	}
	w.routes[id] = runtime
	return runtime, nil
}
func (w *localWorkers) WorkAdmission(ctx context.Context) error {
	w.mu.Lock()
	owner := w.resident
	w.mu.Unlock()
	return owner.WorkAdmission(ctx)
}
func (w *localWorkers) WorkStates() []api.WorkState {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := []api.WorkState{}
	appendStates := func(runtime string, owner api.WorkRuntime) {
		for _, state := range owner.WorkStates() {
			state.Runtime = runtime
			if old := w.routes[state.Task.ID]; old != "" && old != runtime {
				continue
			}
			w.routes[state.Task.ID] = runtime
			out = append(out, state)
		}
	}
	appendStates(w.active, w.resident)
	for id, owner := range w.owners {
		appendStates(id, owner)
	}
	return out
}
func (w *localWorkers) target(ctx context.Context, id string) (api.WorkRuntime, error) {
	w.mu.Lock()
	runtime := w.routes[id]
	owner := w.owners[runtime]
	resident := w.resident
	active := w.active
	if runtime != "" && runtime != active && owner == nil {
		var err error
		owner, err = w.open(runtime)
		if err != nil {
			w.mu.Unlock()
			return nil, err
		}
		w.owners[runtime] = owner
	}
	w.mu.Unlock()
	if runtime == active {
		return resident, nil
	}
	if owner == nil {
		return nil, errors.New("original_task_runtime_unavailable")
	}
	if err := owner.Connect(ctx); err != nil {
		return nil, err
	}
	return owner, nil
}
func (w *localWorkers) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if err := w.WorkAdmission(ctx); err != nil {
		return api.Task{}, err
	}
	if _, err := w.BindWork(ctx, in.TaskStart, in.ID, ""); err != nil {
		return api.Task{}, err
	}
	owner, err := w.target(ctx, in.ID)
	if err != nil {
		return api.Task{}, err
	}
	w.mu.Lock()
	follow := w.defaultRuntime == "conversation"
	runtime := w.routes[in.ID]
	w.mu.Unlock()
	if follow {
		dir, err := providerDirectory(w.root, runtime)
		if err != nil {
			return api.Task{}, err
		}
		prefs, err := backend.LoadExecutionSettings(filepath.Join(dir, "execution.json"), api.ExecutionSettings{})
		if err != nil {
			return api.Task{}, err
		}
		if prefs.Model == "" {
			return api.Task{}, errors.New("conversation model unavailable")
		}
		execution := api.WorkModel(prefs)
		in.ExecutionOverride = &execution
	}
	return owner.StartWork(ctx, in)
}
func (w *localWorkers) ReadWork(ctx context.Context, id string) (api.Task, error) {
	owner, err := w.target(ctx, id)
	if err != nil {
		return api.Task{}, err
	}
	return owner.ReadWork(ctx, id)
}
func (w *localWorkers) ReadWorkConversation(ctx context.Context, id string) ([]api.TaskConversationMessage, error) {
	owner, err := w.target(ctx, id)
	if err != nil {
		return nil, err
	}
	reader, ok := owner.(api.WorkConversationReader)
	if !ok {
		return nil, errors.New("native task messages unavailable for original owner")
	}
	return reader.ReadWorkConversation(ctx, id)
}
func (w *localWorkers) RetireWork(ctx context.Context, id string) (api.Task, error) {
	owner, err := w.target(ctx, id)
	if err != nil {
		return api.Task{}, err
	}
	p, ok := owner.(api.WorkRetirer)
	if !ok {
		return api.Task{}, errors.New("original owner cannot retire tasks")
	}
	return p.RetireWork(ctx, id)
}
func (w *localWorkers) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	if err := w.WorkAdmission(ctx); err != nil {
		return api.Task{}, err
	}
	owner, err := w.target(ctx, in.ID)
	if err != nil {
		return api.Task{}, err
	}
	return owner.SendWork(ctx, in)
}
func (w *localWorkers) StopWork(ctx context.Context, id string) (api.Task, error) {
	w.mu.Lock()
	runtime, active := w.routes[id], w.active
	retained := w.owners[runtime]
	w.mu.Unlock()
	if runtime == "codex" && runtime != active && retained != nil {
		if connected, ok := retained.(interface{ ConnectedForStop() bool }); ok && connected.ConnectedForStop() {
			// This exact local owner is already connected. StopWork cancels its
			// capacity wait before taking the adapter operation lock; Connect
			// would wait on that lock and prevent the cancellation.
			return retained.StopWork(ctx, id)
		}
	}
	owner, err := w.target(ctx, id)
	if err != nil {
		return api.Task{}, err
	}
	return owner.StopWork(ctx, id)
}
func (w *localWorkers) WorkTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	owner, err := w.target(ctx, id)
	if err != nil {
		return api.TerminalTarget{}, err
	}
	t, ok := owner.(api.WorkTerminalProvider)
	if !ok {
		return api.TerminalTarget{}, errors.New("terminal unavailable")
	}
	return t.WorkTerminal(ctx, id)
}
func (w *localWorkers) WorkMessageRecorded(in api.TaskMessage) bool {
	w.mu.Lock()
	runtime := w.routes[in.ID]
	owner := api.WorkRuntime(w.owners[runtime])
	w.mu.Unlock()
	if runtime == w.active {
		owner = w.resident
	}
	p, ok := owner.(api.RecordedWorkMessage)
	return ok && p.WorkMessageRecorded(in)
}
func (w *localWorkers) Observe(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		w.mu.Lock()
		w.reloadSettingsLocked()
		owners := []retainedWorker{}
		for _, owner := range w.owners {
			owners = append(owners, owner)
		}
		w.mu.Unlock()
		var observations sync.WaitGroup
		for _, owner := range owners {
			observations.Go(func() { c, cancel := context.WithTimeout(ctx, 10*time.Second); defer cancel(); _ = owner.Connect(c) })
		}
		observations.Wait()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *localWorkers) reloadSettingsLocked() {
	if w.loadErr == nil {
		return
	}
	b, err := os.ReadFile(w.path)
	var v struct {
		Version int    `json:"version"`
		Runtime string `json:"runtime"`
	}
	if err == nil && json.Unmarshal(b, &v) == nil && v.Version == 1 && (v.Runtime == "conversation" || w.OwnsWork(v.Runtime)) {
		w.defaultRuntime, w.loadErr = v.Runtime, nil
	}
}
func (w *localWorkers) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var err error
	for _, owner := range w.owners {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		err = errors.Join(err, owner.Close(ctx))
		cancel()
	}
	return err
}

func (w *localWorkers) CanDetachForUpdate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, owner := range w.owners {
		if _, ok := owner.(interface{ DetachForUpdate(context.Context) error }); ok {
			if preparer, ok := owner.(interface{ PrepareDetachForUpdate(context.Context) error }); ok {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				err := preparer.PrepareDetachForUpdate(ctx)
				cancel()
				if err != nil {
					return err
				}
			}
			continue
		}
		for _, state := range owner.WorkStates() {
			switch state.Task.Status {
			case "completed", "failed", "cancelled", "interrupted":
			default:
				return errors.New("原任务 Runtime 无法保存活跃工作以完成更新")
			}
		}
	}
	return nil
}

func (w *localWorkers) CloseForUpdate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var err error
	for _, owner := range w.owners {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		if detacher, ok := owner.(interface{ DetachForUpdate(context.Context) error }); ok {
			err = errors.Join(err, detacher.DetachForUpdate(ctx))
		} else {
			busy := false
			for _, state := range owner.WorkStates() {
				switch state.Task.Status {
				case "completed", "failed", "cancelled", "interrupted":
				default:
					busy = true
				}
			}
			if busy {
				err = errors.Join(err, errors.New("原任务 Runtime 无法保存活跃工作以完成更新"))
			} else {
				err = errors.Join(err, owner.Close(ctx))
			}
		}
		cancel()
	}
	return err
}
