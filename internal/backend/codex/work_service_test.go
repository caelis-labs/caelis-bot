package codex

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func workerNativeTarget(t *testing.T, s *Session, id string) (string, string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.binding.Tasks[id]
	return r.Thread, r.Run
}
func TestWorkerServiceObservesOnlyOwnedOutputAndFullOriginalRequests(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "work-service-parent")
	task := newTask(t, m, "work-service-create")
	thread, run := workerNativeTarget(t, s, task.ID)
	f.emit(wireMessage{Method: "item/agentMessage/delta", Params: raw(map[string]any{"threadId": thread, "turnId": run, "itemId": "native-stream", "delta": "live partial"})})
	f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": thread, "turnId": run, "item": nativeItem{ID: "async-original", Type: "agentMessage", Delivery: "async", Questions: []struct {
		Title   string   `json:"title"`
		Options []string `json:"options"`
	}{{"Choose?", []string{"one", "two"}}}}})})
	f.emit(wireMessage{ID: raw("original-secret-form"), Method: "mcpServer/elicitation/request", Params: raw(map[string]any{"threadId": thread, "turnId": run, "mode": "form", "serverName": "fixture", "message": "Secret needed", "requestedSchema": map[string]any{"type": "object", "required": []string{"secret"}, "properties": map[string]any{"secret": map[string]any{"type": "string", "format": "password"}}}})})
	awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) > 0 })
	obs, err := s.ObserveWork(testContext(t), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs.Approvals) != 1 || obs.Approvals[0].TaskID != task.ID || !obs.Approvals[0].Questions[0].Secret || len(obs.State.AsyncQuestions) != 1 || obs.State.AsyncQuestions[0].AsyncCallID != "async-original" {
		t.Fatalf("full native projection lost: %+v", obs)
	}
	found := false
	for _, i := range obs.Items {
		if i.Text == "live partial" {
			found = true
		}
	}
	if !found {
		t.Fatal("live partial output missing", obs.Items)
	}
	for _, i := range s.Snapshot().Items {
		if i.Text == "live partial" {
			t.Fatal("worker output leaked into resident")
		}
	}
	if _, err := s.ObserveWork(testContext(t), "unowned"); err == nil {
		t.Fatal("foreign Worker observed")
	}
	// An unanswered question remains bound to its original call across turns.
	finishTask(s, f, task.ID, "completed")
	awaitState(t, s, func(api.Snapshot) bool {
		v, _ := s.ObserveWork(testContext(t), task.ID)
		return v.State.Task.Status == "completed"
	})
	_, err = s.SendWork(testContext(t), api.TaskMessage{ID: task.ID, RequestID: "next-worker-turn", Prompt: "Continue"})
	if err != nil {
		t.Fatal(err)
	}
	obs, err = s.ObserveWork(testContext(t), task.ID)
	if err != nil || len(obs.State.AsyncQuestions) != 1 || obs.State.AsyncQuestions[0].AsyncCallID != "async-original" {
		t.Fatal("new turn expired original unanswered question", obs.State, err)
	}
}

