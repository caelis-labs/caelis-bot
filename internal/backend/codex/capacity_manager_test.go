package codex

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

// Product assembly routes local work through a runtime-tagged owner. Keep that
// ledger shape in the waiting/stop test while still using the real Codex session.
type taggedCodexRouter struct{ *Session }

func (taggedCodexRouter) BindWork(context.Context, api.TaskStart, string, string) (string, error) {
	return "codex", nil
}
func (taggedCodexRouter) OwnsWork(runtime string) bool { return runtime == "codex" }

type managerResult struct {
	task api.Task
	err  error
}

func managerCall[T any](t *testing.T, label string, call func() (T, error)) (T, error) {
	t.Helper()
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := call()
		done <- result{value, err}
	}()
	select {
	case got := <-done:
		return got.value, got.err
	case <-time.After(time.Second):
		t.Fatalf("%s blocked behind capacity backoff", label)
		var zero T
		return zero, context.DeadlineExceeded
	}
}

func waitManagerCapacity(t *testing.T, s *Session) api.Task {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		for _, state := range s.WorkStates() {
			if state.Task.Status == "pending" && strings.Contains(state.Task.Result, "正在等待") {
				return state.Task
			}
		}
		select {
		case <-deadline:
			t.Fatal("native worker did not enter capacity backoff")
		case <-time.After(time.Millisecond):
		}
	}
}

func awaitManagerResult(t *testing.T, label string, done <-chan managerResult) managerResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not settle", label)
		return managerResult{}
	}
}

