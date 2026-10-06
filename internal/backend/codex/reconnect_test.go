package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

func TestSlowSessionConsumerDoesNotBlockWireRPC(t *testing.T) {
	s, f := sessionPair(t, "hold")
	if r := sendSynthetic(t, s, "slow-consumer-original"); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	s.mu.Lock()
	c, revision := s.client, s.state.Revision
	burst := make(chan struct{})
	go func() {
		for i := 0; i < 150; i++ {
			f.emit(wireMessage{Method: "thread/tokenUsage/updated", Params: raw(map[string]any{"threadId": "thread-native", "index": i})})
		}
		f.emit(wireMessage{ID: raw("burst-approval-original"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{
			"threadId": "thread-native", "turnId": "run-native", "itemId": "command", "command": "fixture", "availableDecisions": []any{"accept", "decline"},
		})})
		f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "run-native", Status: "completed"}})})
		close(burst)
	}()
	select {
	case <-burst:
	case <-time.After(5 * time.Second):
		s.mu.Unlock()
		t.Fatal("wire reader blocked at old 64-event capacity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var auth struct {
		Account json.RawMessage `json:"account"`
	}
	err := callDecode(ctx, c, "account/read", map[string]bool{"refreshToken": false}, &auth)
	s.mu.Unlock()
	if err != nil || len(auth.Account) == 0 {
		t.Fatal("response stalled behind slow event projection", err)
	}
	v := awaitState(t, s, func(v api.Snapshot) bool { return v.Revision >= revision+152 && v.Phase == "completed" })
	if len(v.Approvals) != 1 || v.Approvals[0].Status != "resolved" {
		t.Fatal("ordered approval/completion lost", v.Approvals)
	}
	if c.Err() != nil {
		t.Fatal("burst disconnected transport", c.Err())
	}
}

func TestHandshakeBufferIsBoundedAndFailsExplicitly(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return time.Hour }
	s.mu.Lock()
	s.loading = true
	c := s.client
	s.mu.Unlock()
	for i := 0; i < maxQueuedEvents+1; i++ {
		f.emit(wireMessage{Method: "thread/tokenUsage/updated", Params: raw(map[string]any{"threadId": "thread-native", "index": i})})
	}
	select {
	case <-c.Done():
	case <-testContext(t).Done():
		t.Fatal("unbounded handshake buffer")
	}
	if !errors.Is(c.Err(), ErrEventOverflow) {
		t.Fatal(c.Err())
	}
	s.mu.Lock()
	count, bytes := len(s.buffer), s.bufferBytes
	s.mu.Unlock()
	if count > maxQueuedEvents || bytes > maxQueuedEventBytes {
		t.Fatal(count, bytes)
	}
}

func TestEOFAutomaticallyReconcilesOriginalSubmission(t *testing.T) {
	s, f := sessionPair(t, "disconnect")
	s.reconnectDelay = func(int) time.Duration { return time.Millisecond }
	r := sendSynthetic(t, s, "submit-auto-original")
	if r.Outcome != "unknown" {
		t.Fatal(r)
	}
	v := awaitState(t, s, func(v api.Snapshot) bool {
		return v.Connection == "ready" && v.LastReceipt.ID == r.ID && v.LastReceipt.Outcome == "accepted"
	})
	if !v.CanSend {
		t.Fatal("original receipt was not reconciled", v.LastReceipt)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.starts != 1 || f.connections != 2 {
		t.Fatal("submission replay or missing reconnect", f.starts, f.connections)
	}
}

func TestManualReconnectCancelsScheduledAutomaticAttempt(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return 200 * time.Millisecond }
	f.mu.Lock()
	f.peer.Close()
	f.mu.Unlock()
	awaitState(t, s, func(v api.Snapshot) bool { return v.Connection == "offline" })
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connections != 2 {
		t.Fatal("cancelled automatic attempt reopened connection", f.connections)
	}
}

func TestQuitCancelsReconnectBackoff(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return 200 * time.Millisecond }
	f.mu.Lock()
	f.peer.Close()
	f.mu.Unlock()
	awaitState(t, s, func(v api.Snapshot) bool { return v.Connection == "offline" })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.Close(ctx) // The broken fixture cannot confirm native tool cleanup.
	time.Sleep(300 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connections != 1 {
		t.Fatal("quit opened a new owner", f.connections)
	}
}

