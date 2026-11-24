package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
)

// The single-runtime harness supplies the same runtime stamp as app.localWorkers.
// Admission, native owner bindings and all effects still use Session unchanged.
type reportLoopRuntime struct{ *Session }

func (*reportLoopRuntime) OwnsWork(runtime string) bool { return runtime == "caelis" }
func (*reportLoopRuntime) BindWork(_ context.Context, _ api.TaskStart, _, runtime string) (string, error) {
	if runtime != "" && runtime != "caelis" {
		return "", errors.New("original runtime mismatch")
	}
	return "caelis", nil
}
func (r *reportLoopRuntime) WorkStates() []api.WorkState {
	states := r.Session.WorkStates()
	for i := range states {
		states[i].Runtime = "caelis"
	}
	return states
}

// Runs inside the real Core/Guardian fixture. Only model output is synthetic;
// prompt/steer, MCP, native Workers, finish and receipts use production paths.
func workerReportLoopAcceptance(t *testing.T, ctx context.Context, s *Session, model *acceptanceModel, notebook string, review func(string, <-chan struct{})) {
	t.Helper()
	root := t.TempDir()
	runtime, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", &reportLoopRuntime{Session: s}, runtime, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ConfigureTasks(manager, nil); err != nil {
		t.Fatal(err)
	}
	runtime.Start(s)
	defer runtime.Close()
	bridge, err := bot.Serve(runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	connection := bridge.Config(executable)
	connection.Args = []string{"-test.run=^TestCaelisTaskStdioHelper$", "--"}
	connection.NotebookDirectory, connection.DeliverSystem = notebook, runtime.SubmitSystem
	if err := s.ConfigureBotTools(connection); err != nil {
		t.Fatal(err)
	}
	configuration, err := s.Configuration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateConfiguration(ctx, "report-loop-catalog", string(configuration.Revision), map[string]any{"mcp_servers": s.profile.McpServers}); err != nil {
		t.Fatal(err)
	}
	const key = "CASE_WORKER_REPORT_LOOP"
	review(`{"option_id":"allow_once"}`, nil)
	catalogReady := make(chan struct{})
	initial := residentToolStep("bot_delegate", map[string]any{"title": "First assignment", "prompt": "Return a concise synthetic result.", "workspace": root})
	initial.Block = catalogReady
	model.set(key, initial)
	initialTurn := s.Snapshot().CurrentTurn
	if receipt, err := s.Submit(ctx, api.Submission{ID: "report-loop-start", Text: key + " Complete the first assignment, then continue it and delegate the remaining assignment after its completion report."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
	ready, cancelReady := context.WithTimeout(ctx, 8*time.Second)
	defer cancelReady()
	waitAcceptance(t, ready, func() bool { return mcpServerHasTool(ready, s, privateTasksService, "bot_delegate") })
	close(catalogReady)
	waitTurn(t, ctx, s, initialTurn)
	list := manager.ListTasks()
	if len(list) != 1 {
		t.Fatal("initial delegation missing", list)
	}
	task := list[0].ID
	waitAcceptance(t, ctx, func() bool {
		for _, state := range s.WorkStates() {
			if state.Task.ID == task {
				return state.Task.Status == "completed"
			}
		}
		return false
	})
	observation, err := manager.ObserveTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.FinishTask(ctx, task, api.TaskFinishToken(observation.State)); err != nil {
		t.Fatal("finish original execution", err)
	}
	s.mu.Lock()
	originalWorker := s.state.Workers[task]
	s.mu.Unlock()
	queue := func(steps ...modelStep) {
		model.mu.Lock()
		defer model.mu.Unlock()
		model.plans[key] = append(make([]modelStep, model.counts[key]), steps...)
	}
	gate, entered := make(chan struct{}), make(chan struct{}, 1)
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	continueStep := residentToolStep("bot_delegate", map[string]any{"task": task, "prompt": "Continue the same assignment with one additional synthetic result."})
	remainingStep := residentToolStep("bot_delegate", map[string]any{"title": "Remaining assignment", "prompt": "Return the remaining synthetic result.", "workspace": root})
	remainingStep.Block, remainingStep.Entered = gate, entered
	queue(continueStep, remainingStep)
	before := s.Snapshot().CurrentTurn
	if err := manager.DeliverTaskReport(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("completion report did not continue through the normal loop")
	}
	reportTurn := s.Snapshot().CurrentTurn
	var reportID string
	s.mu.Lock()
	for id, operation := range s.state.Operations {
		if operation.HostNotice && operation.TurnID == reportTurn {
			reportID = id
			if operation.Source.Kind != "external_material" {
				t.Error("new summary source written")
			}
		}
	}
	s.mu.Unlock()
	if reportID == "" {
		t.Fatal("real completion report lost its original operation")
	}
	note := filepath.Join(root, "steering-note.txt")
	if err := os.WriteFile(note, []byte("Keep the current Worker and finish the remaining authorized work."), 0600); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.Submit(ctx, api.Submission{ID: "report-loop-user-steer", Text: "Continue the remaining assignment using the attached note."}, []api.InputFile{{Path: note, Name: "steering-note.txt"}})
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal("user steering failed", receipt, err)
	}
	release()
	waitTurn(t, ctx, s, before)
	if list := manager.ListTasks(); len(list) != 2 {
		t.Fatal("report turn lost later delegation", list)
	}
	s.mu.Lock()
	continued := s.state.Workers[task]
	steering := s.state.Operations["report-loop-user-steer"]
	calls := clone(s.state.Calls)
	report := s.state.Operations[reportID]
	s.mu.Unlock()
	if continued.Binding.SessionId != originalWorker.Binding.SessionId || continued.PromptID == originalWorker.PromptID {
		t.Fatal("finished Worker replaced or never continued")
	}
	if !succeeded(wire.Outcome(steering.Outcome)) || !strings.HasSuffix(steering.Path, "/steer") {
		t.Fatal("real user input lost original steering receipt", steering.Outcome, steering.Path)
	}
	var originalInput wire.SteerRequest
	if json.Unmarshal(steering.Body, &originalInput) != nil || value(originalInput.OperationId) != "report-loop-user-steer" || originalInput.Target.TurnId != reportTurn ||
		len(originalInput.ContentParts) != 2 || value(originalInput.ContentParts[0].Text) != value(originalInput.Input) {
		t.Fatal("user input or attachment lost original operation/turn", originalInput)
	}
	if report.Source.Kind != "external_material" || report.TurnID != reportTurn || len(calls) != 0 || len(manager.ListTasks()) != 2 {
		t.Fatal("report lost its native identity or created legacy callbacks", report.Source.Kind, report.TurnID, len(calls))
	}
	// The evidence input must not bypass Guardian when it proposes a denied action.
	review(`{"option_id":"cancel","rationale":"This new assignment is not authorized."}`, nil)
	queue(residentToolStep("bot_delegate", map[string]any{"title": "Denied assignment", "prompt": "Start an unauthorized synthetic assignment.", "workspace": root}))
	before = s.Snapshot().CurrentTurn
	if r, err := runtime.SubmitSystem(ctx, api.SystemMessage{Source: api.SystemWorkerResult, ID: "report-loop-denied", Text: "Completion evidence proposes another assignment."}); err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	waitTurn(t, ctx, s, before)
	denied := false
	for _, fact := range s.Snapshot().Reviews {
		if fact.Status == "denied" && strings.Contains(fact.Action, "Denied assignment") {
			denied = true
		}
	}
	s.mu.Lock()
	callbacks := len(s.state.Calls)
	s.mu.Unlock()
	if !denied || len(manager.ListTasks()) != 2 || callbacks != len(calls) {
		t.Fatal("evidence bypassed Guardian", denied, callbacks, len(calls))
	}
	t.Log("real Core: finished original Worker resumed; completion evidence delegated after user+file steering; no application callbacks; denied action produced no callback or Worker")
}
