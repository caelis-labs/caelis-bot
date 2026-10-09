package codex

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"strings"
	"testing"
)

func TestBuiltinMCPServiceScopesAndLegacyOwner(t *testing.T) {
	defs := []api.ToolDefinition{
		{Name: "bot_tasks", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "bot_schedule", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	raw, _ := json.Marshal(defs)
	c := &api.ToolConnection{Command: "/bin/false", Args: []string{"--bot-tools"}, Env: map[string]string{"CAELIS_BOT_TOOL_CATALOG": string(raw)}, ApprovedTools: []string{"bot_tasks", "bot_schedule"}, Services: []api.ToolService{{Name: "caelis_tasks", Tools: []string{"bot_tasks"}}, {Name: "caelis_schedule", Tools: []string{"bot_schedule"}}}}
	s := NewSession(SessionOptions{Directory: t.TempDir(), BotTools: c})
	config := s.connectionParams()["config"].(map[string]any)
	if config["mcp_servers.caelis_bot"] != nil || config["mcp_servers.caelis_tasks"] == nil || config["mcp_servers.caelis_schedule"] == nil {
		t.Fatal("new Bot thread did not use partitioned services")
	}
	tasks := config["mcp_servers.caelis_tasks"].(map[string]any)
	var catalog []api.ToolDefinition
	if err := json.Unmarshal([]byte(tasks["env"].(map[string]string)["CAELIS_BOT_TOOL_CATALOG"]), &catalog); err != nil || len(catalog) != 1 || catalog[0].Name != "bot_tasks" {
		t.Fatal("task service gained another tool", catalog, err)
	}
	if approved := tasks["tools"].(map[string]any); len(approved) != 1 || approved["bot_tasks"] == nil {
		t.Fatal("task approval list broadened", approved)
	}
	s.binding.ThreadID = "legacy-thread"
	config = s.connectionParams()["config"].(map[string]any)
	if config["mcp_servers.caelis_bot"] == nil || config["mcp_servers.caelis_tasks"] != nil {
		t.Fatal("legacy thread exposed two owners")
	}
	s.binding.ToolLayout = 2
	config = s.connectionParams()["config"].(map[string]any)
	if config["mcp_servers.caelis_bot"] != nil || config["mcp_servers.caelis_tasks"] == nil {
		t.Fatal("partitioned thread lost its owner")
	}
}

func TestOwnedWorkerApprovalRoutesWithoutLeakingWorkerMessages(t *testing.T) {
	s, f := sessionPair(t, "hold")
	sendSynthetic(t, s, "root-prompt")
	worker := nativeThread{ID: "worker", ParentThreadID: "thread-native", Turns: []nativeTurn{{ID: "worker-turn", Status: "inProgress"}}}
	worker.Status.Type = "active"
	f.mu.Lock()
	f.workers = map[string]nativeThread{"worker": worker}
	f.mu.Unlock()
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
	s.opts.BotTools = &api.ToolConnection{Command: "synthetic", Instructions: "- bot_clock: synthetic catalog", SkillInstructions: "\nResident Skill guide"}
	p = s.connectionParams()
	if !strings.Contains(p["developerInstructions"].(string), "- bot_clock:") || !strings.Contains(p["developerInstructions"].(string), "Resident Skill guide") || len(p["runtimeWorkspaceRoots"].([]string)) != 0 {
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
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "worker-v2", "turn": worker.Turns[0]})})
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend && !v.CanInterrupt })
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.children["worker-v2"] || len(s.binding.Children) != 1 {
		t.Fatal("v2 ownership not persisted")
	}
}

func TestWorkerObservationFailureCanReconnectWithoutReplaying(t *testing.T) {
	for _, status := range []string{"active", "idle"} {
		t.Run(status, func(t *testing.T) {
			s, f := sessionPair(t, "hold")
			sendSynthetic(t, s, "root-prompt")
			awaitState(t, s, func(v api.Snapshot) bool { return v.CurrentTurn == opaque("run-native") })
			resumeStarted, releaseResume := make(chan struct{}), make(chan struct{})
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method == "thread/resume" && strings.Contains(string(m.Params), `"threadId":"worker-v2"`) {
					close(resumeStarted)
					<-releaseResume
					return map[string]any{"thread": nativeThread{ID: "wrong-thread"}}, true
				}
				return nil, false
			}
			f.mu.Unlock()
			f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": "thread-native", "turnId": "run-native", "item": nativeItem{ID: "spawn", Type: "subAgentActivity", AgentThreadID: "worker-v2", ActivityKind: "started"}})})
			<-resumeStarted
			finishRoot(f)
			awaitState(t, s, func(v api.Snapshot) bool { return v.CurrentTurn == "" })
			close(releaseResume)
			v := awaitState(t, s, func(v api.Snapshot) bool { return v.Message == workerUnconfirmed })
			if !v.CanSend || v.Connection != "ready" {
				t.Fatal("worker observation failure blocked resident control")
			}
			worker := nativeThread{ID: "worker-v2", Turns: []nativeTurn{{ID: "w", Status: "inProgress"}}}
			worker.Status.Type = "active"
			wantPhase, wantSend := "working", false
			if status == "idle" {
				worker.Status.Type = "idle"
				worker.Turns[0].Status = "completed"
				wantPhase, wantSend = "completed", true
			}
			f.mu.Lock()
			f.handle = nil
			f.workers = map[string]nativeThread{"worker-v2": worker}
			starts := f.starts
			f.mu.Unlock()
			if err := s.Connect(testContext(t)); err != nil {
				t.Fatal(err)
			}
			awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == wantPhase && v.CanSend == wantSend && v.Message == "" })
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.starts != starts {
				t.Fatal("reconnect replayed input")
			}
		})
	}
}
