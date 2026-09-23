package tasks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
)

type fixtureRuntime struct {
	states                      map[string]api.WorkState
	starts, reports             int
	admit                       error
	unknownStart, unknownReport bool
	snapshot                    api.Snapshot
	lastStart                   api.WorkStart
}

func newRuntime() *fixtureRuntime {
	return &fixtureRuntime{states: map[string]api.WorkState{}, snapshot: api.Snapshot{CanSend: true}}
}
func (f *fixtureRuntime) WorkAdmission(context.Context) error { return f.admit }
func (f *fixtureRuntime) WorkStates() []api.WorkState {
	out := []api.WorkState{}
	for _, s := range f.states {
		out = append(out, s)
	}
	return out
}
func (f *fixtureRuntime) StartWork(_ context.Context, in api.WorkStart) (api.Task, error) {
	f.starts++
	f.lastStart = in
	v := api.Task{ID: in.ID, Title: in.Title, Workspace: in.Workspace, Status: "working", Outcome: "accepted"}
	if f.unknownStart {
		v.Status = "unknown"
		v.Outcome = "unknown"
	}
	f.states[v.ID] = api.WorkState{Task: v, ExecutionKey: "native-turn-1"}
	if f.unknownStart {
		return v, errors.New("lost native receipt")
	}
	return v, nil
}
func (f *fixtureRuntime) ReadWork(_ context.Context, id string) (api.Task, error) {
	return f.states[id].Task, nil
}
func (f *fixtureRuntime) SendWork(_ context.Context, in api.TaskMessage) (api.Task, error) {
	s := f.states[in.ID]
	s.Task.Status = "working"
	s.Task.Outcome = "accepted"
	s.ExecutionKey = "native-turn-2"
	f.states[in.ID] = s
	return s.Task, nil
}
func (f *fixtureRuntime) StopWork(_ context.Context, id string) (api.Task, error) {
	s := f.states[id]
	s.StopRequested = true
	f.states[id] = s
	return s.Task, nil
}
func (f *fixtureRuntime) SubmitReport(_ context.Context, in api.Submission) (api.Receipt, error) {
	f.reports++
	out := api.Receipt{ID: in.ID, Outcome: "accepted"}
	if f.unknownReport {
		out.Outcome = "unknown"
	}
	f.snapshot.LastReceipt = out
	return out, nil
}
func (f *fixtureRuntime) complete(id string) {
	s := f.states[id]
	s.Task.Status = "completed"
	s.Task.Result = "Synthetic result"
	f.states[id] = s
}
func (f *fixtureRuntime) Snapshot() api.Snapshot { return f.snapshot }
func openFixture(t *testing.T, root, provider string, f *fixtureRuntime) *Manager {
	t.Helper()
	m, e := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), provider, f, f, f.Snapshot)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func input(id string) api.TaskStart {
	return api.TaskStart{RequestID: id, Title: "Synthetic", Prompt: "Produce an artifact in the assigned workspace."}
}
func start(t *testing.T, m *Manager, id string) api.Task {
	t.Helper()
	v, e := m.StartTask(t.Context(), input(id))
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestSameProductPolicyForDifferentRuntimeAdapters(t *testing.T) {
	for _, provider := range []string{"codex", "generic-fixture"} {
		t.Run(provider, func(t *testing.T) {
			f := newRuntime()
			root := t.TempDir()
			m := openFixture(t, root, provider, f)
			f.admit = errors.New("no native user activation")
			if _, e := m.StartTask(t.Context(), input("unauthorized")); e == nil {
				t.Fatal("admission bypassed")
			}
			if _, e := os.Stat(filepath.Join(root, "Tasks")); !os.IsNotExist(e) {
				t.Fatal("allocated before admission")
			}
			f.admit = nil
			v := start(t, m, "first-request")
			if f.lastStart.Instructions != botpolicy.WorkerInstructions || f.lastStart.Workspace != v.Workspace {
				t.Fatal("host did not own role/workspace")
			}
			info, e := os.Stat(v.Workspace)
			if e != nil || info.Mode().Perm() != 0700 {
				t.Fatal("workspace not private", e)
			}
			if again := start(t, m, "first-request"); again.ID != v.ID || f.starts != 1 {
				t.Fatal("duplicate start")
			}
			different := input("first-request")
			different.Prompt = "Different assignment"
			if _, e = m.StartTask(t.Context(), different); e == nil {
				t.Fatal("conflicting request admitted")
			}
			start(t, m, "second-request")
			start(t, m, "third-request")
			if _, e = m.StartTask(t.Context(), input("fourth-request")); e == nil || f.starts != 3 {
				t.Fatal("capacity escaped")
			}
			if _, e = m.ReadTask(t.Context(), "native-foreign-id"); e == nil {
				t.Fatal("foreign task admitted")
			}
		})
	}
}
func TestUnknownCreationPersistsWithoutRedispatch(t *testing.T) {
	root := t.TempDir()
	f := newRuntime()
	f.unknownStart = true
	m := openFixture(t, root, "fixture", f)
	v, e := m.StartTask(t.Context(), input("uncertain-request"))
	if e == nil || v.Outcome != "unknown" {
		t.Fatal(v, e)
	}
	m = openFixture(t, root, "fixture", f)
	if _, e = m.StartTask(t.Context(), input("uncertain-request")); e != nil || f.starts != 1 {
		t.Fatal("uncertain create replayed", e)
	}
}
func TestReportPersistsAndUnknownDeliveryNeverReplays(t *testing.T) {
	root := t.TempDir()
	f := newRuntime()
	m := openFixture(t, root, "fixture", f)
	v := start(t, m, "report-request")
	f.complete(v.ID)
	f.unknownReport = true
	if e := m.DeliverTaskReport(t.Context()); e != nil {
		t.Fatal(e)
	}
	id := f.snapshot.LastReceipt.ID
	m = openFixture(t, root, "fixture", f)
	for range 3 {
		if e := m.DeliverTaskReport(t.Context()); e != nil {
			t.Fatal(e)
		}
	}
	if f.reports != 1 {
		t.Fatal("unknown delivery replayed")
	}
	f.snapshot.LastReceipt = api.Receipt{ID: id, Outcome: "accepted"}
	if e := m.DeliverTaskReport(t.Context()); e != nil {
		t.Fatal(e)
	}
	if m.state.Records[v.ID].ReportState != "delivered" || f.reports != 1 {
		t.Fatal("receipt not reconciled")
	}
}
func TestReadAndStopSuppressReportsButNewTurnReportsOnce(t *testing.T) {
	f := newRuntime()
	m := openFixture(t, t.TempDir(), "fixture", f)
	v := start(t, m, "read-request")
	f.complete(v.ID)
	if _, e := m.ReadTask(t.Context(), v.ID); e != nil {
		t.Fatal(e)
	}
	if e := m.DeliverTaskReport(t.Context()); e != nil {
		t.Fatal(e)
	}
	if f.reports != 0 {
		t.Fatal("observed result reported again")
	}
	if _, e := m.SendTask(t.Context(), api.TaskMessage{ID: v.ID, RequestID: "new-request", Prompt: "Continue."}); e != nil {
		t.Fatal(e)
	}
	f.complete(v.ID)
	if e := m.DeliverTaskReport(t.Context()); e != nil {
		t.Fatal(e)
	}
	if e := m.DeliverTaskReport(t.Context()); e != nil {
		t.Fatal(e)
	}
	if f.reports != 1 {
		t.Fatal("new native generation not reported once")
	}
	other := start(t, m, "stop-request")
	if _, e := m.StopTask(t.Context(), other.ID); e != nil {
		t.Fatal(e)
	}
	f.complete(other.ID)
	_ = m.DeliverTaskReport(t.Context())
	if f.reports != 1 {
		t.Fatal("stopped task woke secretary")
	}
}
func TestFailedPersistenceCannotDispatchAndIsRetried(t *testing.T) {
	f := newRuntime()
	m := openFixture(t, t.TempDir(), "fixture", f)
	m.write = func() error { return os.ErrPermission }
	if _, e := m.StartTask(t.Context(), input("blocked-request")); e == nil || f.starts != 0 {
		t.Fatal("created despite failed persistence")
	}
	m.write = m.save
	v := start(t, m, "valid-request")
	f.complete(v.ID)
	m.write = func() error { return os.ErrPermission }
	if e := m.DeliverTaskReport(t.Context()); e == nil || f.reports != 0 {
		t.Fatal("reported before persistence")
	}
	if e := m.DeliverTaskReport(t.Context()); e == nil || f.reports != 0 {
		t.Fatal("dirty in-memory state bypassed retry")
	}
	m.write = m.save
	if e := m.DeliverTaskReport(t.Context()); e != nil || f.reports != 1 {
		t.Fatal("could not recover persistence", e)
	}
}
func TestNativeReportImportAndLegacyRequestDoNotDuplicate(t *testing.T) {
	root := t.TempDir()
	f := newRuntime()
	in := input("legacy-request")
	id := "task-" + hash(in.RequestID)
	f.states[id] = api.WorkState{Task: api.Task{ID: id, Status: "completed", Outcome: "accepted"}, ExecutionKey: "old-generation", PreviousReportID: "old-report-id", PreviousReportState: "delivered", StartFingerprint: hash(in.Title, in.Prompt)}
	m := openFixture(t, root, "codex", f)
	if v := start(t, m, in.RequestID); v.ID != id || f.starts != 0 {
		t.Fatal("legacy request recreated")
	}
	if e := m.DeliverTaskReport(t.Context()); e != nil || f.reports != 0 {
		t.Fatal("legacy completion redelivered", e)
	}
}
func TestProviderSwitchPreservesLedgerWithoutAdoptingWork(t *testing.T) {
	root := t.TempDir()
	a := newRuntime()
	ma := openFixture(t, root, "a", a)
	v := start(t, ma, "same-request")
	b := newRuntime()
	mb := openFixture(t, root, "b", b)
	other := start(t, mb, "same-request")
	if v.ID == other.ID || len(mb.ListTasks()) != 1 {
		t.Fatal("provider identity collision")
	}
	if _, e := mb.ReadTask(t.Context(), v.ID); e == nil {
		t.Fatal("foreign runtime task used")
	}
	ma = openFixture(t, root, "a", a)
	if len(ma.state.Records) != 2 || len(ma.ListTasks()) != 1 {
		t.Fatal("switch lost product records")
	}
}
func TestWorkspaceNeverAdoptsExistingOrSymlinkDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Tasks")
	if _, e := prepareWorkspace(root, "one"); e != nil {
		t.Fatal(e)
	}
	if _, e := prepareWorkspace(root, "one"); e == nil {
		t.Fatal("adopted existing directory")
	}
	link := filepath.Join(t.TempDir(), "redirect")
	if e := os.Symlink(t.TempDir(), link); e != nil {
		t.Fatal(e)
	}
	if _, e := prepareWorkspace(link, "two"); e == nil {
		t.Fatal("followed symlink")
	}
}
