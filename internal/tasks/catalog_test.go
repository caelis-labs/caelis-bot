package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestUnlimitedHistoryStablePagesAndSearch(t *testing.T) {
	f := newRuntime()
	m := openFixture(t, t.TempDir(), "codex", f)
	for i := 0; i < 105; i++ {
		v, e := m.StartTask(t.Context(), input(fmt.Sprintf("history-%03d", i)))
		if e != nil {
			t.Fatal(i, e)
		}
		f.complete(v.ID)
	}
	first, e := m.QueryTasks(api.TaskQuery{Limit: 20})
	if e != nil || first.Total != 105 || len(first.Tasks) != 20 || first.NextCursor == "" {
		t.Fatal(first, e)
	}
	seen := map[string]bool{}
	for _, v := range first.Tasks {
		seen[v.ID] = true
		if v.Pinned {
			t.Fatal("new task pinned")
		}
	}
	newer, e := m.StartTask(t.Context(), api.TaskStart{RequestID: "new-history-item", Title: "Needle", Prompt: "Read a unique MATCH in history"})
	if e != nil {
		t.Fatal(e)
	}
	cursor := first.NextCursor
	for cursor != "" {
		page, e := m.QueryTasks(api.TaskQuery{Limit: 20, Cursor: cursor})
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range page.Tasks {
			if seen[v.ID] || v.ID == newer.ID {
				t.Fatal("page duplicated or shifted", v)
			}
			seen[v.ID] = true
		}
		cursor = page.NextCursor
	}
	if len(seen) != 105 {
		t.Fatal("history omitted", len(seen))
	}
	page, e := m.QueryTasks(api.TaskQuery{Query: "  unique match  "})
	if e != nil || len(page.Tasks) != 1 || page.Tasks[0].ID != newer.ID {
		t.Fatal(page, e)
	}
	if _, e = m.QueryTasks(api.TaskQuery{Query: "different", Cursor: first.NextCursor}); e == nil {
		t.Fatal("cursor reused with another search")
	}
	for _, q := range []api.TaskQuery{{Limit: -1}, {Limit: 51}, {Cursor: "bad"}} {
		if _, e = m.QueryTasks(q); e == nil {
			t.Fatal("bad query accepted")
		}
	}
	other := openFixture(t, filepath.Dir(m.path), "other", newRuntime())
	page, e = other.QueryTasks(api.TaskQuery{})
	if e != nil || page.Total != 0 {
		t.Fatal("provider history leaked", page, e)
	}
}

