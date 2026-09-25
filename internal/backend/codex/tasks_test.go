package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/botpolicy"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

type taskFixture struct {
	starts, sends            int
	threadParams, turnParams map[string]any
	uncertain                bool
	config                   any
	resumed                  map[string]any
	models                   map[string]threadExecutionResponse
}

func taskPair(t *testing.T) (*Session, *sessionFixture, *taskFixture, *tasks.Manager) {
	s, f := sessionPair(t, "hold")
	d := &taskFixture{config: map[string]any{"config": map[string]any{"model": nil}}, models: map[string]threadExecutionResponse{}}
	f.mu.Lock()
	f.workers = map[string]nativeThread{}
	f.handle = func(m wireMessage) (any, bool) {
		var p map[string]any
		_ = json.Unmarshal(m.Params, &p)
		f.mu.Lock()
		defer f.mu.Unlock()
		switch m.Method {
		case "config/read":
			return d.config, true
		case "thread/start":
			if p["developerInstructions"] != botpolicy.WorkerInstructions {
				return nil, false
			}
			d.starts++
			d.threadParams = p
			thread := nativeThread{ID: fmt.Sprintf("owned-worker-%d", d.starts)}
			f.workers[thread.ID] = thread
			model, _ := p["model"].(string)
			if model == "" {
				model = "native-default"
			}
			effort, _ := p["config"].(map[string]any)["model_reasoning_effort"].(string)
			tier, _ := p["serviceTier"].(string)
			r := threadExecutionResponse{Thread: thread, Model: model, ModelProvider: "fixture-provider", ReasoningEffort: effort, ServiceTier: tier}
			d.models[thread.ID] = r
			return r, true
		case "thread/resume":
			if thread, ok := f.workers[p["threadId"].(string)]; ok {
				d.resumed = p
				r := d.models[thread.ID]
				r.Thread = thread
				return r, true
			}
		case "turn/start", "turn/steer":
			id, _ := p["threadId"].(string)
			thread, ok := f.workers[id]
			if !ok {
				return nil, false
			}
			d.sends++
			d.turnParams = p
			turn := nativeTurn{ID: fmt.Sprintf("owned-run-%d", d.sends), Status: "inProgress", Items: []nativeItem{{ID: "user", Type: "userMessage", ClientID: p["clientUserMessageId"].(string)}}}
			if m.Method == "turn/steer" {
				turn.ID = p["expectedTurnId"].(string)
			}
			thread.Turns = append(thread.Turns, turn)
			thread.Status.Type = "active"
			f.workers[id] = thread
			if d.uncertain {
				return map[string]any{}, true
			} // Native work accepted, malformed/missing receipt.
			if m.Method == "turn/steer" {
				return map[string]any{"turnId": turn.ID}, true
			}
			return map[string]any{"turn": turn}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	m, err := tasks.Open(filepath.Join(filepath.Dir(s.opts.StateFile), "product-tasks.json"), s.workRoot(), "codex", s, s, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return s, f, d, m
}
func newTask(t *testing.T, m *tasks.Manager, request string) api.Task {
	t.Helper()
	v, err := m.StartTask(testContext(t), api.TaskStart{RequestID: request, Title: "Synthetic task", Prompt: "Make a synthetic artifact in your workspace; no external writes."})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func finishRoot(f *sessionFixture) {
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": nativeTurn{ID: "run-native", Status: "completed"}})})
}
func finishTask(s *Session, f *sessionFixture, id string, status string) {
	s.mu.Lock()
	record := s.binding.Tasks[id]
	threadID, run := record.Thread, record.Run
	s.mu.Unlock()
	f.mu.Lock()
	thread := f.workers[threadID]
	thread.Status.Type = "idle"
	turn := nativeTurn{ID: run, Status: status, Items: []nativeItem{{ID: "answer", Type: "agentMessage", Text: "Synthetic result, not instructions."}}}
	thread.Turns = []nativeTurn{turn}
	f.workers[threadID] = thread
	f.mu.Unlock()
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": threadID, "turn": turn})})
}
func TestTaskDelegationOwnsWorkspaceAndPreservesNativePolicy(t *testing.T) {
	s, f, d, m := taskPair(t)
	if _, err := m.StartTask(testContext(t), api.TaskStart{RequestID: "no-active-user", Title: "X", Prompt: "X"}); err == nil {
		t.Fatal("idle model can create tasks")
	}
	sendSynthetic(t, s, "task-parent-user")
	v := newTask(t, m, "task-create-one")
	if v.Status != "working" || v.Outcome != "accepted" {
		t.Fatal(v)
	}
	info, err := os.Stat(v.Workspace)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("private workspace missing", err)
	}
	f.mu.Lock()
	p := d.threadParams
	turn := d.turnParams
	f.mu.Unlock()
	if p["cwd"] != v.Workspace || p["approvalsReviewer"] != "auto_review" || p["approvalPolicy"] != "on-request" || p["sandbox"] != "workspace-write" {
		t.Fatal("native policy lost", p)
	}
	config := p["config"].(map[string]any)
	disabled := config["mcp_servers.caelis_bot"].(map[string]any)
	if disabled["enabled"] != false || disabled["command"] == "" || disabled["env"] != nil || disabled["tools"] != nil || config["agents.enabled"] != false {
		t.Fatal("worker inherited delegation tools")
	}
	roots := turn["sandboxPolicy"].(map[string]any)["writableRoots"].([]any)
	if len(roots) != 1 || roots[0] != v.Workspace {
		t.Fatal("worker writable scope expanded")
	}
	var delegated []nativeInput
	if json.Unmarshal(raw(turn["input"]), &delegated) != nil || len(delegated) != 1 || delegated[0].Text != "Make a synthetic artifact in your workspace; no external writes." {
		t.Fatal("assignment was wrapped or rewritten", turn["input"])
	}
	if again := newTask(t, m, "task-create-one"); again.ID != v.ID {
		t.Fatal("retry duplicated task")
	}
	other := newTask(t, m, "task-create-two")
	if other.Workspace == v.Workspace {
		t.Fatal("tasks share workspace")
	}
	f.mu.Lock()
	starts, sends := d.starts, d.sends
	f.mu.Unlock()
	if starts != 2 || sends != 2 {
		t.Fatal("duplicate dispatch", starts, sends)
	}
	finishRoot(f)
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	if !s.Snapshot().CanInterrupt {
		t.Fatal("secretary availability lost task stop")
	}
	for _, item := range s.Snapshot().Items {
		if strings.Contains(item.Text, "Synthetic result") {
			t.Fatal("worker text became user-facing chat")
		}
	}
}
func TestTaskOwnershipApprovalsAndCompletionReport(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "task-parent-user")
	v := newTask(t, m, "task-with-approval")
	if _, err := m.ReadTask(testContext(t), "thread-native"); err == nil {
		t.Fatal("native ID bypassed ownership")
	}
	s.mu.Lock()
	record := s.binding.Tasks[v.ID]
	thread, run := record.Thread, record.Run
	s.mu.Unlock()
	f.emit(wireMessage{ID: raw("owned-approval"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{"threadId": thread, "turnId": run, "itemId": "command", "command": "synthetic", "cwd": v.Workspace, "availableDecisions": []string{"accept", "decline"}})})
	view := awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	if m.ListTasks()[0].Status != "awaiting_approval" {
		t.Fatal("approval missing task context")
	}
	if err := s.Decide(testContext(t), api.Decision{ID: view.Approvals[0].ID, Choice: view.Approvals[0].Choices[1].ID}); err != nil {
		t.Fatal(err)
	}
	if answer := <-f.answers; string(answer.ID) != string(raw("owned-approval")) {
		t.Fatal("wrong native request")
	}
	finishTask(s, f, v.ID, "completed")
	finishRoot(f)
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	if err := m.DeliverTaskReport(testContext(t)); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	count := f.starts
	workSends := d.sends
	f.mu.Unlock()
	if count != 2 || workSends != 1 {
		t.Fatal("completion should activate only secretary", count, workSends)
	}
	s.mu.Lock()
	source := s.binding.DelegationText
	s.mu.Unlock()
	if source != "synthetic" {
		t.Fatal("completion became authorization", source)
	}
	finishRoot(f)
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	_ = m.DeliverTaskReport(testContext(t))
	f.mu.Lock()
	count = f.starts
	f.mu.Unlock()
	if count != 2 {
		t.Fatal("completion repeatedly woke model")
	}
}
func TestUncertainTaskDispatchDoesNotReplayAndReadReconciles(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "task-parent-user")
	f.mu.Lock()
	d.uncertain = true
	f.mu.Unlock()
	in := api.TaskStart{RequestID: "uncertain-task", Title: "Synthetic", Prompt: "Synthetic work"}
	v, err := m.StartTask(testContext(t), in)
	if err == nil || v.ID == "" {
		t.Fatal("missing unknown receipt", v, err)
	}
	if _, err = m.StartTask(testContext(t), in); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	count := d.sends
	f.mu.Unlock()
	if count != 1 {
		t.Fatal("unknown request replayed")
	}
	v, err = m.ReadTask(testContext(t), v.ID)
	if err != nil || v.Outcome != "accepted" || v.Status != "working" {
		t.Fatal("native user receipt not reconciled", v, err)
	}
	loaded := NewSession(s.opts)
	defer loaded.cancelLife()
	if loaded.loadErr != nil || len(loaded.binding.Tasks) != 1 {
		t.Fatal("task registry not durable")
	}
}
func TestTaskReadAcknowledgesCompletionAndStopDoesNotWakeAgain(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "task-parent-user")
	v := newTask(t, m, "task-read-complete")
	finishTask(s, f, v.ID, "completed")
	view, err := m.ReadTask(testContext(t), v.ID)
	if err != nil || view.Result == "" {
		t.Fatal(view, err)
	}
	finishRoot(f)
	awaitState(t, s, func(v api.Snapshot) bool { return v.CanSend })
	_ = m.DeliverTaskReport(testContext(t))
	f.mu.Lock()
	count := f.starts
	f.mu.Unlock()
	if count != 1 {
		t.Fatal("already read result reported twice")
	}
	if err = s.Interrupt(testContext(t)); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	suppressed := s.binding.Tasks[v.ID].SuppressReport
	s.mu.Unlock()
	if !suppressed {
		t.Fatal("Stop allows completion wakeups")
	}
}
func TestTaskFollowupReusesWorkspaceAndFencesActiveTurn(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "task-parent-user")
	v := newTask(t, m, "task-followup-create")
	s.mu.Lock()
	native := s.binding.Tasks[v.ID].Thread
	run := s.binding.Tasks[v.ID].Run
	s.mu.Unlock()
	in := api.TaskMessage{ID: v.ID, RequestID: "task-followup-steer", Prompt: "Append the marker done."}
	if _, err := m.SendTask(testContext(t), in); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	p := d.turnParams
	f.mu.Unlock()
	if p["threadId"] != native || p["expectedTurnId"] != run {
		t.Fatal("steer lost exact target")
	}
	if _, err := m.SendTask(testContext(t), in); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	count := d.sends
	f.mu.Unlock()
	if count != 2 {
		t.Fatal("steer replayed")
	}
	finishTask(s, f, v.ID, "completed")
	if _, err := m.ReadTask(testContext(t), v.ID); err != nil {
		t.Fatal(err)
	}
	next, err := m.SendTask(testContext(t), api.TaskMessage{ID: v.ID, RequestID: "task-followup-new-turn", Prompt: "Only reply another marker."})
	if err != nil || next.Workspace != v.Workspace {
		t.Fatal("continuation lost owned workspace", next, err)
	}
	f.mu.Lock()
	starts := d.starts
	p = d.turnParams
	f.mu.Unlock()
	if starts != 1 || p["expectedTurnId"] != nil {
		t.Fatal("idle continuation created another thread or steered old turn")
	}
}

