package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestTaskHistoryDatedFiltersAndLegacyFallback(t *testing.T) {
	root := t.TempDir()
	f := newRuntime()
	m := openFixture(t, root, "codex", f)
	now := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	old := start(t, m, "history-old")
	now = now.Add(time.Hour)
	newer := start(t, m, "history-new")
	now = now.Add(time.Hour)
	legacy := start(t, m, "history-legacy")
	m.mu.Lock()
	m.state.Records[legacy.ID].CreatedAt = 0
	if err := m.write(); err != nil {
		m.mu.Unlock()
		t.Fatal(err)
	}
	m.mu.Unlock()
	m = openFixture(t, root, "codex", f)
	page, err := m.QueryTaskHistory(api.TaskHistoryQuery{})
	if err != nil || len(page) != 3 || page[0].ID != newer.ID || page[1].ID != old.ID || page[2].ID != legacy.ID || page[2].CreatedAt != "" {
		t.Fatal("dated order or legacy fallback", page, err)
	}
	page, err = m.QueryTaskHistory(api.TaskHistoryQuery{Order: "asc", Offset: 1, Limit: 1})
	if err != nil || len(page) != 1 || page[0].ID != newer.ID {
		t.Fatal("ascending page", page, err)
	}
	page, err = m.QueryTaskHistory(api.TaskHistoryQuery{CreatedAfter: "2026-10-13T09:00:00Z", CreatedBefore: "2026-10-13T10:00:00Z"})
	if err != nil || len(page) != 1 || page[0].ID != newer.ID {
		t.Fatal("inclusive/exclusive time boundary", page, err)
	}
	page, err = m.QueryTaskHistory(api.TaskHistoryQuery{Query: old.ID})
	if err != nil || len(page) != 1 || page[0].ID != old.ID {
		t.Fatal("original prompt/task search", page, err)
	}
	for _, q := range []api.TaskHistoryQuery{{CreatedAfter: "yesterday"}, {CreatedBefore: "2026-10-13T10:00:00"}, {Limit: 51}, {Offset: -1}, {Order: "random"}} {
		if _, err := m.QueryTaskHistory(q); err == nil {
			t.Fatal("invalid query accepted", q)
		}
	}
	if _, err := m.PinTask(legacy.ID, true); err != nil {
		t.Fatal(err)
	}
	if f.starts != 3 || !strings.Contains(m.state.Records[legacy.ID].View.ID, "task-") {
		t.Fatal("pin dispatched native work")
	}
}

type submissionFixture struct {
	*fixtureRuntime
	sends   int
	unknown bool
}

func (f *submissionFixture) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	f.sends++
	if f.unknown {
		state := f.states[in.ID]
		state.Task.Status, state.Task.Outcome = "unknown", "unknown"
		f.states[in.ID] = state
		return state.Task, errors.New("native receipt lost")
	}
	return f.fixtureRuntime.SendWork(ctx, in)
}
func (f *submissionFixture) WorkMessageRecorded(api.TaskMessage) bool { return false }

func TestSubmissionIDsAcceptDistinctSameTextAndNeverReplayUnknown(t *testing.T) {
	f := &submissionFixture{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	m, err := Open(root+"/tasks.json", root+"/Tasks", "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	task := start(t, m, "original-start")
	first := api.TaskMessage{ID: task.ID, RequestID: "native-call-a", Prompt: "same wording"}
	second := api.TaskMessage{ID: task.ID, RequestID: "native-call-b", Prompt: "same wording"}
	if _, err := m.SendTask(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SendTask(t.Context(), second); err != nil || f.sends != 2 {
		t.Fatal("distinct calls were deduplicated", f.sends, err)
	}
	m, err = Open(m.path, m.root, "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.SendTask(t.Context(), first); err != nil || f.sends != 2 {
		t.Fatal("confirmed same call was replayed after restart", f.sends, err)
	}
	changed := first
	changed.Prompt = "changed wording"
	if _, err := m.SendTask(t.Context(), changed); err == nil || f.sends != 2 {
		t.Fatal("original submission identity accepted changed prompt", f.sends, err)
	}
	f.unknown = true
	third := api.TaskMessage{ID: task.ID, RequestID: "native-call-unknown", Prompt: "another prompt"}
	if _, err := m.SendTask(t.Context(), third); err == nil || f.sends != 3 {
		t.Fatal("unknown fixture did not dispatch once", f.sends, err)
	}
	m, err = Open(m.path, m.root, "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := m.SendTask(t.Context(), third); err == nil || out.Status != "unknown" || f.sends != 3 {
		t.Fatal("unknown submission replayed or forged success", out, f.sends, err)
	}
}

type rejectedReceiptFixture struct {
	*fixtureRuntime
	reads int
}

func (f *rejectedReceiptFixture) WorkMessageRecorded(api.TaskMessage) bool { return true }
func (f *rejectedReceiptFixture) SendWork(_ context.Context, in api.TaskMessage) (api.Task, error) {
	f.reads++
	v := f.states[in.ID].Task
	v.Outcome = "rejected"
	return v, nil // original native receipt; this must not dispatch another prompt
}

func TestLegacyBooleanConfirmationReconcilesRejectedNativeReceipt(t *testing.T) {
	f := &rejectedReceiptFixture{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	open := func() *Manager {
		m, err := Open(root+"/tasks.json", root+"/Tasks", "codex", f, f, f.Snapshot)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := open()
	task := start(t, m, "legacy-bool-original")
	in := api.TaskMessage{ID: task.ID, RequestID: "legacy-bool-followup", Prompt: "follow up"}
	m.mu.Lock()
	record := m.state.Records[task.ID]
	record.Requests = append(record.Requests, in.RequestID)
	record.ConfirmedRequests = map[string]bool{in.RequestID: true}
	if err := m.write(); err != nil {
		m.mu.Unlock()
		t.Fatal(err)
	}
	m.mu.Unlock()
	for i := 0; i < 2; i++ {
		got, err := m.SendTask(t.Context(), in)
		if err != nil || got.Outcome != "rejected" || f.reads != 1 {
			t.Fatalf("legacy boolean forged acceptance or replayed: %+v %v reads=%d", got, err, f.reads)
		}
	}
	m = open()
	got, err := m.SendTask(t.Context(), in)
	if err != nil || got.Outcome != "rejected" || f.reads != 1 {
		t.Fatal("restart changed reconciled rejected outcome", got, err, f.reads)
	}
}