func TestExpiredApprovalCannotBeDispatchedAfterAutoReconnect(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return time.Millisecond }
	if r := sendSynthetic(t, s, "approval-root-request"); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	f.emit(wireMessage{ID: raw("native-approval-original"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{
		"threadId": "thread-native", "turnId": "run-native", "itemId": "command", "command": "fixture", "availableDecisions": []any{"accept", "decline"},
	})})
	v := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 && v.Approvals[0].Status == "pending" })
	oldID, choice := v.Approvals[0].ID, v.Approvals[0].Choices[0].ID
	s.mu.Lock()
	originalEpoch := s.epoch
	s.mu.Unlock()
	f.mu.Lock()
	f.peer.Close()
	f.mu.Unlock()
	awaitState(t, s, func(v api.Snapshot) bool {
		return v.Connection == "ready" && s.DiagnosticStatus()["sessionEpoch"].(uint64) > originalEpoch && s.DiagnosticStatus()["autoReconnectActive"] == false
	})
	if err := s.Decide(testContext(t), api.Decision{ID: oldID, Choice: choice}); err == nil {
		t.Fatal("expired native approval was dispatched")
	}
	select {
	case answer := <-f.answers:
		t.Fatal("old approval replayed", answer)
	default:
	}
}

type fixtureRetainedOwner struct{ endpoint string }

func (o *fixtureRetainedOwner) captureTools()            {}
func (o *fixtureRetainedOwner) toolCleanupError() error  { return nil }
func (o *fixtureRetainedOwner) terminalEndpoint() string { return o.endpoint }

func TestAutoReconnectPinsOriginalPrivateOwnerEndpoint(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return time.Millisecond }
	owner := &fixtureRetainedOwner{endpoint: "unix:///tmp/fixture-original-owner.sock"}
	s.mu.Lock()
	originalEpoch := s.epoch
	s.client.owner = owner
	s.retainedOwner = s.client
	s.mu.Unlock()
	start := s.start
	var observed Options
	s.start = func(ctx context.Context, opts Options) (*Client, error) { observed = opts; return start(ctx, opts) }
	f.mu.Lock()
	f.peer.Close()
	f.mu.Unlock()
	awaitState(t, s, func(v api.Snapshot) bool {
		return v.Connection == "ready" && s.DiagnosticStatus()["sessionEpoch"].(uint64) > originalEpoch && s.DiagnosticStatus()["autoReconnectActive"] == false
	})
	if observed.Socket != "/tmp/fixture-original-owner.sock" || !observed.RequiredSocket || s.client.owner != owner {
		t.Fatal("recovery did not retain original private owner", observed.Socket, observed.RequiredSocket)
	}
}

func TestFailedReconnectHandshakeDoesNotStopOriginalPrivateOwner(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return time.Millisecond }
	var stopped atomic.Int32
	s.mu.Lock()
	s.client.owner = &fixtureRetainedOwner{endpoint: "unix:///tmp/fixture-original-owner.sock"}
	s.client.rpc.stop = func() { stopped.Add(1) }
	s.retainedOwner = s.client
	s.mu.Unlock()
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "account/read" {
			return &NativeError{Code: -32000, Message: "fixture handshake failure"}, true
		}
		return nil, false
	}
	f.peer.Close()
	f.mu.Unlock()
	awaitState(t, s, func(v api.Snapshot) bool {
		return v.Connection == "offline" && s.DiagnosticStatus()["autoReconnectActive"] == false && s.DiagnosticStatus()["autoReconnectAttempts"] == maxAutoReconnectAttempts
	})
	if stopped.Load() != 0 {
		t.Fatal("failed observer handshake stopped original private runtime", stopped.Load())
	}
}

func TestUnconfirmedCleanupStopsAutomaticReconnectWithoutRepeatingIt(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return time.Millisecond }
	if receipt := sendSynthetic(t, s, "cleanup-original"); receipt.Outcome != "accepted" {
		t.Fatal(receipt)
	}
	s.mu.Lock()
	s.binding.CleanupTargets = []string{"thread-native"}
	s.binding.StopState = "prepared"
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "thread/read" {
			thread := nativeThread{ID: "thread-native", Turns: []nativeTurn{{ID: "run-native", Status: "inProgress"}}}
			thread.Status.Type = "active"
			return map[string]any{"thread": thread}, true
		}
		return nil, false
	}
	f.peer.Close()
	f.mu.Unlock()
	awaitState(t, s, func(v api.Snapshot) bool {
		return v.Connection == "ready" && v.Phase == "unknown" && s.DiagnosticStatus()["autoReconnectActive"] == false
	})
	f.mu.Lock()
	connections := f.connections
	f.mu.Unlock()
	if connections != 2 {
		t.Fatal("unknown cleanup caused a second automatic connection", connections)
	}
	s.mu.Lock()
	remaining := append([]string(nil), s.binding.CleanupTargets...)
	s.mu.Unlock()
	if len(remaining) != 1 || remaining[0] != "thread-native" {
		t.Fatal("original cleanup target was discarded", remaining)
	}
}