func TestWorkerServiceNativeURLChallengeAndOriginalDecisionReceipt(t *testing.T) {
	var completed atomic.Bool
	challenge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		completed.Store(true)
		_, _ = io.WriteString(w, "fixture challenge complete")
	}))
	defer challenge.Close()
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "url-service-parent")
	task := newTask(t, m, "url-service-worker")
	thread, run := workerNativeTarget(t, s, task.ID)
	f.emit(wireMessage{ID: raw("native-url-original"), Method: "mcpServer/elicitation/request", Params: raw(map[string]any{"threadId": thread, "turnId": run, "mode": "url", "serverName": "fixture challenge", "url": challenge.URL, "message": "Finish external challenge"})})
	awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	obs, err := s.ObserveWork(testContext(t), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	approval := obs.Approvals[0]
	u, err := s.WorkApprovalURL(testContext(t), task.ID, approval.ID)
	if err != nil || u != challenge.URL {
		t.Fatal("URL not native", u, err)
	}
	if _, err := s.WorkApprovalURL(testContext(t), "other-task", approval.ID); err == nil {
		t.Fatal("URL escaped task scope")
	}
	select {
	case answer := <-f.answers:
		t.Fatal("URL implicitly approved", answer)
	default:
	}
	response, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if !completed.Load() {
		t.Fatal("native URL challenge not completed")
	}
	in := api.WorkDecision{TaskID: task.ID, RequestID: "original-url-decision", Decision: api.Decision{ID: approval.ID, Choice: "accept"}}
	if err := s.DecideWork(testContext(t), in); err != nil {
		t.Fatal(err)
	}
	answer := <-f.answers
	if string(answer.ID) != `"native-url-original"` || string(answer.Result) != `{"_meta":null,"action":"accept","content":null}` {
		t.Fatalf("native target or decision changed: %s %s", answer.ID, answer.Result)
	}
	q := api.WorkOperationQuery{TaskID: task.ID, RequestID: in.RequestID, Kind: "decision", Fingerprint: api.WorkFingerprint(in)}
	receipt, err := s.ReadWorkOperation(testContext(t), q)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	if err := s.DecideWork(testContext(t), in); err != nil {
		t.Fatal("original accepted receipt not reusable", err)
	}
	select {
	case answer := <-f.answers:
		t.Fatal("accepted native decision replayed", answer)
	default:
	}
	f.emit(wireMessage{Method: "serverRequest/resolved", Params: raw(map[string]any{"threadId": thread, "requestId": "native-url-original"})})
	awaitState(t, s, func(v api.Snapshot) bool {
		return len(v.Approvals) == 1 && v.Approvals[0].Status == "resolved" && v.Approvals[0].Resolution != nil
	})
	resolved, _ := s.ObserveWork(testContext(t), task.ID)
	if resolved.Approvals[0].Resolution == nil || resolved.Approvals[0].Resolution.ChoiceID != "accept" {
		t.Fatal("native Worker resolution lost original decision", resolved.Approvals)
	}
	in.Decision.Choice = "cancel"
	if err := s.DecideWork(testContext(t), in); err == nil {
		t.Fatal("same operation ID changed intent")
	}
}

func TestWorkerServiceStopAtomicallyFencesNextGenerationAndDoesNotReplayUnknown(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "stop-service-parent")
	task := newTask(t, m, "stop-service-worker")
	observation, err := s.ObserveWork(testContext(t), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := observation.State.StopExecutionKey
	var interrupts atomic.Int32
	f.mu.Lock()
	previous := f.handle
	f.handle = func(w wireMessage) (any, bool) {
		if w.Method == "turn/interrupt" {
			interrupts.Add(1)
			return []string{"malformed native acknowledgement"}, true
		}
		return previous(w)
	}
	f.mu.Unlock()
	// Hold the actual adapter operation lock, queue a stop, then expose a newer
	// native generation before releasing it. HTTP-level preflight cannot pass this.
	s.op.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := s.StopWorkRequest(testContext(t), api.WorkStop{ID: task.ID, RequestID: "stale-stop-original", ExecutionKey: old})
		done <- err
	}()
	s.mu.Lock()
	r := s.binding.Tasks[task.ID]
	r.Run = "new-native-generation"
	s.childRuns[r.Thread] = r.Run
	s.mu.Unlock()
	s.op.Unlock()
	if err := <-done; err == nil || interrupts.Load() != 0 {
		t.Fatal("late stop reached newer generation", err, interrupts.Load())
	}
	observation, _ = s.ObserveWork(testContext(t), task.ID)
	in := api.WorkStop{ID: task.ID, RequestID: "unknown-stop-original", ExecutionKey: observation.State.StopExecutionKey}
	if _, err := s.StopWorkRequest(testContext(t), in); err == nil {
		t.Fatal("malformed stop receipt became success")
	}
	receipt, err := s.ReadWorkOperation(testContext(t), api.WorkOperationQuery{TaskID: task.ID, RequestID: in.RequestID, Kind: "stop", Fingerprint: api.WorkFingerprint(in)})
	if err != nil || receipt.Outcome != "unknown" {
		t.Fatal(receipt, err)
	}
	if _, err := s.StopWorkRequest(testContext(t), in); err == nil || interrupts.Load() != 1 {
		t.Fatal("unknown stop was replayed", err, interrupts.Load())
	}
}

