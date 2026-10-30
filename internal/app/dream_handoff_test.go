package app

import (
	"context"
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type handoffNoticeEngine struct {
	*workerApprovalEngine
	creating, create, adopted, finish chan struct{}
	onInput                           func()
}

func (e *handoffNoticeEngine) RenewAfterTool(ctx context.Context, _ api.ToolInvocation) (string, error) {
	close(e.creating)
	<-e.create
	err := api.CommitRenewal(ctx, "new-session", func() error { return nil })
	close(e.adopted)
	<-e.finish
	return "new-session", err
}
func (e *handoffNoticeEngine) SubmitSystem(ctx context.Context, in api.SystemMessage) (api.Receipt, error) {
	e.onInput()
	return e.workerApprovalEngine.SubmitSystem(ctx, in)
}
func TestWorkerInteractionProducersWaitForHandoffAndDoNotInvalidateIt(t *testing.T) {
	e := &handoffNoticeEngine{workerApprovalEngine: &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true, Approvals: []api.Approval{{ID: "original-approval", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "allowOnce"}}}}}}, creating: make(chan struct{}), create: make(chan struct{}), adopted: make(chan struct{}), finish: make(chan struct{})}
	a, root := fixtureApp(t, e, Host{})
	startNoticeResident(t, a)
	owner := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
	wireTestWorkers(t, a, workPool(root, "codex", map[string]*workerFixture{"codex": owner}))
	owner.states["work-one"] = api.WorkState{Task: api.Task{ID: "work-one"}, AsyncQuestions: []api.Item{{ID: "worker-question:original", TurnKey: "worker-turn", Kind: "assistant", AsyncCallID: "original-question", AsyncQuestions: []api.AsyncQuestion{{Title: "Choose", Options: []string{"A", "B"}}}}}}
	call := api.ToolInvocation{Provider: e.ProviderInfo().ID, CallID: "original-dream", Session: "old-session", Turn: "terminal"}
	out := a.companion.CallTool(api.WithToolInvocation(t.Context(), call), "bot_dream", json.RawMessage(`{"handoff":"Original tasks and read-only limits; Notebook/review.md lines 30 onward unread."}`))
	if out.IsError || !out.TurnComplete {
		t.Fatal(out)
	}
	renewed := make(chan error, 1)
	go func() { renewed <- a.companion.RenewPendingToolHandoff(t.Context()) }()
	<-e.creating
	observe := func() { a.observeWorkerApprovalNotice(t.Context(), e.snapshot); a.observeWorkerQuestions(t.Context()) }
	for range 3 {
		observe()
	}
	if len(e.reports) != 0 {
		t.Fatal("producer bypassed resident handoff admission", e.reports)
	}
	var seeds []api.ContextSeed
	e.onInput = func() {
		seed := a.companion.PrepareHandoffContext()
		seeds = append(seeds, seed)
		if err := a.companion.ConsumeHandoffContext(seed); err != nil {
			t.Error(err)
		}
	}
	close(e.create)
	<-e.adopted
	for range 3 {
		observe()
	}
	close(e.finish)
	if err := <-renewed; err != nil {
		t.Fatal(err)
	}
	if len(e.reports) != 2 || len(seeds) != 2 || seeds[0].HandoffDigest == "" || !strings.Contains(seeds[0].Text, "Notebook/review.md lines 30") || seeds[1].HandoffDigest != "" {
		t.Fatal("reports lost handoff, duplicated, or starved renewal", e.reports, seeds)
	}
	if a.companion.PrepareHandoffContext().HandoffDigest != "" || len(a.WorkerApprovals()) != 1 || len(a.WorkerQuestions()) != 1 {
		t.Fatal("original interactions or consumed seed were lost")
	}
}

func TestHumanWorkerAnswerSupersedesDreamButAgentAnswerDoesNot(t *testing.T) {
	for _, channel := range []string{"telegram", "bot"} {
		t.Run(channel, func(t *testing.T) {
			e := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true}}
			a, root := fixtureApp(t, e, Host{})
			startNoticeResident(t, a)
			owner := &workerFixture{runtime: "codex", states: map[string]api.WorkState{}}
			pool := workPool(root, "codex", map[string]*workerFixture{"codex": owner})
			wireTestWorkers(t, a, pool)
			manager, err := tasks.Open(filepath.Join(root, "question-tasks.json"), filepath.Join(root, "Tasks"), "codex", pool, owner, a.Backend.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			a.tasks = manager
			work, err := manager.StartTask(t.Context(), api.TaskStart{RequestID: "original-start", Title: "Original task", Prompt: "Original task", Workspace: root})
			if err != nil {
				t.Fatal(err)
			}
			state := owner.states[work.ID]
			state.AsyncQuestions = []api.Item{{ID: "original-question", TurnKey: "original-turn", Kind: "assistant", AsyncCallID: "original-call", AsyncQuestions: []api.AsyncQuestion{{Title: "Which file?"}}}}
			owner.states[work.ID] = state
			a.observeWorkerQuestions(t.Context())
			call := api.ToolInvocation{Provider: e.ProviderInfo().ID, CallID: "dream-before-answer", Session: "old", Turn: "turn"}
			out := a.companion.CallTool(api.WithToolInvocation(t.Context(), call), "bot_dream", json.RawMessage(`{"handoff":"Original Worker question remains pending; keep its original records."}`))
			if out.IsError || !out.TurnComplete {
				t.Fatal(out)
			}
			if channel == "bot" {
				q := a.WorkerQuestions()[0]
				receipt, err := a.AnswerWorkerQuestion(t.Context(), q.ID+"@"+q.Binding, "README.md", "original-answer")
				if err != nil || receipt.Outcome != "accepted" {
					t.Fatal(receipt, err)
				}
			} else {
				feedback := a.textControl.Handle(t.Context(), textchannel.Inbound{Channel: channel, Conversation: "paired", ID: "original-answer", Text: "/answer Q1 README.md"}, api.Snapshot{})
				if !strings.Contains(feedback, "已提交") {
					t.Fatal(feedback)
				}
			}
			if owner.sends != 1 {
				t.Fatal("original Worker answer replayed", owner.sends)
			}
			if err := out.CheckTurnComplete(); (err != nil) != (channel != "bot") {
				t.Fatal("wrong human/Agent priority", channel, err)
			}
		})
	}
}