func TestManagerCapacityStartRemainsReadableAndStopCancelsOriginalReceipt(t *testing.T) {
	s, f, d, _ := taskPair(t)
	m, err := tasks.Open(filepath.Join(t.TempDir(), "routed-tasks.json"), s.workRoot(), "codex", taggedCodexRouter{s}, s, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	sendSynthetic(t, s, "manager-capacity-parent")
	s.capacityBackoff = func(int) time.Duration { return time.Hour }
	request := "manager-capacity-start"
	var mu sync.Mutex
	var ids []string
	f.mu.Lock()
	original := f.handle
	f.handle = func(msg wireMessage) (any, bool) {
		if msg.Method == "turn/start" {
			var params struct {
				ClientID string `json:"clientUserMessageId"`
			}
			_ = json.Unmarshal(msg.Params, &params)
			mu.Lock()
			ids = append(ids, params.ClientID)
			mu.Unlock()
			return capacityRejection(), true
		}
		return original(msg)
	}
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	start := api.TaskStart{RequestID: request, Title: "Capacity task", Prompt: "Synthetic task"}
	first, duplicate := make(chan managerResult, 1), make(chan managerResult, 1)
	go func() { v, err := m.StartTask(ctx, start); first <- managerResult{v, err} }()
	pending := waitManagerCapacity(t, s)
	go func() { v, err := m.StartTask(ctx, start); duplicate <- managerResult{v, err} }()
	list, err := managerCall(t, "ListTasks", func() ([]api.Task, error) { return m.ListTasks(), nil })
	if err != nil || len(list) != 1 || list[0].ID != pending.ID || list[0].Status != "pending" {
		t.Fatal("task list lost waiting worker", list, err)
	}
	page, err := managerCall(t, "QueryTasks", func() (api.TaskPage, error) { return m.QueryTasks(api.TaskQuery{}) })
	if err != nil || len(page.Tasks) != 1 || page.Tasks[0].Status != "pending" {
		t.Fatal("catalog lost waiting worker", page, err)
	}
	history, err := managerCall(t, "QueryTaskHistory", func() ([]api.TaskHistoryEntry, error) {
		return m.QueryTaskHistory(api.TaskHistoryQuery{})
	})
	if err != nil || len(history) != 1 || history[0].ID != pending.ID || history[0].Status != "pending" {
		t.Fatal("history lost waiting worker", history, err)
	}
	read, err := managerCall(t, "ReadTask", func() (api.Task, error) { return m.ReadTask(ctx, pending.ID) })
	if err != nil || read.Status != "pending" || !strings.Contains(read.Result, "正在等待") {
		t.Fatal("task read lost capacity wait", read, err)
	}
	receipt, err := managerCall(t, "ReadTaskRequest", func() (api.Task, error) { return m.ReadTaskRequest(ctx, request) })
	if err != nil || receipt.ID != pending.ID || receipt.Status != "pending" {
		t.Fatal("original request lookup blocked or changed", receipt, err)
	}
	if _, err := managerCall(t, "RefreshWatchlist", func() (struct{}, error) { return struct{}{}, m.RefreshWatchlist() }); err != nil {
		t.Fatal(err)
	}
	stopped, err := managerCall(t, "StopTask", func() (api.Task, error) { return m.StopTask(ctx, pending.ID) })
	if err != nil || stopped.ID != pending.ID {
		t.Fatal("stop did not cancel waiting worker", stopped, err)
	}
	settled := awaitManagerResult(t, "start", first)
	if settled.task.ID != pending.ID || settled.task.Outcome != "rejected" || settled.err == nil {
		t.Fatal("canceled start did not retain rejection", settled)
	}
	again := awaitManagerResult(t, "duplicate start", duplicate)
	if again.err != nil || again.task.ID != pending.ID || again.task.Outcome != "rejected" {
		t.Fatal("duplicate start changed original result", again)
	}
	receipt, err = m.ReadTaskRequest(ctx, request)
	if err != nil || receipt.ID != pending.ID || receipt.Outcome != "rejected" || !strings.Contains(receipt.Result, "取消") {
		t.Fatal("original receipt did not settle after cancellation", receipt, err)
	}
	mu.Lock()
	gotIDs := append([]string(nil), ids...)
	mu.Unlock()
	f.mu.Lock()
	starts, sends := d.starts, d.sends
	f.mu.Unlock()
	if len(gotIDs) != 1 || gotIDs[0] != request || starts != 1 || sends != 0 {
		t.Fatal("cancellation or duplicate start replayed native turn", gotIDs, starts, sends)
	}
}

func TestManagerCapacityContinueStopAndDuplicateReceipt(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "manager-continue-parent")
	initial := newTask(t, m, "manager-continue-initial")
	finishTask(s, f, initial.ID, "completed")
	deadline := time.After(2 * time.Second)
	for {
		if v, _ := m.ReadTask(testContext(t), initial.ID); v.Status == "completed" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("initial worker did not finish")
		case <-time.After(time.Millisecond):
		}
	}
	s.capacityBackoff = func(int) time.Duration { return time.Hour }
	request := "manager-continue-capacity"
	var mu sync.Mutex
	var ids []string
	f.mu.Lock()
	original := f.handle
	f.handle = func(msg wireMessage) (any, bool) {
		if msg.Method == "turn/start" {
			var params struct {
				ClientID string `json:"clientUserMessageId"`
			}
			_ = json.Unmarshal(msg.Params, &params)
			if params.ClientID == request {
				mu.Lock()
				ids = append(ids, params.ClientID)
				mu.Unlock()
				return capacityRejection(), true
			}
		}
		return original(msg)
	}
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	message := api.TaskMessage{ID: initial.ID, RequestID: request, Prompt: "Continue synthetic task"}
	first, duplicate := make(chan managerResult, 1), make(chan managerResult, 1)
	go func() { v, err := m.SendTask(ctx, message); first <- managerResult{v, err} }()
	pending := waitManagerCapacity(t, s)
	if pending.ID != initial.ID {
		t.Fatal("continuation changed task handle", pending.ID, initial.ID)
	}
	go func() { v, err := m.SendTask(ctx, message); duplicate <- managerResult{v, err} }()
	if view, err := managerCall(t, "continuation ReadTaskRequest", func() (api.Task, error) { return m.ReadTaskRequest(ctx, request) }); err != nil || view.Status != "pending" {
		t.Fatal("continuation receipt not visible while waiting", view, err)
	}
	if _, err := managerCall(t, "continuation StopTask", func() (api.Task, error) { return m.StopTask(ctx, initial.ID) }); err != nil {
		t.Fatal("continuation stop failed", err)
	}
	settled := awaitManagerResult(t, "continuation", first)
	again := awaitManagerResult(t, "duplicate continuation", duplicate)
	if settled.task.ID != initial.ID || settled.task.Outcome != "rejected" || settled.err == nil || again.err != nil || again.task.ID != initial.ID || again.task.Outcome != "rejected" {
		t.Fatal("continuation receipt did not settle once", settled, again)
	}
	mu.Lock()
	count := len(ids)
	mu.Unlock()
	f.mu.Lock()
	starts, sends := d.starts, d.sends
	f.mu.Unlock()
	if count != 1 || starts != 1 || sends != 1 {
		t.Fatal("continuation or duplicate replayed turn", count, starts, sends)
	}
}

