package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
	"github.com/caelis-labs/caelis-bot/internal/nodeworker"
)

type workerSourceFixture struct {
	mu    sync.Mutex
	value api.WorkDispatchSource
	err   error
}

func (s *workerSourceFixture) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.err
}
func (s *workerSourceFixture) next() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value.OperationID += "-next"
}

type workerFixture struct {
	w                          *WorkerClient
	f                          *sessionFixture
	source                     *workerSourceFixture
	starts, sends, initializes int
	startUnknown, sendUnknown  bool
	advanceOnStart             bool
	threadParams, turnParams   map[string]any
	interrupts                 []map[string]string
	stops                      atomic.Int32
	resumed                    []string
}

func workerPair(t *testing.T) *workerFixture {
	t.Helper()
	d := &workerFixture{source: &workerSourceFixture{value: api.WorkDispatchSource{NodeID: "host-node", Backend: "codex", BindingID: "native-host-thread", OperationID: "native-host-turn", Kind: "native_activation"}}}
	d.f = &sessionFixture{workers: map[string]nativeThread{}, answers: make(chan wireMessage, 16), started: make(chan struct{}, 16)}
	d.w = NewWorker(WorkerOptions{Target: api.WorkTarget{NodeID: "linux-worker", Backend: "codex", Role: api.RoleWorker}, Directory: t.TempDir(), Source: d.source})
	d.f.handle = func(m wireMessage) (any, bool) {
		var p map[string]any
		_ = json.Unmarshal(m.Params, &p)
		d.f.mu.Lock()
		defer d.f.mu.Unlock()
		switch m.Method {
		case "initialize":
			d.initializes++
			return map[string]string{"userAgent": "codex-fixture"}, true
		case "initialized":
			return map[string]any{}, true
		case "config/read":
			return map[string]any{"config": map[string]any{"model": "node-native-model", "model_reasoning_effort": "medium"}}, true
		case "thread/start":
			d.starts++
			d.threadParams = p
			thread := nativeThread{ID: fmt.Sprintf("worker-native-%d", d.starts)}
			d.f.workers[thread.ID] = thread
			if d.advanceOnStart {
				d.source.next()
			}
			if d.startUnknown {
				return map[string]any{}, true
			}
			return threadExecutionResponse{Thread: thread, Model: "node-native-model", ModelProvider: "native-provider", ReasoningEffort: "medium"}, true
		case "thread/resume":
			id, _ := p["threadId"].(string)
			thread, exists := d.f.workers[id]
			if !exists {
				return &NativeError{Code: -32000, Message: "unowned thread"}, true
			}
			d.resumed = append(d.resumed, id)
			return threadExecutionResponse{Thread: thread, Model: "node-native-model", ModelProvider: "native-provider", ReasoningEffort: "medium"}, true
		case "turn/start", "turn/steer":
			id, _ := p["threadId"].(string)
			thread, exists := d.f.workers[id]
			if !exists {
				return &NativeError{Code: -32000, Message: "unowned thread"}, true
			}
			d.sends++
			d.turnParams = p
			turn := nativeTurn{ID: fmt.Sprintf("worker-turn-%d", d.sends), Status: "inProgress", Items: []nativeItem{{ID: "user", Type: "userMessage", ClientID: p["clientUserMessageId"].(string)}}}
			if m.Method == "turn/steer" {
				turn.ID = p["expectedTurnId"].(string)
			}
			thread.Turns = append(thread.Turns, turn)
			thread.Status.Type = "active"
			d.f.workers[id] = thread
			if d.sendUnknown {
				return map[string]any{}, true
			}
			if m.Method == "turn/steer" {
				return map[string]string{"turnId": turn.ID}, true
			}
			return map[string]any{"turn": turn}, true
		case "turn/interrupt":
			d.interrupts = append(d.interrupts, map[string]string{"threadId": p["threadId"].(string), "turnId": p["turnId"].(string)})
			return nil, false // Existing protocol fixture emits the actual terminal event.
		}
		return nil, false
	}
	d.w.open = func(ctx context.Context, opts Options) (*Client, func(), string, error) {
		a, b := net.Pipe()
		d.f.mu.Lock()
		d.f.peer = b
		d.f.connections++
		d.f.mu.Unlock()
		go d.f.serve(b)
		c, err := initializeClient(ctx, a, nil, opts)
		var stop func()
		if opts.Socket == "" {
			stop = func() { d.stops.Add(1) }
		}
		return c, stop, "fixture-private-endpoint", err
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = d.w.Close(ctx)
	})
	if err := d.w.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	return d
}

