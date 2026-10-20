package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

func awaitCapacityOwnerDisconnect(t *testing.T, c *Client) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for c.Err() == nil {
		select {
		case <-deadline:
			t.Fatal("fixture owner did not disconnect")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestBotSavedCapacityStopReconnectInterruptsOnlyOriginalTurn(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return time.Hour }
	request, run := "saved-bot-capacity-request", "saved-bot-turn"
	turn := nativeTurn{ID: run, Status: "inProgress", Items: []nativeItem{{ID: "original-user", Type: "userMessage", ClientID: request}}}
	interrupts := make(chan struct{ Thread, Turn string }, 2)
	f.mu.Lock()
	f.history = []nativeTurn{turn}
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method != "turn/interrupt" {
			return nil, false
		}
		var p struct {
			Thread string `json:"threadId"`
			Turn   string `json:"turnId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		interrupts <- struct{ Thread, Turn string }{p.Thread, p.Turn}
		return map[string]any{}, true
	}
	f.mu.Unlock()
	s.mu.Lock()
	old := s.client
	s.state.Connection = "offline" // persisted while disconnected; Connect must queue it on ready
	s.binding.CapacityStopRequest, s.binding.CapacityStopThread, s.binding.CapacityStopTurn, s.binding.CapacityStopPhase, s.binding.CapacityStopAccepted = request, "thread-native", run, "requested", true
	receipt := api.Receipt{ID: request, Outcome: "accepted"}
	s.binding.LastReceipt = &receipt
	s.state.LastReceipt = receipt
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	f.mu.Lock()
	_ = f.peer.Close()
	f.mu.Unlock()
	awaitCapacityOwnerDisconnect(t, old)
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case target := <-interrupts:
		if target.Thread != "thread-native" || target.Turn != run {
			t.Fatal("reconnect targeted another Turn", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect did not reconcile saved stop")
	}
	if err := s.Interrupt(testContext(t)); err == nil {
		t.Fatal("interrupt RPC acknowledgement became terminal success")
	}
	select {
	case target := <-interrupts:
		t.Fatal("repeated stop replayed interrupt", target)
	default:
	}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: run, Status: "completed"}})})
	view := awaitState(t, s, func(v api.Snapshot) bool {
		return v.Phase == "completed" && strings.Contains(v.Message, "停止请求未确认及时生效")
	})
	if view.LastReceipt.ID != request || view.LastReceipt.Outcome != "accepted" {
		t.Fatal("original receipt changed during stop", view.LastReceipt)
	}
}

func TestWorkerSavedCapacityStopReconnect(t *testing.T) {
	s, f, _, m := taskPair(t)
	s.reconnectDelay = func(int) time.Duration { return time.Hour }
	sendSynthetic(t, s, "saved-worker-parent")
	started := newTask(t, m, "saved-worker-request")
	interrupts := make(chan struct{ Thread, Turn string }, 2)
	f.mu.Lock()
	original := f.handle
	f.handle = func(msg wireMessage) (any, bool) {
		if msg.Method != "turn/interrupt" {
			return original(msg)
		}
		var p struct {
			Thread string `json:"threadId"`
			Turn   string `json:"turnId"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		interrupts <- struct{ Thread, Turn string }{p.Thread, p.Turn}
		return map[string]any{}, true
	}
	f.mu.Unlock()
	s.mu.Lock()
	old := s.client
	record := s.binding.Tasks[started.ID]
	thread, run := record.Thread, record.Run
	record.StopPendingRequest, record.StopPendingTurn, record.StopPendingPhase = "saved-worker-request", run, "requested"
	s.state.Connection = "offline"
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	f.mu.Lock()
	_ = f.peer.Close()
	f.mu.Unlock()
	awaitCapacityOwnerDisconnect(t, old)
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	select {
	case target := <-interrupts:
		if target.Thread != thread || target.Turn != run {
			t.Fatal("reconnect targeted another Worker Turn", target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect did not reconcile saved Worker stop")
	}
	if view, err := s.StopWork(testContext(t), started.ID); err == nil || view.Status != "unknown" {
		t.Fatal("Worker stop acknowledged before native terminal", view, err)
	}
	select {
	case target := <-interrupts:
		t.Fatal("repeated Worker stop replayed interrupt", target)
	default:
	}
	// Simulate a missed native notification: only the original owner read can
	// establish terminal status after the interrupt RPC acknowledgement.
	f.mu.Lock()
	worker := f.workers[thread]
	worker.Status.Type = "idle"
	worker.Turns = []nativeTurn{{ID: run, Status: "interrupted", Items: []nativeItem{{ID: "user", Type: "userMessage", ClientID: "saved-worker-request"}}}}
	f.workers[thread] = worker
	f.mu.Unlock()
	deadline := time.After(2 * time.Second)
	for {
		states := s.WorkStates()
		if len(states) == 1 && states[0].StopResolution == "interrupted" && states[0].Task.Status == "interrupted" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Worker original terminal not recorded", states)
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := s.StopWork(testContext(t), started.ID); err != nil {
		t.Fatal("original Worker interruption not confirmed", err)
	}
}

func TestBotSavedCapacityStopTerminalBeforeInterruptKeepsResult(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return time.Hour }
	request, run := "terminal-bot-stop-request", "terminal-original-turn"
	turn := nativeTurn{ID: run, Status: "completed", Items: []nativeItem{{ID: "original-user", Type: "userMessage", ClientID: request}}}
	interrupts := make(chan struct{}, 1)
	f.mu.Lock()
	f.history = []nativeTurn{turn}
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/interrupt" {
			interrupts <- struct{}{}
			return map[string]any{}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	s.mu.Lock()
	old := s.client
	s.state.Connection = "offline"
	s.binding.CapacityStopRequest, s.binding.CapacityStopThread, s.binding.CapacityStopTurn, s.binding.CapacityStopPhase, s.binding.CapacityStopAccepted = request, "thread-native", run, "requested", true
	receipt := api.Receipt{ID: request, Outcome: "accepted"}
	s.binding.LastReceipt = &receipt
	s.state.LastReceipt = receipt
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	f.mu.Lock()
	_ = f.peer.Close()
	f.mu.Unlock()
	awaitCapacityOwnerDisconnect(t, old)
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	view := awaitState(t, s, func(v api.Snapshot) bool {
		return v.Phase == "completed" && strings.Contains(v.Message, "停止请求未确认及时生效")
	})
	if view.LastReceipt.ID != request || view.LastReceipt.Outcome != "accepted" {
		t.Fatal("terminal recovery changed original receipt", view.LastReceipt)
	}
	select {
	case <-interrupts:
		t.Fatal("already completed Turn received an interrupt")
	default:
	}
	if err := s.Interrupt(testContext(t)); err == nil || !strings.Contains(err.Error(), "未确认及时生效") {
		t.Fatal("completed original was falsely called stopped", err)
	}
}

func TestCapacityStopCloseRetainsUnknownTargetWithoutDispatch(t *testing.T) {
	s, f := sessionPair(t, "hold")
	request := "closing-unknown-request"
	interrupts := make(chan struct{}, 1)
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/interrupt" {
			interrupts <- struct{}{}
			return map[string]any{}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	s.mu.Lock()
	s.state.Connection = "offline"
	s.binding.CapacityStopRequest, s.binding.CapacityStopThread, s.binding.CapacityStopPhase = request, "thread-native", "requested"
	s.binding.OwnerEndpoint = "unix:///tmp/synthetic-capacity-owner.sock"
	s.binding.OwnerProcess = &ownerProcessIdentity{PID: 1001, Birth: 91}
	s.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle { return ownerGone }
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-interrupts:
		t.Fatal("close guessed an unidentified Turn")
	default:
	}
	loaded := NewSession(s.opts)
	defer loaded.cancelLife()
	if loaded.loadErr != nil || !loaded.binding.OwnerUnavailable || loaded.binding.CapacityStopRequest != request || loaded.binding.CapacityStopTurn != "" || loaded.binding.OwnerEndpoint != "unix:///tmp/synthetic-capacity-owner.sock" {
		t.Fatal("close discarded unknown stop intent or original owner", loaded.loadErr, loaded.binding.CapacityStopRequest, loaded.binding.CapacityStopTurn, loaded.binding.OwnerEndpoint)
	}
}

func TestPrivateOwnerCloseNewSessionManualRecoveryKeepsOldReceiptsReadOnly(t *testing.T) {
	s, f, _, _ := taskPair(t)
	const request, taskID = "closed-owner-original-request", "closed-owner-worker"
	endpoint := "unix://" + filepath.Join(t.TempDir(), "owned-original.sock")
	var stopped atomic.Int32
	var oldInterrupts atomic.Int32
	var oldReads atomic.Int32
	var oldDispatches atomic.Int32
	f.mu.Lock()
	originalHandle := f.handle
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/interrupt" {
			oldInterrupts.Add(1)
			return map[string]any{}, true
		}
		if m.Method == "thread/read" || m.Method == "thread/resume" {
			var params struct {
				Thread string `json:"threadId"`
			}
			_ = json.Unmarshal(m.Params, &params)
			if params.Thread == "old-worker-thread" || m.Method == "thread/resume" && params.Thread == "thread-native" {
				oldReads.Add(1)
			}
		}
		if m.Method == "turn/start" {
			var params struct {
				Request string `json:"clientUserMessageId"`
			}
			_ = json.Unmarshal(m.Params, &params)
			if params.Request == request {
				oldDispatches.Add(1)
			}
		}
		return originalHandle(m)
	}
	f.mu.Unlock()
	s.mu.Lock()
	s.client.owner = &fixtureRetainedOwner{endpoint: endpoint, identity: ownerProcessIdentity{PID: 1002, Birth: 92}}
	s.client.rpc.stop = func() { stopped.Add(1) }
	s.ownerLifecycleProbe = func(ownerProcessIdentity) ownerLifecycle {
		if stopped.Load() == 0 {
			return ownerAlive
		}
		return ownerGone
	}
	s.binding.OwnerEndpoint = endpoint
	s.binding.CapacityStopRequest, s.binding.CapacityStopThread, s.binding.CapacityStopTurn = request, "thread-native", "old-turn"
	s.binding.CapacityStopPhase, s.binding.CapacityStopAccepted = "attempted", true
	s.binding.LastReceipt = &api.Receipt{ID: request, Outcome: "accepted"}
	s.binding.Tasks = map[string]*taskRecord{taskID: {
		Thread: "old-worker-thread", View: api.Task{ID: taskID, Status: "working", Outcome: "accepted"},
		Requests:           map[string]taskReceipt{request: {Outcome: "accepted"}},
		StopPendingRequest: request, StopPendingTurn: "old-worker-turn", StopPendingPhase: "attempted",
	}}
	s.state.Connection = "offline" // no observer may invent a native terminal for the fixture
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	if err := s.Close(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if stopped.Load() != 1 {
		t.Fatal("ordinary Close did not stop its private owner", stopped.Load())
	}
	oldReads.Store(0)
	oldDispatches.Store(0)
	loaded := NewSession(s.opts)
	loaded.ownerLifecycleProbe = s.ownerLifecycleProbe
	t.Cleanup(func() { _ = loaded.Close(testContext(t)) })
	start := s.start
	var started atomic.Int32
	loaded.start = func(ctx context.Context, options Options) (*Client, error) {
		started.Add(1)
		if options.RequiredSocket || options.Socket == strings.TrimPrefix(endpoint, "unix://") {
			t.Errorf("new owner was pinned to dead private socket: %+v", options)
		}
		return start(ctx, options)
	}
	if err := loaded.Connect(testContext(t)); err == nil || !strings.Contains(err.Error(), "无法再核对") {
		t.Fatal("automatic Connect hid original owner loss", err)
	}
	if started.Load() != 0 || loaded.Snapshot().ConnectionIssue != "owner_unavailable" {
		t.Fatal("automatic Connect started replacement owner or hid loss", started.Load(), loaded.Snapshot())
	}
	service := backend.NewService(loaded, nil, nil, nil, nil)
	fence := service.RecoveryState().Fence
	if fence == "" || !service.RecoveryState().Manual {
		t.Fatal("original loss did not expose fenced manual recovery", service.RecoveryState())
	}
	stateFile := loaded.opts.StateFile
	loaded.mu.Lock()
	loaded.opts.StateFile = filepath.Dir(stateFile)
	loaded.mu.Unlock()
	if err := service.RecoverIfCurrent(testContext(t), fence); err == nil || started.Load() != 0 {
		t.Fatal("failed recovery archive write started a replacement owner", err, started.Load())
	}
	loaded.mu.Lock()
	loaded.opts.StateFile = stateFile
	stillLost := loaded.binding.OwnerUnavailable && len(loaded.binding.LostOwners) == 0
	loaded.mu.Unlock()
	if !stillLost {
		t.Fatal("failed archive write changed the original owner record")
	}
	fence = service.RecoveryState().Fence
	if err := service.RecoverIfCurrent(testContext(t), fence); err != nil {
		t.Fatal(err)
	}
	if started.Load() != 1 || loaded.Snapshot().Connection != "ready" {
		t.Fatal("manual recovery did not establish a new usable owner", started.Load(), loaded.Snapshot())
	}
	if err := service.RecoverIfCurrent(testContext(t), fence); err == nil {
		t.Fatal("used recovery fence was accepted again")
	}
	archive := loaded.binding.LostOwners
	if len(archive) != 1 || archive[0].Endpoint != endpoint || archive[0].Thread != "thread-native" || archive[0].StopRequest != request || archive[0].StopTurn != "old-turn" || archive[0].LastReceipt == nil || archive[0].LastReceipt.ID != request || !loaded.binding.LostOwnerTasks[taskID] {
		t.Fatal("original owner identities and receipts were not archived", archive, loaded.binding.LostOwnerTasks)
	}
	if old, err := loaded.ReadWork(testContext(t), taskID); err == nil || old.Status != "unknown" || !strings.Contains(old.Result, "运行环境尚未确认") {
		t.Fatal("old worker result was lost or attributed to new owner", old, err)
	}
	if old, err := loaded.StopWork(testContext(t), taskID); err == nil || old.Status != "unknown" {
		t.Fatal("old worker was interrupted on the new owner", old, err)
	}
	if receipt := sendSynthetic(t, loaded, "new-owner-fresh-request"); receipt.Outcome != "accepted" {
		t.Fatal("new owner could not accept fresh work", receipt)
	}
	if oldInterrupts.Load() != 0 || oldReads.Load() != 0 || oldDispatches.Load() != 0 {
		t.Fatal("old work was queried, dispatched or interrupted on the new owner", oldReads.Load(), oldDispatches.Load(), oldInterrupts.Load())
	}
	m, err := tasks.Open(filepath.Join(t.TempDir(), "recovered-product-tasks.json"), loaded.workRoot(), "codex", loaded, loaded, loaded.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.ConfigureLimit(func() int { return 1 })
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	states := loaded.WorkStates()
	if len(states) != 1 || states[0].Task.Status != "unknown" || states[0].Activity != api.WorkActivityOwnerUnavailable {
		t.Fatal("archived Worker changed result or still occupied the new owner", states)
	}
	if _, err := m.StartTask(testContext(t), api.TaskStart{RequestID: "new-owner-worker", Title: "Fresh worker", Prompt: "Create a new fixture artifact"}); err != nil {
		t.Fatal("archived owner consumed the new connection's only Worker slot", err)
	}
	if oldInterrupts.Load() != 0 || oldReads.Load() != 0 || oldDispatches.Load() != 0 {
		t.Fatal("new Worker creation replayed old native work", oldReads.Load(), oldDispatches.Load(), oldInterrupts.Load())
	}
	reloaded := NewSession(loaded.opts)
	defer reloaded.cancelLife()
	if len(reloaded.binding.LostOwners) != 1 || reloaded.binding.LostOwners[0].StopRequest != request || !reloaded.binding.LostOwnerTasks[taskID] {
		t.Fatal("old unknown result did not survive another reload", reloaded.binding)
	}
}

func TestWorkerStopTerminalSaveFailureBlocksManagerReportAndReload(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "save-failure-parent")
	request := "save-failure-worker-request"
	started := newTask(t, m, request)
	s.mu.Lock()
	task := s.binding.Tasks[started.ID]
	task.StopPendingRequest, task.StopPendingTurn, task.StopPendingPhase = request, task.Run, "attempted"
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	stateFile := s.opts.StateFile
	s.opts.StateFile = filepath.Dir(stateFile) // atomic rename onto a directory fails deterministically
	s.mu.Unlock()
	t.Cleanup(func() { s.mu.Lock(); s.opts.StateFile = stateFile; s.mu.Unlock() })
	finishTask(s, f, started.ID, "completed")
	deadline := time.After(2 * time.Second)
	for {
		states := s.WorkStates()
		if len(states) == 1 && states[0].Task.Status == "unknown" && states[0].StopPending && states[0].StopResolution == "" && strings.Contains(states[0].Task.Result, "尚未保存") {
			break
		}
		select {
		case <-deadline:
			t.Fatal("failed save published native terminal", states)
		case <-time.After(time.Millisecond):
		}
	}
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	if list := m.ListTasks(); len(list) != 1 || list[0].Status != "unknown" {
		t.Fatal("manager reported terminal before binding was durable", list)
	}
	if err := m.DeliverTaskReport(testContext(t)); err != nil {
		t.Fatal(err)
	}
	before := NewSession(SessionOptions{Directory: s.opts.Directory, StateFile: stateFile})
	if before.binding.Tasks[started.ID].StopPendingStatus != "" {
		t.Fatal("failed native terminal write appeared on disk")
	}
	before.cancelLife()
	s.mu.Lock()
	s.opts.StateFile = stateFile
	s.mu.Unlock()
	deadline = time.After(3 * time.Second)
	for {
		states := s.WorkStates()
		if len(states) == 1 && states[0].StopResolution == "completed" && states[0].Task.Status == "completed" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("local persistence retry did not release terminal", states)
		case <-time.After(time.Millisecond):
		}
	}
	after := NewSession(SessionOptions{Directory: s.opts.Directory, StateFile: stateFile})
	defer after.cancelLife()
	if after.binding.Tasks[started.ID].StopPendingStatus != "completed" {
		t.Fatal("observed terminal was not durable after retry")
	}
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	finishRoot(f)
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	if err := m.DeliverTaskReport(testContext(t)); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	report := string(f.lastParams["input"])
	f.mu.Unlock()
	if !strings.Contains(report, "stop was requested") || !strings.Contains(report, "ended completed") {
		t.Fatal("durable terminal did not reach factual report", report)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatal(err)
	}
}