func TestManagerCapacityExhaustionAndUnknownDoNotReplay(t *testing.T) {
	for _, scenario := range []string{"exhausted", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			s, f, d, m := taskPair(t)
			sendSynthetic(t, s, "manager-"+scenario+"-parent")
			s.capacityBackoff = func(int) time.Duration { return time.Millisecond }
			request := "manager-capacity-" + scenario
			var mu sync.Mutex
			var ids []string
			f.mu.Lock()
			original := f.handle
			f.handle = func(msg wireMessage) (any, bool) {
				if msg.Method == "turn/start" {
					var params struct {
						ClientID string `json:"clientUserMessageId"`
					}
					_ = json.Unmarshal(msg.Params, &params)
					mu.Lock()
					ids = append(ids, params.ClientID)
					mu.Unlock()
					if scenario == "exhausted" {
						return capacityRejection(), true
					}
				}
				return original(msg)
			}
			f.mu.Unlock()
			if scenario == "unknown" {
				f.mu.Lock()
				d.uncertain = true
				f.mu.Unlock()
			}
			start := api.TaskStart{RequestID: request, Title: "Capacity result", Prompt: "Synthetic task"}
			view, err := m.StartTask(testContext(t), start)
			if err == nil || view.ID == "" {
				t.Fatal("failed submission lost original task", view, err)
			}
			if scenario == "exhausted" && (view.Status != "failed" || view.Outcome != "rejected" || !strings.Contains(view.Result, "耗尽")) {
				t.Fatal("exhaustion was not visible", view)
			}
			if scenario == "unknown" && view.Outcome != "unknown" {
				t.Fatal("missing native receipt was treated as rejected", view)
			}
			again, err := m.StartTask(testContext(t), start)
			if err != nil || again.ID != view.ID {
				t.Fatal("duplicate request changed task ownership", again, err)
			}
			receipt, err := m.ReadTaskRequest(testContext(t), request)
			if err != nil || receipt.ID != view.ID {
				t.Fatal("original receipt became unreachable", receipt, err)
			}
			mu.Lock()
			gotIDs := append([]string(nil), ids...)
			mu.Unlock()
			want := 1
			if scenario == "exhausted" {
				want = 3
			}
			for _, id := range gotIDs {
				if id != request {
					t.Fatal("native request ID changed", gotIDs)
				}
			}
			if len(gotIDs) != want {
				t.Fatal("unsafe replay count", gotIDs, want)
			}
		})
	}
}

func TestManagerCapacityStopRacingAcceptedRetryReconcilesOriginalTurn(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "manager-race-parent")
	s.capacityBackoff = func(int) time.Duration { return time.Millisecond }
	request := "manager-capacity-race"
	secondEntered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var mu sync.Mutex
	var ids []string
	f.mu.Lock()
	original := f.handle
	f.handle = func(msg wireMessage) (any, bool) {
		if msg.Method == "turn/start" {
			var params struct {
				ClientID string `json:"clientUserMessageId"`
			}
			_ = json.Unmarshal(msg.Params, &params)
			mu.Lock()
			ids = append(ids, params.ClientID)
			attempt := len(ids)
			mu.Unlock()
			if attempt == 1 {
				return capacityRejection(), true
			}
			if attempt == 2 {
				secondEntered <- struct{}{}
				<-release // Native may accept after the local call is canceled.
			}
		}
		return original(msg)
	}
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	start := api.TaskStart{RequestID: request, Title: "Race task", Prompt: "Synthetic task"}
	started := make(chan managerResult, 1)
	go func() { v, err := m.StartTask(ctx, start); started <- managerResult{v, err} }()
	select {
	case <-secondEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("second dispatch was not reached")
	}
	pending := s.WorkStates()
	if len(pending) != 1 {
		t.Fatal("original worker missing during dispatched retry", pending)
	}
	stopped := make(chan managerResult, 1)
	go func() { v, err := m.StopTask(ctx, pending[0].Task.ID); stopped <- managerResult{v, err} }()
	result := awaitManagerResult(t, "start racing stop", started)
	if result.task.ID != pending[0].Task.ID || result.err == nil || result.task.Outcome != "unknown" {
		t.Fatal("in-flight retry cancellation was incorrectly called rejected", result)
	}
	if stop := awaitManagerResult(t, "stop racing reply", stopped); stop.err != nil || stop.task.ID != result.task.ID {
		t.Fatal("stop did not resolve after cancellation", stop)
	}
	releaseOnce.Do(func() { close(release) })
	deadline := time.After(2 * time.Second)
	for {
		view, err := m.ReadTaskRequest(ctx, request)
		if err == nil && view.ID == result.task.ID && view.Outcome == "accepted" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("original receipt did not reconcile native acceptance", view, err)
		case <-time.After(time.Millisecond):
		}
	}
	duplicate, err := m.StartTask(ctx, start)
	if err != nil || duplicate.ID != result.task.ID {
		t.Fatal("same request created a second task after reconciliation", duplicate, err)
	}
	mu.Lock()
	gotIDs := append([]string(nil), ids...)
	mu.Unlock()
	f.mu.Lock()
	starts, sends := d.starts, d.sends
	f.mu.Unlock()
	if len(gotIDs) != 2 || gotIDs[0] != request || gotIDs[1] != request || starts != 1 || sends != 1 {
		t.Fatal("unknown dispatch was replayed or lost native ownership", gotIDs, starts, sends)
	}
}
