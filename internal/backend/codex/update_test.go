package codex

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type updateEndpointConn struct {
	net.Conn
	endpoint string
}

func (c *updateEndpointConn) terminalEndpoint() string { return c.endpoint }

func TestUpdateDetachReopensExactResidentOwnerAndInvalidatesOldApproval(t *testing.T) {
	root := t.TempDir()
	opts := SessionOptions{Directory: filepath.Join(root, "Work"), StateFile: filepath.Join(root, "binding.json")}
	const endpoint = "unix:///tmp/fixture-update-original.sock"
	f := &sessionFixture{mode: "hold", started: make(chan struct{}, 8), answers: make(chan wireMessage, 8), loginReply: make(chan struct{})}
	var stopped atomic.Int32
	var starts atomic.Int32
	start := func(_ context.Context, o Options) (*Client, error) {
		if starts.Add(1) > 1 && (!o.RequiredSocket || o.Socket != strings.TrimPrefix(endpoint, "unix://")) {
			t.Errorf("restart escaped original owner: %+v", o)
		}
		a, b := net.Pipe()
		f.mu.Lock()
		f.peer, f.connections = b, f.connections+1
		f.mu.Unlock()
		go f.serve(b)
		var stop func()
		if starts.Load() == 1 {
			stop = func() { stopped.Add(1) }
		}
		c := &Client{rpc: newTransportOptions(&updateEndpointConn{a, endpoint}, stop, true)}
		if starts.Load() == 1 {
			c.owner = &fixtureRetainedOwner{endpoint: endpoint}
		}
		return c, nil
	}
	s := NewSession(opts)
	s.start = start
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if r := sendSynthetic(t, s, "update-original-submit"); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	f.emit(wireMessage{ID: raw("old-native-approval"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{
		"threadId": "thread-native", "turnId": "run-native", "itemId": "command", "command": "fixture", "availableDecisions": []any{"accept", "decline"},
	})})
	v := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 && v.Approvals[0].Status == "pending" })
	oldApproval := v.Approvals[0].ID
	// The test owner keeps executing after the observer transport is removed.
	f.mu.Lock()
	f.history = []nativeTurn{{ID: "run-native", Status: "inProgress", Items: []nativeItem{{ID: "user", Type: "userMessage", ClientID: "update-original-submit"}}}}
	f.mu.Unlock()
	if err := s.PrepareDetachForUpdate(testContext(t)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(opts.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	var saved binding
	if err := json.Unmarshal(b, &saved); err != nil || saved.OwnerEndpoint != endpoint || len(saved.PendingApprovalIDs) != 1 || saved.PendingApprovalIDs[0] != `"old-native-approval"` || saved.LastReceipt == nil || saved.LastReceipt.ID != "update-original-submit" {
		t.Fatal("original owner, approval and request receipts not durable", err)
	}
	if err := s.DetachForUpdate(testContext(t)); err != nil || stopped.Load() != 0 {
		t.Fatal("detach stopped the owner", err, stopped.Load())
	}
	s2 := NewSession(opts)
	s2.start = start
	if err := s2.Connect(testContext(t)); err != nil {
		t.Fatal("restart did not reconcile original owner", err)
	}
	if err := s2.Decide(testContext(t), api.Decision{ID: oldApproval, Choice: "decision-0"}); err == nil {
		t.Fatal("stale approval ID remained actionable")
	}
	if starts.Load() != 2 || f.starts != 1 || stopped.Load() != 0 {
		t.Fatal("original request replayed or owner stopped", starts.Load(), f.starts, stopped.Load())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s2.DetachForUpdate(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestUpdatePreflightSaveFailureKeepsResidentLive(t *testing.T) {
	s, _ := sessionPair(t, "hold")
	s.mu.Lock()
	s.binding.OwnerEndpoint = "unix:///tmp/fixture-owner.sock"
	s.mu.Unlock()
	if r := sendSynthetic(t, s, "update-save-failure"); r.Outcome != "accepted" {
		t.Fatal(r)
	}
	original := s.opts.StateFile
	s.opts.StateFile = filepath.Dir(original) // Rename cannot replace a directory.
	if err := s.PrepareDetachForUpdate(testContext(t)); err == nil {
		t.Fatal("failed preflight was accepted")
	}
	if s.Snapshot().Connection != "ready" || s.closed || s.closing {
		t.Fatal("failed preflight detached live owner")
	}
	s.opts.StateFile = original
	if err := s.PrepareDetachForUpdate(testContext(t)); err != nil {
		t.Fatal("retry after recovery failed", err)
	}
}

func TestUpdateDetachesRetainedWorkerAndReconcilesOriginalRequest(t *testing.T) {
	root := t.TempDir()
	opts := SessionOptions{Directory: filepath.Join(root, "Work"), StateFile: filepath.Join(root, "binding.json")}
	const endpoint = "unix:///tmp/fixture-retained-worker.sock"
	original := binding{Version: 1, Tasks: map[string]*taskRecord{
		"worker-task": {Thread: "worker-thread", Run: "worker-turn", View: api.Task{ID: "worker-task", Status: "working"}, Requests: map[string]taskReceipt{
			"worker-request": {Fingerprint: "fixture", Outcome: "unknown"},
		}},
	}}
	b, _ := json.Marshal(original)
	if err := os.WriteFile(opts.StateFile, b, 0600); err != nil {
		t.Fatal(err)
	}
	f := &sessionFixture{mode: "hold", answers: make(chan wireMessage, 8), workers: map[string]nativeThread{
		"worker-thread": {ID: "worker-thread", Turns: []nativeTurn{{ID: "worker-turn", Status: "inProgress", Items: []nativeItem{{ID: "worker-input", Type: "userMessage", ClientID: "worker-request"}}}}},
	}}
	worker := f.workers["worker-thread"]
	worker.Status.Type = "active"
	f.workers["worker-thread"] = worker
	var attempts atomic.Int32
	var stopped atomic.Int32
	start := func(_ context.Context, o Options) (*Client, error) {
		if attempts.Add(1) > 1 && (!o.RequiredSocket || o.Socket != strings.TrimPrefix(endpoint, "unix://")) {
			t.Errorf("retained worker switched owner: %+v", o)
		}
		a, b := net.Pipe()
		f.mu.Lock()
		f.peer = b
		f.mu.Unlock()
		go f.serve(b)
		var stop func()
		if attempts.Load() == 1 {
			stop = func() { stopped.Add(1) }
		}
		return &Client{rpc: newTransportOptions(&updateEndpointConn{a, endpoint}, stop, true)}, nil
	}
	w, err := NewRetainedWorkOwner(opts)
	if err != nil {
		t.Fatal(err)
	}
	w.engine.start = start
	if err := w.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if err := w.PrepareDetachForUpdate(testContext(t)); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(opts.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	var saved binding
	if err := json.Unmarshal(b, &saved); err != nil || saved.OwnerEndpoint != endpoint || saved.Tasks["worker-task"].Requests["worker-request"].Outcome != "accepted" {
		t.Fatal("worker receipt was not reconciled and saved", err)
	}
	if err := w.DetachForUpdate(testContext(t)); err != nil || stopped.Load() != 0 {
		t.Fatal("retained worker was stopped", err, stopped.Load())
	}
	w2, err := NewRetainedWorkOwner(opts)
	if err != nil {
		t.Fatal(err)
	}
	w2.engine.start = start
	if err := w2.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if states := w2.WorkStates(); len(states) != 1 || states[0].Task.Status != "working" || attempts.Load() != 2 || f.starts != 0 {
		t.Fatal("active original worker did not survive restart", states, attempts.Load(), f.starts)
	}
	if err := w2.DetachForUpdate(testContext(t)); err != nil {
		t.Fatal(err)
	}
}

func TestOrdinaryCloseClearsResolvedOwnerEndpoint(t *testing.T) {
	s, _ := sessionPair(t, "normal")
	s.mu.Lock()
	s.binding.OwnerEndpoint = "unix:///tmp/obsolete-private-owner.sock"
	if err := s.save(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	if err := s.Close(testContext(t)); err != nil {
		t.Fatal(err)
	}
	loaded := NewSession(s.opts)
	if loaded.binding.OwnerEndpoint != "" {
		t.Fatal("normal exit pinned next launch to stopped owner")
	}
}
