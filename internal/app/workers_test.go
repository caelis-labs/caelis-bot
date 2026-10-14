package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

type retainedCapacityStopFixture struct {
	*workerFixture
	connected       bool
	connects, stops int
}

func (w *retainedCapacityStopFixture) ConnectedForStop() bool { return w.connected }
func (w *retainedCapacityStopFixture) Connect(ctx context.Context) error {
	w.connects++
	<-ctx.Done() // A real retained Codex Connect waits on its active operation.
	return ctx.Err()
}
func (w *retainedCapacityStopFixture) StopWork(_ context.Context, id string) (api.Task, error) {
	w.stops++
	return w.states[id].Task, nil
}

func TestRetainedLocalCodexStopReachesConnectedOwnerBeforeConnect(t *testing.T) {
	id := "task-original-codex"
	owner := &retainedCapacityStopFixture{workerFixture: &workerFixture{runtime: "codex", states: map[string]api.WorkState{id: {Task: api.Task{ID: id, Status: "pending"}}}}, connected: true}
	resident := &workerFixture{runtime: "caelis", states: map[string]api.WorkState{}}
	w := &localWorkers{active: "caelis", resident: resident, owners: map[string]retainedWorker{"codex": owner}, routes: map[string]string{id: "codex"}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	view, err := w.StopWork(ctx, id)
	if err != nil || view.ID != id || owner.stops != 1 || owner.connects != 0 {
		t.Fatal("retained Codex stop waited behind Connect or changed owner", view, err, owner.stops, owner.connects)
	}
	owner.connected = false
	ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := w.StopWork(ctx, id); !errors.Is(err, context.DeadlineExceeded) || owner.connects != 1 || owner.stops != 1 {
		t.Fatal("disconnected owner bypassed normal Connect route", err, owner.connects, owner.stops)
	}
}

type workerFixture struct {
	reports                                   int
	runtime                                   string
	states                                    map[string]api.WorkState
	starts, sends, stops, connections, closed int
	lastPrompt                                string
	reject                                    bool
}

type detachableWorkerFixture struct {
	*workerFixture
	detached int
}

type workerModeFixture struct {
	*workerFixture
	value api.WorkExecutionSettings
}

func (w *workerModeFixture) Models(context.Context) ([]api.ModelOption, error) {
	return []api.ModelOption{{Model: "work", Efforts: []string{""}}}, nil
}
func (w *workerModeFixture) RuntimeDefault(context.Context) (api.WorkExecutionSettings, error) {
	return api.WorkExecutionSettings{}, nil
}
func (w *workerModeFixture) ChangeWorkExecution(_ context.Context, v api.WorkExecutionSettings, persist func() error) error {
	if err := persist(); err != nil {
		return err
	}
	w.value = v
	return nil
}

func TestWorkerExecutionModeScopedToSelectedRuntime(t *testing.T) {
	root := t.TempDir()
	f := &workerModeFixture{workerFixture: &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}}
	w := &localWorkers{root: root, active: "codex", defaultRuntime: "codex", resident: f}
	if _, err := w.SaveLocalWorkerExecutionMode(t.Context(), "invalid"); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if _, err := w.SaveLocalWorkerExecutionMode(t.Context(), "auto"); err != nil {
		t.Fatal(err)
	}
	if f.value.ApprovalMode != "auto" {
		t.Fatal("Codex owner did not receive preference", f.value)
	}
	if _, err := w.SaveLocalWorkerExecutionMode(t.Context(), "full-access"); err != nil {
		t.Fatal("Codex full access was not saved", err)
	}
	if f.value.ApprovalMode != "full-access" {
		t.Fatal("Codex owner did not receive full access", f.value)
	}
	model := api.WorkExecutionSettings{Model: "work"}
	if _, err := w.SaveLocalWorkerModel(t.Context(), model); err != nil {
		t.Fatal(err)
	}
	if f.value.Model != "work" || f.value.ApprovalMode != "full-access" {
		t.Fatal("model update erased permission", f.value)
	}
	w.defaultRuntime = "caelis"
	if _, err := w.SaveLocalWorkerExecutionMode(t.Context(), "full-access"); err == nil {
		t.Fatal("unsupported Core Worker permission accepted")
	}
	if f.value.ApprovalMode != "full-access" {
		t.Fatal("Caelis choice changed Codex preference")
	}
}

func (w *detachableWorkerFixture) DetachForUpdate(context.Context) error { w.detached++; return nil }

