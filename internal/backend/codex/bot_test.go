package codex

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"strings"
	"testing"
)

func TestOwnedWorkerApprovalRoutesWithoutLeakingWorkerMessages(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "root-prompt")
	f.emit(wireMessage{Method: "thread/started", Params: raw(map[string]any{"thread": nativeThread{ID: "worker", ParentThreadID: "thread-native"}})})
	f.emit(wireMessage{Method: "turn/started", Params: raw(map[string]any{"threadId": "worker", "turn": nativeTurn{ID: "worker-turn", Status: "inProgress"}})})
	f.emit(wireMessage{Method: "item/agentMessage/delta", Params: raw(map[string]any{"threadId": "worker", "turnId": "worker-turn", "itemId": "private", "delta": "private worker chatter"})})
	f.emit(wireMessage{ID: raw("worker-approval"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{"threadId": "worker", "turnId": "worker-turn", "itemId": "command", "command": "synthetic", "availableDecisions": []any{"accept", "decline"}})})
	view := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	for _, item := range view.Items {
		if item.Text == "private worker chatter" {
			t.Fatal("worker became user chat")
		}
	}
	if err := s.Decide(testContext(t), api.Decision{ID: view.Approvals[0].ID, Choice: view.Approvals[0].Choices[0].ID}); err != nil {
		t.Fatal(err)
	}
	answer := <-f.answers
	if string(answer.ID) != string(raw("worker-approval")) {
		t.Fatal("decision routed to wrong request")
	}
	f.emit(wireMessage{Method: "serverRequest/resolved", Params: raw(map[string]any{"threadId": "worker", "requestId": "worker-approval"})})
	view = awaitState(t, s, func(v api.Snapshot) bool { return v.Approvals[0].Status == "resolved" })
	if !view.CanInterrupt {
		t.Fatal("worker lifecycle lost")
	}
	if err := s.Interrupt(testContext(t)); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().CanInterrupt {
		t.Fatal("owned worker survived explicit stop")
	}
}
func TestUnrelatedThreadCannotJoinBotByClaimingAnApproval(t *testing.T) {
	s, f := sessionPair(t, "hold")
	f.emit(wireMessage{ID: raw("foreign"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{"threadId": "unrelated", "turnId": "other", "command": "synthetic"})})
	answer := <-f.answers
	if answer.Error == nil || len(s.Snapshot().Approvals) != 0 {
		t.Fatal("unowned approval accepted")
	}
}

func TestBotConnectionKeepsSandboxAndScopesDiscovery(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir()})
	p := s.connectionParams()
	if p["approvalsReviewer"] != "auto_review" || p["approvalPolicy"] != "on-request" || p["sandbox"] != "workspace-write" {
		t.Fatal("native policy changed", p)
	}
	c := p["config"].(map[string]any)
	if len(c) != 0 {
		t.Fatal("unexpected configuration before tool injection")
	}
	if strings.Contains(p["developerInstructions"].(string), "- bot_clock:") {
		t.Fatal("unconfigured tool advertised")
	}
	s.opts.BotTools = &api.ToolConnection{Command: "synthetic"}
	p = s.connectionParams()
	if !strings.Contains(p["developerInstructions"].(string), "- bot_clock:") || len(p["runtimeWorkspaceRoots"].([]string)) != 0 {
		t.Fatal("catalog/workspace contract")
	}
	s.opts.RequireApproval = true
	p = s.connectionParams()
	if p["approvalsReviewer"] != "user" || p["approvalPolicy"] != "untrusted" {
		t.Fatal("synthetic approval override weakened")
	}
}
func TestV2WorkerActivityOwnsTargetAndObservesNativeIdle(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "root-prompt")
	worker := nativeThread{ID: "worker-v2", ParentThreadID: "thread-native", Turns: []nativeTurn{{ID: "worker-turn", Status: "inProgress"}}}
	worker.Status.Type = "active"
	f.mu.Lock()
	f.workers = map[string]nativeThread{"worker-v2": worker}
	f.mu.Unlock()
	f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "item": nativeItem{ID: "spawn", Type: "subAgentActivity", AgentThreadID: "worker-v2", ActivityKind: "started"}})})
	awaitState(t, s, func(v api.Snapshot) bool { return len(v.Items) > 0 })
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "run-native", Status: "completed"}})})
	v := awaitState(t, s, func(v api.Snapshot) bool { return v.CurrentTurn == "" })
	if v.CanSend || !v.CanInterrupt || v.Phase != "working" {
		t.Fatal("root completion abandoned worker", v)
	}
	worker.Status.Type = "idle"
	worker.Turns = []nativeTurn{{ID: "worker-turn", Status: "completed"}}
	f.mu.Lock()
	f.workers["worker-v2"] = worker
	f.mu.Unlock()
	f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "item": nativeItem{ID: "communicate", Type: "subAgentActivity", AgentThreadID: "worker-v2", ActivityKind: "interacted"}})})
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend && !v.CanInterrupt })
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.children["worker-v2"] || len(s.binding.Children) != 1 {
		t.Fatal("v2 ownership not persisted")
	}
}

func TestWorkerObservationFailureCanReconnectWithoutReplaying(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "root-prompt")
	f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "item": nativeItem{ID: "spawn", Type: "subAgentActivity", AgentThreadID: "worker-v2", ActivityKind: "started"}})})
	// Fixture returns the root for this missing worker: reject mismatched identity.
	v := awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "unknown" })
	if v.CanSend {
		t.Fatal("unconfirmed worker permitted new work")
	}
	worker := nativeThread{ID: "worker-v2", Turns: []nativeTurn{{ID: "w", Status: "completed"}}}
	worker.Status.Type = "idle"
	f.mu.Lock()
	f.workers = map[string]nativeThread{"worker-v2": worker}
	starts := f.starts
	f.mu.Unlock()
	if err := s.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "working" && v.Message == "" })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.starts != starts {
		t.Fatal("reconnect replayed input")
	}
}