func TestBotStopTerminalSaveFailureStaysVisibleUntilDurable(t *testing.T) {
	s, f := sessionPair(t, "hold")
	request := "bot-save-failure-original"
	if receipt := sendSynthetic(t, s, request); receipt.Outcome != "accepted" {
		t.Fatal(receipt)
	}
	s.mu.Lock()
	s.binding.CapacityStopRequest, s.binding.CapacityStopThread, s.binding.CapacityStopTurn = request, "thread-native", "run-native"
	s.binding.CapacityStopAccepted, s.binding.CapacityStopPhase = true, "attempted"
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	stateFile := s.opts.StateFile
	s.opts.StateFile = filepath.Dir(stateFile)
	s.mu.Unlock()
	t.Cleanup(func() { s.mu.Lock(); s.opts.StateFile = stateFile; s.mu.Unlock() })
	finishRoot(f)
	awaitState(t, s, func(v api.Snapshot) bool {
		return v.Phase == "unknown" && strings.Contains(v.Message, "尚未保存") && !v.CanSend
	})
	before := NewSession(SessionOptions{Directory: s.opts.Directory, StateFile: stateFile})
	if before.binding.CapacityStopStatus != "" {
		t.Fatal("unsaved Bot terminal appeared on disk")
	}
	before.cancelLife()
	s.mu.Lock()
	s.opts.StateFile = stateFile
	s.mu.Unlock()
	awaitState(t, s, func(v api.Snapshot) bool {
		return v.Phase == "completed" && strings.Contains(v.Message, "未确认及时生效")
	})
	after := NewSession(SessionOptions{Directory: s.opts.Directory, StateFile: stateFile})
	defer after.cancelLife()
	if after.binding.CapacityStopStatus != "completed" || after.binding.CapacityStopRequest != request {
		t.Fatal("Bot terminal/receipt not durable after local retry")
	}
}

func TestCapacityStopNeverUsesAnotherThreadReceipt(t *testing.T) {
	s, _ := sessionPair(t, "hold")
	s.mu.Lock()
	s.binding.CapacityStopRequest = "same-request-on-other-thread"
	s.binding.CapacityStopThread = "old-native-thread"
	s.binding.CapacityStopTurn = "old-native-turn"
	s.binding.CapacityStopPhase = "requested"
	s.binding.ThreadID = "new-native-thread"
	s.state.LastReceipt = api.Receipt{ID: s.binding.CapacityStopRequest, Outcome: "accepted"}
	s.binding.LastReceipt = &s.state.LastReceipt
	s.applyItem(s.binding.ThreadID, "new-native-turn", nativeItem{ID: "new-user", Type: "userMessage", ClientID: s.binding.CapacityStopRequest}, false)
	s.queueCapacityStopsLocked()
	accepted, turn, queued := s.binding.CapacityStopAccepted, s.binding.CapacityStopTurn, s.capacityStopReconciling["bot"]
	s.mu.Unlock()
	if accepted || turn != "old-native-turn" || queued {
		t.Fatal("another thread's same-name receipt authorized the old stop", accepted, turn, queued)
	}
}
