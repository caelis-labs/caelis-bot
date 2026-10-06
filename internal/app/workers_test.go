package app

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"path/filepath"
	"testing"
)

type workerFixture struct {
	reports                                   int
	runtime                                   string
	states                                    map[string]api.WorkState
	starts, sends, stops, connections, closed int
	reject                                    bool
}

type detachableWorkerFixture struct {
	*workerFixture
	detached int
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