func (w *workerFixture) Connect(context.Context) error { w.connections++; return nil }
func (w *workerFixture) Close(context.Context) error   { w.closed++; return nil }
func (w *workerFixture) WorkAdmission(context.Context) error {
	if w.reject {
		return errors.New("native source rejected")
	}
	return nil
}
func (w *workerFixture) WorkStates() []api.WorkState {
	out := []api.WorkState{}
	for _, s := range w.states {
		out = append(out, s)
	}
	return out
}
func (w *workerFixture) StartWork(_ context.Context, in api.WorkStart) (api.Task, error) {
	w.starts++
	v := api.Task{ID: in.ID, Workspace: in.Workspace, Title: in.Title, Status: "working", Outcome: "accepted"}
	w.states[in.ID] = api.WorkState{Task: v, ExecutionKey: w.runtime + "-native-turn"}
	return v, nil
}
func (w *workerFixture) ReadWork(_ context.Context, id string) (api.Task, error) {
	s, ok := w.states[id]
	if !ok {
		return api.Task{}, errors.New("wrong native owner")
	}
	return s.Task, nil
}
func (w *workerFixture) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	w.sends++
	w.lastPrompt = in.Prompt
	return w.ReadWork(ctx, in.ID)
}
func (w *workerFixture) StopWork(ctx context.Context, id string) (api.Task, error) {
	w.stops++
	return w.ReadWork(ctx, id)
}
func (w *workerFixture) WorkTerminal(ctx context.Context, id string) (api.TerminalTarget, error) {
	_, e := w.ReadWork(ctx, id)
	return api.TerminalTarget{Runtime: w.runtime, Thread: w.runtime + "-" + id}, e
}
func (w *workerFixture) Models(context.Context) ([]api.ModelOption, error) {
	return []api.ModelOption{{Model: w.runtime + "-model", Name: w.runtime + " model"}}, nil
}
func (w *workerFixture) RuntimeDefault(context.Context) (api.WorkExecutionSettings, error) {
	return api.WorkExecutionSettings{Model: w.runtime + "-model"}, nil
}
func (w *workerFixture) SubmitReport(_ context.Context, in api.Submission) (api.Receipt, error) {
	w.reports++
	return api.Receipt{ID: in.ID, Outcome: "accepted"}, nil
}
func workPool(root, active string, owners map[string]*workerFixture) *localWorkers {
	return &localWorkers{root: root, path: filepath.Join(root, "worker-runtime.json"), defaultRuntime: active, active: active, resident: owners[active], owners: map[string]retainedWorker{}, routes: map[string]string{}, open: func(runtime string) (retainedWorker, error) { return owners[runtime], nil }}
}

