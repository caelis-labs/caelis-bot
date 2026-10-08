package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestWatchlistObserverCanChangeDuringRefresh(t *testing.T) {
	m := openFixture(t, t.TempDir(), "codex", newRuntime())
	var deliveries atomic.Int64
	observer := func([]api.TaskPreview) { deliveries.Add(1) }
	m.ObserveWatchlist(observer)
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 200 {
			m.ObserveWatchlist(nil)
			m.ObserveWatchlist(observer)
		}
	})
	wg.Go(func() {
		for range 200 {
			if err := m.RefreshWatchlist(); err != nil {
				t.Error(err)
				return
			}
		}
	})
	wg.Wait()
	m.ObserveWatchlist(observer)
	before := deliveries.Load()
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	if deliveries.Load() != before+1 {
		t.Fatal("current observer did not receive the refreshed watchlist")
	}
}

func TestUnknownTaskAttentionKeepsOriginalExecutionAndDoesNotReplay(t *testing.T) {
	root := t.TempDir()
	f := newRuntime()
	m := openFixture(t, root, "codex", f)
	now := time.Now().UTC()
	m.now = func() time.Time { return now }
	v := start(t, m, "unknown-attention")
	state := f.states[v.ID]
	state.Task.Status = "unknown"
	f.states[v.ID] = state
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	if got, err := m.ClaimUnknownNotices(); err != nil || len(got) != 0 {
		t.Fatal("announced during recovery grace", got, err)
	}
	now = now.Add(31 * time.Second)
	if got, err := m.ClaimUnknownNotices(); err != nil || len(got) != 1 || got[0] != v.ID {
		t.Fatal("missing original task alert", got, err)
	}
	restored := openFixture(t, root, "codex", f)
	restored.now = func() time.Time { return now }
	if got, err := restored.ClaimUnknownNotices(); err != nil || len(got) != 0 {
		t.Fatal("replayed an old unknown alert", got, err)
	}
	state.Task.Status = "working"
	f.states[v.ID] = state
	if err := restored.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	state.Task.Status = "unknown"
	f.states[v.ID] = state
	now = now.Add(31 * time.Second)
	if got, err := restored.ClaimUnknownNotices(); err != nil || len(got) != 0 {
		t.Fatal("same execution alerted twice", got, err)
	}
	state.ExecutionKey = "new-native-execution"
	f.states[v.ID] = state
	if err := restored.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	if got, err := restored.ClaimUnknownNotices(); err != nil || len(got) != 1 || got[0] != v.ID {
		t.Fatal("new execution was not alerted", got, err)
	}
	old := state
	old.ExecutionKey = "native-turn-1"
	old.Task.Status = "completed"
	f.states[v.ID] = old // A late A result must not replace B or reopen its alert.
	if err := restored.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	if got, err := restored.ClaimUnknownNotices(); err != nil || len(got) != 0 {
		t.Fatal("late old outcome created another alert", got, err)
	}
	if r := restored.state.Records[v.ID]; r.Execution != "new-native-execution" || r.View.Status != "unknown" {
		t.Fatal("late old outcome replaced the current execution", r.Execution, r.View.Status)
	}
	if f.starts != 1 || f.reports != 0 {
		t.Fatal("alert dispatched or reported work", f.starts, f.reports)
	}
}

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
		if !v.Pinned {
			t.Fatal("new task missing from background list")
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

	})
	for _, id := range ids[:8] {
		if _, e = m.PinTask(id, true); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = m.PinTask(ids[8], true); e != nil {
		t.Fatal("display capacity hid task", e)
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
	if len(m.TaskPreviews()) != 10 {
		t.Fatal("active legacy work excluded")
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

func TestBackgroundListLifecycle(t *testing.T) {
	for _, provider := range []string{"codex", "caelis"} {
		t.Run(provider, func(t *testing.T) {
			f := &terminalFixture{fixtureRuntime: newRuntime()}
			root := t.TempDir()
			m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), provider, f, f, f.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			m.now = func() time.Time { return now }
			m.ConfigureLimit(func() int { return 12 })
			var ids []string
			for i := 0; i < 12; i++ {
				ids = append(ids, start(t, m, fmt.Sprintf("background-%02d", i)).ID)
				now = now.Add(time.Second)
			}
			previews := m.TaskPreviews()
			if len(previews) != 12 || previews[0].ID != ids[11] {
				t.Fatal("running work omitted or misordered", previews)
			}
			if _, err = m.LockTask(ids[0], true); err != nil {
				t.Fatal(err)
			}
			for _, id := range ids[:10] {
				f.complete(id)
			}
			if err = m.RefreshWatchlist(); err != nil {
				t.Fatal(err)
			}
			previews = m.TaskPreviews()
			if len(previews) != 12 || previews[0].Status != "working" || previews[1].Status != "working" {
				t.Fatal("recent completions or active priority lost", previews)
			}
			now = now.Add(CompletedRetention - time.Second)
			if err = m.RefreshWatchlist(); err != nil || len(m.TaskPreviews()) != 12 {
				t.Fatal("premature cleanup", err)
			}
			now = now.Add(2 * time.Second)
			if err = m.RefreshWatchlist(); err != nil || len(m.TaskPreviews()) != 3 {
				t.Fatal("completed items not aged out", err)
			}
			if err = m.ClearTasks(); err != nil {
				t.Fatal(err)
			}
			previews = m.TaskPreviews()
			if len(previews) != 1 || previews[0].ID != ids[0] || !previews[0].Locked {
				t.Fatal("clear lost lock", previews)
			}
			if err = m.RefreshWatchlist(); err != nil || len(m.TaskPreviews()) != 1 {
				t.Fatal("poll undid clear", err)
			}
			// Reopen preserves both lock and manual removal.
			reopened, err := Open(m.path, m.root, provider, f, f, f.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			reopened.now = m.now
			if len(reopened.TaskPreviews()) != 1 {
				t.Fatal("restart undid dismissal")
			}
			msg := api.TaskMessage{ID: ids[1], RequestID: "resume-a-dismissed-task", Prompt: "Continue"}
			if _, err = reopened.SendTask(t.Context(), msg); err != nil {
				t.Fatal(err)
			}
			if len(reopened.TaskPreviews()) != 2 || reopened.TaskPreviews()[0].ID != ids[1] {
				t.Fatal("new execution not repinned")
			}
			if _, err = reopened.PinTask(ids[1], false); err != nil {
				t.Fatal(err)
			}
			// Reconciliation of the same native generation is not a fresh resume.
			if _, err = reopened.SendTask(t.Context(), msg); err != nil || len(reopened.TaskPreviews()) != 1 {
				t.Fatal("same execution repinned", err)
			}
			if _, err = reopened.LockTask(ids[0], false); err != nil {
				t.Fatal(err)
			}
			now = now.Add(CompletedRetention + time.Second)
			if err = reopened.RefreshWatchlist(); err != nil || len(reopened.TaskPreviews()) != 0 {
				t.Fatal("unlock never aged", err)
			}
			page, err := reopened.QueryTasks(api.TaskQuery{})
			if err != nil || page.Total != 12 || f.starts != 12 {
				t.Fatal("cleanup affected history/execution", page, err)
			}
			for _, state := range f.states {
				if state.StopRequested {
					t.Fatal("cleanup canceled worker")
				}
			}
		})
	}
}

func TestBackgroundChangesRollbackWhenStorageFails(t *testing.T) {
	f := &terminalFixture{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	id := start(t, m, "storage-background-task").ID
	original := m.write
	failMutation := func() {
		n := 0
		m.write = func() error {
			n++
			if n == 1 {
				return original()
			}
			return errors.New("disk full")
		}
	}
	failMutation()
	if _, err = m.LockTask(id, true); err == nil || m.TaskPreviews()[0].Locked {
		t.Fatal("failed lock survived", err)
	}
	m.write = original
	if _, err = m.LockTask(id, true); err != nil {
		t.Fatal(err)
	}
	failMutation()
	if _, err = m.LockTask(id, false); err == nil || !m.TaskPreviews()[0].Locked {
		t.Fatal("failed unlock lost lock", err)
	}
	m.write = original
	if _, err = m.LockTask(id, false); err != nil {
		t.Fatal(err)
	}
	failMutation()
	if err = m.ClearTasks(); err == nil || len(m.TaskPreviews()) != 1 {
		t.Fatal("failed clear lost item", err)
	}
}

func TestManualWatchOrderPersistsWithoutReorderingHistory(t *testing.T) {
	f := &terminalFixture{fixtureRuntime: newRuntime()}
	root := t.TempDir()
	m, err := Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := 0; i < 3; i++ {
		v, err := m.StartTask(t.Context(), input(fmt.Sprintf("manual-order-%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, v.ID)
		f.complete(v.ID)
	}
	page, _ := m.QueryTasks(api.TaskQuery{Limit: 2})
	if err := m.MoveTask(ids[0], ids[2]); err != nil {
		t.Fatal(err)
	}
	if got := m.TaskPreviews(); got[0].ID != ids[0] || got[1].ID != ids[2] {
		t.Fatal(got)
	}
	m, err = Open(m.path, m.root, "codex", f, f, f.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.TaskPreviews(); got[0].ID != ids[0] {
		t.Fatal("order lost on restart", got)
	}
	next, err := m.QueryTasks(api.TaskQuery{Limit: 2, Cursor: page.NextCursor})
	if err != nil || len(next.Tasks) != 1 || next.Tasks[0].ID != ids[0] {
		t.Fatal("history cursor reordered", next, err)
	}
	// A new native generation returns to the front, even after explicit removal.
	if _, err := m.PinTask(ids[1], false); err != nil {
		t.Fatal(err)
	}
	state := f.states[ids[1]]
	state.Task.Status = "working"
	state.ExecutionKey = "next-run"
	f.states[ids[1]] = state
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	if got := m.TaskPreviews(); got[0].ID != ids[1] {
		t.Fatal("new run missing from front", got)
	}
	old := m.TaskPreviews()
	writes := 0
	m.write = func() error {
		writes++
		if writes > 1 {
			return errors.New("disk full")
		}
		return nil
	}
	if err := m.MoveTask(ids[0], ids[1]); err == nil {
		t.Fatal("persistence failure hidden")
	}
	if got := m.TaskPreviews(); got[0].ID != old[0].ID {
		t.Fatal("failed move changed order", got)
	}
}