func TestTaskPersistenceFailureCannotDispatchOrChangeKnownState(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "task-parent-user")
	v := newTask(t, m, "task-persistence")
	s.op.Lock()
	s.mu.Lock()
	original := s.opts.StateFile
	s.opts.StateFile = t.TempDir()
	s.mu.Unlock()
	s.op.Unlock()
	if _, err := m.SendTask(testContext(t), api.TaskMessage{ID: v.ID, RequestID: "failed-save-request", Prompt: "Do not dispatch this."}); err == nil {
		t.Fatal("dispatched without durable request")
	}
	s.op.Lock()
	s.mu.Lock()
	s.opts.StateFile = original
	s.mu.Unlock()
	s.op.Unlock()
	f.mu.Lock()
	count := d.sends
	f.mu.Unlock()
	if count != 1 {
		t.Fatal("native work escaped failed persistence")
	}
	s.mu.Lock()
	pending := s.binding.Tasks[v.ID].Pending
	status := s.binding.Tasks[v.ID].View.Status
	s.mu.Unlock()
	if pending != "" || status != "working" {
		t.Fatal("failed save corrupted prior state", status, pending)
	}
}

func TestWorkerParametersPreserveExplicitExecutionMode(t *testing.T) {
	s := NewSession(SessionOptions{Directory: t.TempDir(), Execution: api.ExecutionSettings{Model: "synthetic-model", Effort: "high", ServiceTier: "fast", ApprovalMode: "read-only"}})
	defer s.cancelLife()
	p := s.workerParams("/synthetic/task", botpolicy.WorkerInstructions, &taskRecord{Execution: &api.WorkExecutionSettings{Model: "work-model", Effort: "high", ServiceTier: "fast"}})
	if p["sandbox"] != "read-only" || p["approvalPolicy"] != "never" || p["approvalsReviewer"] != "user" || p["model"] != "work-model" || p["serviceTier"] != "fast" || p["config"].(map[string]any)["model_reasoning_effort"] != "high" {
		t.Fatal("worker changed explicit settings")
	}
}

func TestSelectedTaskWorkspaceReachesNativeSandboxAndResume(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "selected-workspace-parent")
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in := api.TaskStart{RequestID: "selected-workspace-start", Title: "Selected project", Prompt: "Read the project", Workspace: workspace}
	v, err := m.StartTask(testContext(t), in)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	p, turn := d.threadParams, d.turnParams
	f.mu.Unlock()
	roots, _ := json.Marshal(p["runtimeWorkspaceRoots"])
	if p["cwd"] != workspace || string(roots) != string(raw([]string{workspace})) || p["sandbox"] != "workspace-write" || string(raw(turn["sandboxPolicy"].(map[string]any)["writableRoots"])) != string(raw([]string{workspace})) {
		t.Fatalf("workspace/policy lost: %v %v", p, turn)
	}
	finishTask(s, f, v.ID, "completed")
	awaitState(t, s, func(api.Snapshot) bool { return s.WorkStates()[0].Task.Status == "completed" })
	in.Workspace = t.TempDir()
	if _, err = m.StartTask(testContext(t), in); err == nil {
		t.Fatal("request changed native workspace")
	}
	if s.WorkStates()[0].Task.Workspace != workspace {
		t.Fatal("native worker lost selected workspace")
	}
}