func TestWatchlistPersistsAndDoesNotControlExecution(t *testing.T) {
	f := &terminalFixture{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	m, e := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if e != nil {
		t.Fatal(e)
	}
	var ids []string
	for i := 0; i < 9; i++ {
		v, e := m.StartTask(t.Context(), input(fmt.Sprintf("pin-task-%d", i)))
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, v.ID)
		f.complete(v.ID)
	}
	changes := 0
	m.ObserveWatchlist(func(v []api.TaskPreview) {
		changes++
		if len(v) > PinnedLimit {
			t.Fatal("overflow")
		}
	})
	for _, id := range ids[:8] {
		if _, e = m.PinTask(id, true); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = m.PinTask(ids[8], true); e == nil {
		t.Fatal("silent eviction")
	}
	if _, e = m.PinTask(ids[0], true); e != nil || changes != 0 {
		t.Fatal("pin not idempotent", e, changes)
	}
	if _, e = m.PinTask("foreign", true); e == nil {
		t.Fatal("foreign pin")
	}
	if _, e = m.PinTask(ids[0], false); e != nil {
		t.Fatal(e)
	}
	if _, e = m.PinTask(ids[8], true); e != nil {
		t.Fatal(e)
	}
	if f.starts != 9 || f.states[ids[0]].StopRequested {
		t.Fatal("pin changed execution")
	}
	reopened, e := Open(m.path, m.root, "codex", f, f, f.Snapshot)
	if e != nil {
		t.Fatal(e)
	}
	if len(reopened.TaskPreviews()) != 8 {
		t.Fatal("pins lost after restart")
	}
	no := false
	page, e := reopened.QueryTasks(api.TaskQuery{Pinned: &no})
	if e != nil || page.Total != 1 || page.Tasks[0].ID != ids[0] {
		t.Fatal(page, e)
	}
	originalWrite, writes := reopened.write, 0
	reopened.write = func() error {
		writes++
		if writes == 1 {
			return originalWrite()
		}
		return errors.New("disk full")
	}
	if _, e = reopened.PinTask(ids[1], false); e == nil || len(reopened.TaskPreviews()) != 8 {
		t.Fatal("failed save changed pins")
	}
}

func TestLegacyWatchlistMigrationKeepsHistoryWithoutFloodingDock(t *testing.T) {
	root := t.TempDir()
	saved := state{Version: 1, Records: map[string]*record{}}
	for i := 0; i < 15; i++ {
		id := fmt.Sprintf("legacy-%02d", i)
		status := "working"
		if i >= 10 {
			status = "completed"
		}
		saved.Records[id] = &record{Provider: "codex", View: api.Task{ID: id, Status: status}}
	}
	b, _ := json.Marshal(saved)
	path := filepath.Join(root, "tasks.json")
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	f := &terminalFixture{fixtureRuntime: newRuntime()}
	m, e := Open(path, filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if e != nil {
		t.Fatal(e)
	}
	if len(m.TaskPreviews()) != 8 {
		t.Fatal("unbounded migration")
	}
	p, e := m.QueryTasks(api.TaskQuery{})
	if e != nil || p.Total != 15 || len(p.Tasks) != 15 {
		t.Fatal("history removed", p, e)
	}
	seen := map[int64]bool{}
	for _, r := range m.state.Records {
		if r.Sequence <= 0 || seen[r.Sequence] {
			t.Fatal("unstable ordering")
		}
		seen[r.Sequence] = true
	}
}

type replayRuntime struct {
	*fixtureRuntime
	recorded bool
	sends    int
}

func (f *replayRuntime) WorkMessageRecorded(api.TaskMessage) bool { return f.recorded }
func (f *replayRuntime) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	f.sends++
	if f.recorded {
		return f.states[in.ID].Task, nil
	}
	return f.fixtureRuntime.SendWork(ctx, in)
}
func TestConfigurableAdmissionCoversResumeButAllowsReceiptReconciliation(t *testing.T) {
	f := &replayRuntime{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	m, e := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if e != nil {
		t.Fatal(e)
	}
	limit := 4
	m.ConfigureLimit(func() int { return limit })
	var ids []string
	for i := 0; i < 4; i++ {
		v, e := m.StartTask(t.Context(), input(fmt.Sprintf("limit-%03d", i)))
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, v.ID)
	}
	if _, e = m.StartTask(t.Context(), input("over-limit")); e == nil {
		t.Fatal("limit ignored")
	}
	limit = 1
	if _, e = m.SendTask(t.Context(), api.TaskMessage{ID: ids[0], RequestID: "steering-request", Prompt: "Continue this work"}); e != nil {
		t.Fatal("steering used a new slot", e)
	}
	f.complete(ids[0])
	in := api.TaskMessage{ID: ids[0], RequestID: "resume-request", Prompt: "More work"}
	if _, e = m.SendTask(t.Context(), in); e == nil {
		t.Fatal("resumed past limit")
	}
	f.recorded = true
	if _, e = m.SendTask(t.Context(), in); e != nil || f.states[ids[0]].Task.Status != "completed" {
		t.Fatal("receipt reconciliation blocked", e)
	}
	f.recorded = false
	for _, id := range ids[1:] {
		f.complete(id)
	}
	if _, e = m.SendTask(t.Context(), in); e != nil {
		t.Fatal("slot not released", e)
	}
	if _, e = m.StartTask(t.Context(), input("limit-001")); e != nil {
		t.Fatal("same start receipt blocked", e)
	}
}