func TestResourceExhaustionStopsRetryAndPreservesOriginalOwner(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return time.Millisecond }
	var attempts atomic.Int32
	s.start = func(ctx context.Context, opts Options) (*Client, error) {
		attempts.Add(1)
		return nil, errors.Join(errExistingServer, syscall.EMFILE)
	}
	f.mu.Lock()
	f.peer.Close()
	f.mu.Unlock()
	awaitState(t, s, func(v api.Snapshot) bool {
		return v.ConnectionIssue == "resource_exhausted" && v.Connection == "offline" && s.DiagnosticStatus()["autoReconnectActive"] == false
	})
	if attempts.Load() != 1 {
		t.Fatal("resource exhaustion retried", attempts.Load())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connections != 1 {
		t.Fatal("replacement owner started", f.connections)
	}
}

func TestSubscriptionFailureNeverReportsReadyOrCompletesUnknownTask(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.reconnectDelay = func(int) time.Duration { return time.Millisecond }
	worker := nativeThread{ID: "worker-original", Turns: []nativeTurn{{ID: "run-original", Status: "inProgress"}}}
	worker.Status.Type = "active"
	s.mu.Lock()
	s.binding.Tasks = map[string]*taskRecord{"task-original": {Thread: worker.ID, Run: "run-original", View: api.Task{ID: "task-original", Status: "unknown"}, Pending: "request-original", Requests: map[string]taskReceipt{"request-original": {Outcome: "unknown"}}}}
	s.binding.Children = []string{worker.ID}
	s.children[worker.ID] = true
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	f.mu.Lock()
	f.workers = map[string]nativeThread{worker.ID: worker}
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "thread/resume" && string(m.Params) != "" {
			var params struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(m.Params, &params)
			if params.ThreadID == worker.ID {
				return &NativeError{Code: -32000, Message: "fixture subscription refused"}, true
			}
		}
		return nil, false
	}
	f.peer.Close()
	f.mu.Unlock()
	awaitState(t, s, func(v api.Snapshot) bool {
		return v.Connection == "offline" && s.DiagnosticStatus()["autoReconnectActive"] == false && s.DiagnosticStatus()["autoReconnectAttempts"] == maxAutoReconnectAttempts
	})
	s.mu.Lock()
	task := s.binding.Tasks["task-original"]
	status, pending, receipt, thread, run := task.View.Status, task.Pending, task.Requests["request-original"].Outcome, task.Thread, task.Run
	s.mu.Unlock()
	if status != "unknown" || pending != "request-original" || receipt != "unknown" || thread != worker.ID || run != "run-original" {
		t.Fatal("uncertain original work was rewritten", status, pending, receipt, thread, run)
	}
}

func TestEarlierTerminalCannotSettleUnknownOriginalRequest(t *testing.T) {
	s := NewSession(SessionOptions{})
	defer s.cancelLife()
	task := &taskRecord{Thread: "worker-original", Run: "earlier-run", Pending: "request-original", View: api.Task{ID: "task-original", Status: "unknown", Outcome: "unknown"},
		Requests: map[string]taskReceipt{"request-original": {Outcome: "unknown"}}}
	s.observeTaskTurn(task, nativeTurn{ID: "earlier-run", Status: "completed"})
	if task.View.Status != "unknown" || task.Pending != "request-original" || task.Requests["request-original"].Outcome != "unknown" || task.ReportID != "" {
		t.Fatal("older completion settled unknown request", task)
	}
	s.observeTaskTurn(task, nativeTurn{ID: "original-run", Status: "completed", Items: []nativeItem{{Type: "userMessage", ClientID: "request-original"}}})
	if task.View.Status != "completed" || task.Pending != "" || task.Requests["request-original"].Outcome != "accepted" || task.Run != "original-run" {
		t.Fatal("native original receipt did not reconcile", task)
	}
}

func TestTransportAndSessionGenerationAreLoggedSeparately(t *testing.T) {
	dir := t.TempDir()
	a, peer := net.Pipe()
	rpc := newTransportLogged(a, nil, true, diagnosticlog.New(dir))
	defer func() { peer.Close(); rpc.close() }()
	rpc.sessionEpoch.Store(2)
	for i := 0; i < maxQueuedEvents+1; i++ {
		_ = json.NewEncoder(peer).Encode(wireMessage{Method: "item/event"})
	}
	select {
	case <-rpc.done:
	case <-testContext(t).Done():
		t.Fatal("overflow not recorded")
	}
	<-rpc.readDone
	b, err := os.ReadFile(dir + "/error.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var record diagnosticlog.Record
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		var candidate diagnosticlog.Record
		if json.Unmarshal(line, &candidate) == nil && candidate.Code == "event_overflow" {
			record = candidate
			break
		}
	}
	if record.Code != "event_overflow" || record.Generation != rpc.generation || record.TransportGeneration != rpc.generation || record.SessionEpoch != 2 || record.QueueDepth == 0 {
		t.Fatal("wire generation and owner epoch were conflated", record)
	}
}