func (d *workerFixture) start(t *testing.T, n int) (api.WorkStart, api.Task, error) {
	t.Helper()
	id := fmt.Sprintf("task-%032x", n)
	workspace, err := d.w.ResolveWorkWorkspace(testContext(t), id, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.w.PrepareWorkWorkspace(testContext(t), id, workspace, false); err != nil {
		t.Fatal(err)
	}
	source, _ := d.source.WorkDispatchSource(testContext(t))
	target := d.w.target
	in := api.WorkStart{TaskStart: api.TaskStart{RequestID: fmt.Sprintf("worker-start-%d", n), Title: "Fixture assignment", Prompt: "Write an isolated synthetic result.", Target: &target}, ID: id, Workspace: workspace, Instructions: botpolicy.WorkerInstructions, Source: source, RequestDigest: strings.Repeat("a", 64)}
	v, err := d.w.StartWork(testContext(t), in)
	return in, v, err
}

func TestWorkerOnlyNativeHandshakeAndTargetPolicy(t *testing.T) {
	d := workerPair(t)
	s := d.w.engine
	s.mu.Lock()
	resident, run, delegation := s.binding.ThreadID, s.run, s.binding.DelegationText
	s.mu.Unlock()
	d.f.mu.Lock()
	starts, initializes := d.starts, d.initializes
	d.f.mu.Unlock()
	if resident != "" || run != "" || delegation != "" || starts != 0 || initializes != 1 {
		t.Fatal("Worker fabricated a resident activation")
	}
	in, v, err := d.start(t, 1)
	if err != nil || v.Outcome != "accepted" || v.Status != "working" || *v.Target != d.w.target {
		t.Fatal(v, err)
	}
	d.f.mu.Lock()
	p, turn := d.threadParams, d.turnParams
	d.f.mu.Unlock()
	if p["cwd"] != in.Workspace || p["model"] != "node-native-model" || p["approvalsReviewer"] != "auto_review" {
		t.Fatal("lost target-side policy", p)
	}
	config := p["config"].(map[string]any)
	if config["agents.enabled"] != false || config["mcp_servers.caelis_bot"].(map[string]any)["enabled"] != false {
		t.Fatal("Worker gained resident tools")
	}
	if roots := turn["sandboxPolicy"].(map[string]any)["writableRoots"].([]any); len(roots) != 1 || roots[0] != in.Workspace {
		t.Fatal("wrong writable scope", roots)
	}
	if _, exists := turn["dispatchSource"]; exists {
		t.Fatal("authority leaked into model arguments")
	}
	v.Target.NodeID = "caller-edited"
	got, err := d.w.ReadWork(testContext(t), in.ID)
	if err != nil || got.Target.NodeID != d.w.target.NodeID {
		t.Fatal("mutable target escaped", got, err)
	}
}

func TestWorkerOriginalIntentQueryDoesNotRequireNewActivation(t *testing.T) {
	d := workerPair(t)
	in, _, err := d.start(t, 1)
	if err != nil {
		t.Fatal(err)
	}
	d.source.next()
	if _, err = d.w.StartWork(testContext(t), in); err != nil {
		t.Fatal("original receipt needs a fresh activation", err)
	}
	for _, change := range []func(*api.WorkStart){func(v *api.WorkStart) { v.Prompt += "changed" }, func(v *api.WorkStart) { v.Instructions += "changed" }, func(v *api.WorkStart) { v.RequestDigest = strings.Repeat("b", 64) }, func(v *api.WorkStart) { v.Source.BindingID += "changed" }, func(v *api.WorkStart) { x := *v.Target; x.NodeID = "other"; v.Target = &x }} {
		altered := in
		change(&altered)
		if _, err = d.w.StartWork(testContext(t), altered); err == nil {
			t.Fatal("changed original intent accepted")
		}
	}
	msg := api.TaskMessage{ID: in.ID, RequestID: in.RequestID, Prompt: in.Prompt, Source: in.Source, RequestDigest: in.RequestDigest}
	if _, err = d.w.SendWork(testContext(t), msg); err != nil || !d.w.WorkMessageRecorded(msg) {
		t.Fatal("original receipt query failed", err)
	}
	msg.RequestID = "fresh-continuation"
	if _, err = d.w.SendWork(testContext(t), msg); err == nil {
		t.Fatal("stale native source authorized a new mutation")
	}
	msg.Source, _ = d.source.WorkDispatchSource(testContext(t))
	if _, err = d.w.SendWork(testContext(t), msg); err != nil {
		t.Fatal(err)
	}
	d.source.next()
	if _, err = d.w.SendWork(testContext(t), msg); err != nil {
		t.Fatal("saved continuation receipt rejected", err)
	}
	msg.Source.OperationID += "changed"
	if _, err = d.w.SendWork(testContext(t), msg); err == nil || d.w.WorkMessageRecorded(msg) {
		t.Fatal("changed receipt source accepted")
	}
	d.f.mu.Lock()
	starts, sends := d.starts, d.sends
	d.f.mu.Unlock()
	if starts != 1 || sends != 2 {
		t.Fatal("receipt replay dispatched work", starts, sends)
	}
}

func TestWorkerExpiredNativeGrantCannotDispatchAfterThreadReceipt(t *testing.T) {
	d := workerPair(t)
	d.f.mu.Lock()
	d.advanceOnStart = true
	d.f.mu.Unlock()
	in, view, err := d.start(t, 1)
	if err == nil || view.Outcome != "rejected" {
		t.Fatal("expired native invocation dispatched", view, err)
	}
	d.f.mu.Lock()
	starts, sends := d.starts, d.sends
	d.f.mu.Unlock()
	if starts != 1 || sends != 0 {
		t.Fatal("model request escaped native grant", starts, sends)
	}
	if original, err := d.w.StartWork(testContext(t), in); err != nil || original.Outcome != "rejected" {
		t.Fatal("definite original rejection was retried", original, err)
	}
}

func TestWorkerUnconfirmedCreateNeverAdoptsOrRedispatches(t *testing.T) {
	d := workerPair(t)
	d.f.mu.Lock()
	d.startUnknown = true
	d.f.mu.Unlock()
	in, v, err := d.start(t, 1)
	if err == nil || v.Status != "unknown" || v.Outcome != "unknown" {
		t.Fatal(v, err)
	}
	d.source.next()
	if _, err = d.w.StartWork(testContext(t), in); err != nil {
		t.Fatal("original unknown intent unavailable", err)
	}
	if view, readErr := d.w.ReadWork(testContext(t), in.ID); readErr == nil || view.Outcome != "unknown" {
		t.Fatal("unconfirmed native identity was treated as readable", view, readErr)
	}
	d.f.mu.Lock()
	starts, sends := d.starts, d.sends
	d.f.mu.Unlock()
	if starts != 1 || sends != 0 {
		t.Fatal("lost create receipt dispatched/adopted", starts, sends)
	}
	if err = d.w.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	d.w.engine.mu.Lock()
	thread := d.w.engine.binding.Tasks[in.ID].Thread
	d.w.engine.mu.Unlock()
	if thread != "" {
		t.Fatal("unknown create adopted an unrelated native thread")
	}
}

func TestWorkerLostTurnReceiptReconcilesOriginalNativeClientID(t *testing.T) {
	d := workerPair(t)
	d.f.mu.Lock()
	d.sendUnknown = true
	d.f.mu.Unlock()
	in, _, err := d.start(t, 1)
	if err == nil {
		t.Fatal("missing turn receipt accepted")
	}
	d.source.next()
	awaitState(t, d.w.engine, func(api.Snapshot) bool {
		states := d.w.WorkStates()
		return len(states) == 1 && states[0].Task.Outcome == "accepted"
	})
	v, err := d.w.StartWork(testContext(t), in)
	if err != nil || v.Outcome != "accepted" {
		t.Fatal("original client ID did not reconcile", v, err)
	}
	d.f.mu.Lock()
	starts, sends := d.starts, d.sends
	d.f.mu.Unlock()
	if starts != 1 || sends != 1 {
		t.Fatal("uncertainty was retried", starts, sends)
	}
}

func TestWorkerNativeApprovalAndCancelStayOnOwnedTurn(t *testing.T) {
	d := workerPair(t)
	in, _, err := d.start(t, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, other, err := d.start(t, 2)
	if err != nil {
		t.Fatal(err)
	}
	s := d.w.engine
	s.mu.Lock()
	task := s.binding.Tasks[in.ID]
	thread, run := task.Thread, task.Run
	s.mu.Unlock()
	d.f.emit(wireMessage{ID: raw("worker-approval-original"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{"threadId": thread, "turnId": run, "itemId": "owned-command", "command": "synthetic", "cwd": in.Workspace, "availableDecisions": []string{"accept", "decline"}})})
	awaitState(t, s, func(api.Snapshot) bool { return len(d.w.WorkApprovals()) == 1 })
	a := d.w.WorkApprovals()[0]
	decision := api.Decision{ID: a.Approval.ID, Choice: a.Approval.Choices[1].ID}
	wrong := a
	wrong.TaskID = other.ID
	if d.w.DecideWork(testContext(t), wrong, decision) == nil {
		t.Fatal("approval adopted another task")
	}
	wrong = a
	wrong.Target.NodeID = "other"
	if d.w.DecideWork(testContext(t), wrong, decision) == nil {
		t.Fatal("approval changed target")
	}
	bad := decision
	bad.Choice = "invented"
	if d.w.DecideWork(testContext(t), a, bad) == nil {
		t.Fatal("invented native choice")
	}
	if err = d.w.DecideWork(testContext(t), a, decision); err != nil {
		t.Fatal(err)
	}
	select {
	case reply := <-d.f.answers:
		if string(reply.ID) != `"worker-approval-original"` || !strings.Contains(string(reply.Result), "decline") {
			t.Fatal("changed native decision identity", reply)
		}
	case <-testContext(t).Done():
		t.Fatal("native decision missing")
	}
	if d.w.DecideWork(testContext(t), a, decision) == nil {
		t.Fatal("stale approval reused")
	}
	if _, err = d.w.StopWork(testContext(t), in.ID); err != nil {
		t.Fatal(err)
	}
	awaitState(t, s, func(api.Snapshot) bool {
		v, e := d.w.ReadWork(testContext(t), in.ID)
		return e == nil && v.Status == "interrupted"
	})
	v, err := d.w.ReadWork(testContext(t), other.ID)
	if err != nil || v.Status != "working" {
		t.Fatal("cancel interrupted another worker", v, err)
	}
	d.f.mu.Lock()
	interrupts := append([]map[string]string(nil), d.interrupts...)
	d.f.mu.Unlock()
	if len(interrupts) != 1 || interrupts[0]["threadId"] != thread || interrupts[0]["turnId"] != run {
		t.Fatal("cancel lost exact turn", interrupts)
	}
}

func TestWorkerOwnerDetachAndNativeDisconnectPreserveProcess(t *testing.T) {
	d := workerPair(t)
	o := nodeworker.New(d.w)
	ctx, cancel := context.WithCancel(testContext(t))
	if err := o.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	first := o.Observe(testContext(t))
	second := o.Observe(testContext(t))
	first.Detach()
	if _, err := first.Snapshot(); err == nil {
		t.Fatal("detached observer still active")
	}
	if _, err := second.Snapshot(); err != nil {
		t.Fatal(err)
	}
	in, v, err := d.start(t, 1)
	if err != nil {
		t.Fatal(err)
	}
	d.f.mu.Lock()
	peer := d.f.peer
	d.f.mu.Unlock()
	peer.Close()
	awaitState(t, d.w.engine, func(v api.Snapshot) bool { return v.Connection != "ready" })
	if d.stops.Load() != 0 {
		t.Fatal("observation disconnect stopped native owner")
	}
	if err = o.Start(testContext(t)); err != nil {
		t.Fatal(err)
	}
	after, err := d.w.ReadWork(testContext(t), in.ID)
	if err != nil || after.ID != v.ID || after.Status != "working" {
		t.Fatal("reconnect replaced native task", after, err)
	}
	d.f.mu.Lock()
	starts, resumed := d.starts, append([]string(nil), d.resumed...)
	d.f.mu.Unlock()
	if starts != 1 || len(resumed) == 0 || resumed[0] != "worker-native-1" {
		t.Fatal("reconnect recreated native binding", starts, resumed)
	}
	if err = o.Stop(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if err = o.Stop(testContext(t)); err != nil || d.stops.Load() != 1 {
		t.Fatal("owner stop was not exact once", err, d.stops.Load())
	}
	if _, err = second.WaitSnapshot(0); err == nil {
		t.Fatal("stopped owner observer survived")
	}
	if err = o.Start(testContext(t)); err == nil {
		t.Fatal("stopped owner restarted")
	}
}

func TestWorkerRestoreRejectsResidentAndChangedTarget(t *testing.T) {
	d := workerPair(t)
	in, _, err := d.start(t, 1)
	if err != nil {
		t.Fatal(err)
	}
	opts := WorkerOptions{Target: d.w.target, Directory: d.w.engine.opts.Directory, Source: d.source}
	d.w.engine.mu.Lock()
	d.w.engine.binding.Tasks[in.ID].View.Target.NodeID = "other"
	err = d.w.engine.save()
	d.w.engine.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(opts)
	if err = w.Connect(testContext(t)); err == nil {
		t.Fatal("persisted target changed")
	}
	residentDir := t.TempDir()
	b, err := json.Marshal(binding{Version: 1, ThreadID: "resident-native"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(residentDir, "worker-bindings.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	w = NewWorker(WorkerOptions{Target: d.w.target, Directory: residentDir, Source: d.source})
	if err = w.Connect(testContext(t)); err == nil {
		t.Fatal("Worker adopted resident Bot state")
	}
}

func TestWorkerRestoreRetainsReceiptButRejectsChangedNativeBinding(t *testing.T) {
	d := workerPair(t)
	in, _, err := d.start(t, 1)
	if err != nil {
		t.Fatal(err)
	}
	opts := WorkerOptions{Target: d.w.target, Directory: d.w.engine.opts.Directory, Source: d.source}
	d.source.next()
	w := NewWorker(opts)
	w.open = d.w.open // Same native fixture server, no resident creation.
	if err = w.Connect(testContext(t)); err != nil {
		t.Fatal("retained native receipt unavailable", err)
	}
	t.Cleanup(func() { _ = w.Close(testContext(t)) })
	if _, err = w.StartWork(testContext(t), in); err != nil {
		t.Fatal("original activation was lost on restore", err)
	}
	w.engine.mu.Lock()
	w.engine.binding.Tasks[in.ID].Thread = "foreign-native-thread"
	err = w.engine.save()
	w.engine.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	broken := NewWorker(opts)
	if err = broken.Connect(testContext(t)); err == nil {
		t.Fatal("changed persisted native binding restored")
	}
}