func TestWorkerServiceFilesUseNativeInputsAndTaskProvenOutputs(t *testing.T) {
	s, f, d, m := taskPair(t)
	sendSynthetic(t, s, "files-service-parent")
	task := newTask(t, m, "files-service-worker")
	input := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(input, []byte("fixture input"), 0600); err != nil {
		t.Fatal(err)
	}
	in := api.TaskMessage{ID: task.ID, RequestID: "files-input-original", Prompt: "Use attachment", Files: []api.InputFile{{Name: "source.txt", Path: input}}}
	if _, err := s.SendWork(testContext(t), in); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	encoded, _ := json.Marshal(d.turnParams["input"])
	f.mu.Unlock()
	if !strings.Contains(string(encoded), "source.txt") || strings.Contains(string(encoded), input) {
		t.Fatal("native input encoder was bypassed", string(encoded))
	}
	if err := os.WriteFile(input, []byte("changed bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SendWork(testContext(t), in); err == nil {
		t.Fatal("changed bytes reused operation ID")
	}
	thread, run := workerNativeTarget(t, s, task.ID)
	path := filepath.Join(task.Workspace, "result.txt")
	if err := os.WriteFile(path, []byte("worker result"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "resident-private.txt")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	text := "[result](result.txt) [outside](" + outside + ")"
	f.mu.Lock()
	native := f.workers[thread]
	native.Turns = []nativeTurn{{ID: run, Status: "completed", Items: []nativeItem{{ID: "result", Type: "agentMessage", Text: text}}}}
	f.workers[thread] = native
	f.mu.Unlock()
	messages, err := s.ReadWorkConversation(testContext(t), task.ID)
	if err != nil || len(messages) != 1 || len(messages[0].Artifacts) != 1 {
		t.Fatal(messages, err)
	}
	file, err := s.ReadWorkFile(testContext(t), task.ID, messages[0].Artifacts[0].ID)
	if err != nil || string(file.Data) != "worker result" {
		t.Fatal(file, err)
	}
	if _, err := s.ReadWorkFile(testContext(t), task.ID, opaque("artifact", outside)); err == nil {
		t.Fatal("resident artifact map bypassed task scope")
	}
}

func TestWorkerServiceRestartKeepsOriginalOwnerButDoesNotInventPendingChannel(t *testing.T) {
	root := t.TempDir()
	opts := SessionOptions{Directory: filepath.Join(root, "Work"), StateFile: filepath.Join(root, "binding.json")}
	const endpoint = "unix:///tmp/fixture-work-service-owner.sock"
	const task = "task-12345678901234567890123456789012"
	saved := binding{Version: 1, OwnerEndpoint: endpoint, Tasks: map[string]*taskRecord{task: {Owner: "codex:original-native-owner", Build: "fixture-native-build", Thread: "original-thread", Run: "original-run", View: api.Task{ID: task, Status: "working"}, Requests: map[string]taskReceipt{}}}}
	if err := os.WriteFile(opts.StateFile, raw(saved), 0600); err != nil {
		t.Fatal(err)
	}
	f := &sessionFixture{mode: "hold", answers: make(chan wireMessage, 8), workers: map[string]nativeThread{"original-thread": {ID: "original-thread", Turns: []nativeTurn{{ID: "original-run", Status: "inProgress"}}}}}
	worker := f.workers["original-thread"]
	worker.Status.Type = "active"
	f.workers["original-thread"] = worker
	var connections atomic.Int32
	start := func(_ context.Context, o Options) (*Client, error) {
		connections.Add(1)
		if !o.RequiredSocket || o.Socket != strings.TrimPrefix(endpoint, "unix://") {
			t.Errorf("owner switched during restart: %+v", o)
		}
		a, b := net.Pipe()
		f.mu.Lock()
		f.peer = b
		f.mu.Unlock()
		go f.serve(b)
		return &Client{build: "fixture-native-build", rpc: newTransportOptions(&updateEndpointConn{a, endpoint}, nil, true)}, nil
	}
	w, err := NewWorkOwner(opts)
	if err != nil {
		t.Fatal(err)
	}
	w.engine.start = start
	if err := w.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	request := wireMessage{ID: raw("native-pending-original"), Method: "item/commandExecution/requestApproval", Params: raw(map[string]any{"threadId": "original-thread", "turnId": "original-run", "itemId": "command", "command": "fixture", "availableDecisions": []string{"accept", "decline"}})}
	f.emit(request)
	awaitState(t, w.engine, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	before, err := w.ObserveWork(testContext(t), task)
	if err != nil || len(before.Approvals) != 1 || before.Approvals[0].Status != "pending" {
		t.Fatal(before, err)
	}
	old := before.Approvals[0]
	if old.NativeID != `"native-pending-original"` {
		t.Fatal("raw native request ID lost", old.NativeID)
	}
	if err := w.DetachForUpdate(testContext(t)); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewWorkOwner(opts)
	if err != nil {
		t.Fatal(err)
	}
	restarted.engine.start = start
	if err := restarted.Connect(testContext(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.DetachForUpdate(testContext(t)) })
	after, err := restarted.ObserveWork(testContext(t), task)
	if err != nil || after.Owner.Owner != before.Owner.Owner || after.Owner.Build != before.Owner.Build || after.Owner.PendingAcrossRestart || len(after.Approvals) != 1 || after.Approvals[0].Status != "unknown" {
		t.Fatal("lost native channel was treated as restored pending authority", after, err)
	}
	if err := restarted.DecideWork(testContext(t), api.WorkDecision{TaskID: task, RequestID: "stale-after-restart", Decision: api.Decision{ID: old.ID, Choice: old.Choices[0].ID}}); err == nil {
		t.Fatal("persisted pending audit authorized a response")
	}
	select {
	case answer := <-f.answers:
		t.Fatal("restart approved vanished request", answer)
	default:
	}
	// A real reissued request from the same live native owner is addressable;
	// its new transport handle is distinct from persisted audit evidence.
	f.emit(request)
	awaitState(t, restarted.engine, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	fresh, err := restarted.ObserveWork(testContext(t), task)
	if err != nil {
		t.Fatal(err)
	}
	pending := api.Approval{}
	for _, a := range fresh.Approvals {
		if a.Status == "pending" {
			pending = a
		}
	}
	if pending.ID == "" || pending.ID == old.ID || pending.NativeID != old.NativeID {
		t.Fatal("fresh original-owner request not restored distinctly", fresh.Approvals)
	}
	if err := restarted.DecideWork(testContext(t), api.WorkDecision{TaskID: task, RequestID: "fresh-after-restart", Decision: api.Decision{ID: pending.ID, Choice: pending.Choices[0].ID}}); err != nil {
		t.Fatal(err)
	}
	answer := <-f.answers
	if string(answer.ID) != `"native-pending-original"` || connections.Load() != 2 || f.starts != 0 {
		t.Fatal("recovery changed owner/request or started replacement work", answer, connections.Load(), f.starts)
	}
}

func TestWorkerServiceNativeOutputFilesRemainTaskScopedAcrossBindingReload(t *testing.T) {
	s, f, _, m := taskPair(t)
	sendSynthetic(t, s, "native-file-parent")
	task := newTask(t, m, "native-file-worker")
	other := newTask(t, m, "native-file-other-worker")
	filePath := filepath.Join(task.Workspace, "generated.png")
	if err := os.WriteFile(filePath, []byte("fixture generated output"), 0600); err != nil {
		t.Fatal(err)
	}
	thread, run := workerNativeTarget(t, s, task.ID)
	f.emit(wireMessage{Method: "item/completed", Params: raw(map[string]any{"threadId": thread, "turnId": run, "item": nativeItem{ID: "native-output", Type: "imageGeneration", Path: filePath, Status: "completed"}})})
	awaitState(t, s, func(api.Snapshot) bool {
		v, e := s.ObserveWork(testContext(t), task.ID)
		return e == nil && len(v.Items) > 0 && len(v.Items[len(v.Items)-1].Artifacts) > 0
	})
	observation, err := s.ObserveWork(testContext(t), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	artifact := observation.Items[len(observation.Items)-1].Artifacts[0]
	data, err := os.ReadFile(s.opts.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	var restored binding
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.binding = restored
	s.mu.Unlock()
	got, err := s.ReadWorkFile(testContext(t), task.ID, artifact.ID)
	if err != nil || string(got.Data) != "fixture generated output" {
		t.Fatal(got, err)
	}
	if _, err := s.ReadWorkFile(testContext(t), other.ID, artifact.ID); err == nil {
		t.Fatal("same owner file escaped task scope")
	}
}