func TestUpdateClosesRetainedOwnersByOriginalDetachCapability(t *testing.T) {
	active := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	legacy := &workerFixture{runtime: "codex", states: map[string]api.WorkState{"work": {Task: api.Task{ID: "work", Status: "working"}}}}
	pool := &localWorkers{active: "caelis", owners: map[string]retainedWorker{"codex": legacy}}
	if err := pool.CanDetachForUpdate(); err == nil {
		t.Fatal("active original owner without detach was admitted")
	}
	if err := pool.CloseForUpdate(); err == nil || legacy.closed != 0 {
		t.Fatal("active original owner was interrupted", err, legacy.closed)
	}
	detachable := &detachableWorkerFixture{workerFixture: active}
	pool.owners["codex"] = detachable
	if err := pool.CanDetachForUpdate(); err != nil {
		t.Fatal(err)
	}
	if err := pool.CloseForUpdate(); err != nil || detachable.detached != 1 || active.closed != 0 {
		t.Fatal("original owner was not detached", err, detachable.detached, active.closed)
	}
}
func TestLocalDefaultSwitchAndResidentSwitchKeepTaskOwnership(t *testing.T) {
	root := t.TempDir()
	owners := map[string]*workerFixture{}
	for _, runtime := range []string{"codex", "caelis"} {
		owners[runtime] = &workerFixture{runtime: runtime, states: map[string]api.WorkState{}}
	}
	pool := workPool(root, "codex", owners)
	open := func(provider string) *tasks.Manager {
		m, e := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), provider, pool, owners[provider], func() api.Snapshot { return api.Snapshot{CanSend: true} })
		if e != nil {
			t.Fatal(e)
		}
		return m
	}
	m := open("codex")
	in := api.TaskStart{RequestID: "stable-user-request", Title: "original task", Prompt: "Do work"}
	old, e := m.StartTask(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := m.WorkTerminal(t.Context(), old.ID)
	if _, e = pool.InspectLocalWorker(t.Context(), "caelis"); e != nil {
		t.Fatal(e)
	}
	retry, e := m.StartTask(t.Context(), in)
	if e != nil || retry.ID != old.ID || owners["codex"].starts != 1 || owners["caelis"].starts != 0 {
		t.Fatal("retry changed backend", e)
	}
	in.RequestID = "new-user-request"
	next, e := m.StartTask(t.Context(), in)
	if e != nil || owners["caelis"].starts != 1 {
		t.Fatal("new default ignored", e)
	}
	pool = workPool(root, "caelis", owners)
	m = open("caelis")
	if len(m.ListTasks()) != 2 {
		t.Fatal("resident switch hid old local tasks")
	}
	for _, owner := range owners {
		for id, state := range owner.states {
			state.Task.Status = "completed"
			state.Task.Result = "Done"
			owner.states[id] = state
		}
	}
	for range 3 {
		if err := m.DeliverTaskReport(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if owners["caelis"].reports != 2 || owners["codex"].reports != 0 {
		t.Fatal("old completion reports lost or duplicated across resident switch")
	}
	after, e := m.WorkTerminal(t.Context(), old.ID)
	if e != nil || before.Thread != after.Thread || before.Runtime != after.Runtime {
		t.Fatal("original native terminal changed", e)
	}
	if _, e = m.SendTask(t.Context(), api.TaskMessage{ID: old.ID, RequestID: "continue-original", Prompt: "Continue"}); e != nil {
		t.Fatal(e)
	}
	if _, e = m.StopTask(t.Context(), old.ID); e != nil {
		t.Fatal(e)
	}
	if owners["codex"].sends != 1 || owners["codex"].stops != 1 || owners["caelis"].sends != 0 {
		t.Fatal("old task mutated in new runtime")
	}
	if _, e = m.ReadTask(t.Context(), next.ID); e != nil {
		t.Fatal(e)
	}
	owners["caelis"].reject = true
	if _, e = m.SendTask(t.Context(), api.TaskMessage{ID: old.ID, RequestID: "unauthorized-mutation", Prompt: "Continue"}); e == nil || owners["codex"].sends != 1 {
		t.Fatal("retained worker bypassed originating native authority")
	}
	for _, v := range m.TaskPreviews() {
		if v.ID == old.ID && v.Provider != "codex" {
			t.Fatal("Dock displayed resident instead of actual worker runtime")
		}
	}
}

func TestWorkerInspectionDoesNotSelectAndUnreadySelectionPreservesDefault(t *testing.T) {
	root := t.TempDir()
	owners := map[string]*workerFixture{
		"codex":  {runtime: "codex", states: map[string]api.WorkState{}},
		"caelis": {runtime: "caelis", states: map[string]api.WorkState{}},
	}
	pool := workPool(root, "codex", owners)
	if v, err := pool.InspectWorkerRuntime(t.Context(), "caelis"); err != nil || !v.Ready || pool.DefaultRuntime() != "codex" {
		t.Fatalf("inspection changed selection: %+v %v", v, err)
	}
	owners["caelis"].reject = true
	if _, err := pool.SelectLocalWorker(t.Context(), "caelis"); err == nil || pool.DefaultRuntime() != "codex" {
		t.Fatalf("unready selection displaced default: %v", err)
	}
	owners["caelis"].reject = false
	if _, err := pool.SelectLocalWorker(t.Context(), "caelis"); err != nil || pool.DefaultRuntime() != "caelis" {
		t.Fatalf("ready selection failed: %v", err)
	}
}
func TestMissingOriginalLocalRuntimeNeverFallsBack(t *testing.T) {
	root := t.TempDir()
	active := &workerFixture{runtime: "caelis", states: map[string]api.WorkState{}}
	pool := workPool(root, "caelis", map[string]*workerFixture{"caelis": active})
	pool.open = func(string) (retainedWorker, error) { return nil, errors.New("original runtime unavailable") }
	if runtime, e := pool.BindWork(t.Context(), api.TaskStart{}, "original-task", "codex"); e != nil || runtime != "codex" {
		t.Fatal("unavailable original route lost", e)
	}
	if _, e := pool.ReadWork(t.Context(), "original-task"); e == nil {
		t.Fatal("unbound task should fail closed")
	}
	if active.starts != 0 {
		t.Fatal("fell back to default")
	}
}
