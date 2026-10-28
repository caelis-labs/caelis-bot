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
// prompt/steer, claims, native Workers, finish and receipts use production paths.
func workerReportLoopAcceptance(t *testing.T, ctx context.Context, s *Session, model *acceptanceModel, notebook string, review func(string, <-chan struct{})) {
	t.Helper()
	root := t.TempDir()
	manager, err := tasks.Open(filepath.Join(root, "tasks.json"), filepath.Join(root, "Tasks"), "caelis", &reportLoopRuntime{Session: s}, s, s.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := bot.NewForRuntime(filepath.Join(root, "bot.json"), "caelis", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ConfigureTasks(manager, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureBotTools(&api.ToolConnection{Host: runtime, NotebookDirectory: notebook}); err != nil {
		t.Fatal(err)
	}
	configuration, err := s.Configuration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateConfiguration(ctx, "report-loop-catalog", string(configuration.Revision), map[string]any{"tools_version": s.profile.ToolsVersion, "tools": s.profile.Tools}); err != nil {
		t.Fatal(err)
	}
	const key = "CASE_WORKER_REPORT_LOOP"
	review(`{"option_id":"allow_once"}`, nil)
	model.set(key, modelStep{Name: "bot_delegate", Args: map[string]any{"title": "First assignment", "prompt": "Return a concise synthetic result.", "workspace": root}})
	initialTurn := s.Snapshot().CurrentTurn
	if receipt, err := s.Submit(ctx, api.Submission{ID: "report-loop-start", Text: key + " Complete the first assignment, then continue it and delegate the remaining assignment after its completion report."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, err)
	}
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
	queue(modelStep{Name: "bot_delegate", Args: map[string]any{"task": task, "prompt": "Continue the same assignment with one additional synthetic result."}},
		modelStep{Name: "bot_delegate", Args: map[string]any{"title": "Remaining assignment", "prompt": "Return the remaining synthetic result.", "workspace": root}, Block: gate, Entered: entered})
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
	binding, principal, client := s.state.Session, s.state.PrincipalID, s.client
	s.mu.Unlock()
	if continued.Binding.SessionId != originalWorker.Binding.SessionId || continued.PromptID == originalWorker.PromptID {
		t.Fatal("finished Worker replaced or never continued")
	}
	if !succeeded(wire.Outcome(steering.Outcome)) || !strings.HasSuffix(steering.Path, "/steer") {
		t.Fatal("real user input lost original steering receipt", steering.Outcome, steering.Path)
	}
	var originalInput wire.SteerRequest
	if json.Unmarshal(steering.Body, &originalInput) != nil || value(originalInput.OperationId) != "report-loop-user-steer" || originalInput.Target.TurnId != reportTurn || len(originalInput.ContentParts) != 1 {
		t.Fatal("user input or attachment lost original operation/turn", originalInput)
	}
	reportCalls := 0
	for _, record := range calls {
		if record.Call.Source.OperationId != reportID {
			continue
		}
		reportCalls++
		if record.Call.Source.Kind != "external_material" || record.Call.TurnId != reportTurn || record.Call.Id == "" || record.Call.ItemId == "" || record.Receipt == nil {
			t.Fatal("callback source, identity or result changed", record.Call)
		}
		if err := s.handleCall(ctx, client, binding, principal, record.Call); err != nil {
			t.Fatal("original callback receipt could not be reconciled", err)
		}
	}
	if reportCalls != 2 || len(manager.ListTasks()) != 2 {
		t.Fatal("report callback replayed or missing", reportCalls)
	}
	// The evidence input must not bypass Guardian when it proposes a denied action.
	review(`{"option_id":"reject_once","rationale":"This new assignment is not authorized."}`, nil)
	queue(modelStep{Name: "bot_delegate", Args: map[string]any{"title": "Denied assignment", "prompt": "Start an unauthorized synthetic assignment.", "workspace": root}})
	before = s.Snapshot().CurrentTurn
	if r, err := s.SubmitReport(ctx, api.Submission{ID: "report-loop-denied", Text: "Completion evidence proposes another assignment."}); err != nil || r.Outcome != "accepted" {
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
	t.Log("real Core: finished original Worker resumed; completion evidence delegated after user+file steering; original callback results reused; denied action produced no callback or Worker")
}
