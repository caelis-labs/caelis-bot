package app

import (
	"context"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type workerApprovalEngine struct {
	*testEngine
	snapshot      api.Snapshot
	decisions     []api.Decision
	reports       []api.Submission
	reportOutcome string
}

type serviceApprovalEngine struct {
	*workerApprovalEngine
	workDecisions []api.WorkDecision
}

func (e *serviceApprovalEngine) DecideWork(_ context.Context, in api.WorkDecision) error {
	e.workDecisions = append(e.workDecisions, in)
	return nil
}
func (e *serviceApprovalEngine) WorkApprovalURL(context.Context, string, string) (string, error) {
	return "", nil
}
func TestUserWorkerDecisionUsesSharedContractForActiveAndRetainedOwner(t *testing.T) {
	for _, retained := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "retained"}[retained], func(t *testing.T) {
			target := &serviceApprovalEngine{workerApprovalEngine: &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{Approvals: []api.Approval{{ID: "native-form", NativeID: "raw-native", TaskID: "owned-task", Owner: "task", Status: "pending", Questions: []api.Question{{ID: "secret", Secret: true}}}}}}}
			var a *Application
			if retained {
				current := &workerApprovalEngine{testEngine: newTestEngine()}
				a, _ = fixtureApp(t, current, Host{})
				a.localWork.owners["codex"] = target
			} else {
				a, _ = fixtureApp(t, target, Host{})
			}
			if len(a.WorkerApprovals()) != 0 {
				t.Fatal("Agent received sensitive native form")
			}
			d := api.Decision{ID: "native-form", Answers: map[string][]string{"secret": {"fixture-only"}}}
			if err := a.Backend.Decide(t.Context(), d); err != nil {
				t.Fatal(err)
			}
			if len(target.decisions) != 0 || len(target.workDecisions) != 1 {
				t.Fatal("shared native contract was bypassed")
			}
			in := target.workDecisions[0]
			if in.TaskID != "owned-task" || in.RequestID != api.WorkDecisionRequestID("owned-task", d) || in.Decision.Answers["secret"][0] != "fixture-only" {
				t.Fatal("full original user intent lost")
			}
		})
	}
}

func (e *workerApprovalEngine) WorkTerminal(context.Context, string) (api.TerminalTarget, error) {
	return api.TerminalTarget{}, nil
}

func TestRetainedWorkerApprovalRemainsVisibleAndUsesOriginalOwner(t *testing.T) {
	current := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true}}
	a, _ := fixtureApp(t, current, Host{})
	worker := api.Approval{ID: "old-worker-request", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "allowOnce", Scope: "once"}}}
	old := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{Approvals: []api.Approval{worker, {ID: "old-bot-request", Owner: "conversation", Status: "pending"}}}}
	a.localWork.mu.Lock()
	a.localWork.owners["codex"] = old
	a.localWork.mu.Unlock()
	if got := a.WorkerApprovals(); len(got) != 1 || got[0].ID != worker.ID {
		t.Fatal(got)
	}
	if got := a.Backend.Snapshot().Approvals; len(got) != 1 || got[0].ID != worker.ID {
		t.Fatal("old Bot approval leaked or Worker disappeared", got)
	}
	if _, err := a.DecideWorkerApproval(t.Context(), api.Decision{ID: worker.ID, Choice: "allowOnce"}); err != nil {
		t.Fatal(err)
	}
	if len(current.decisions) != 0 || len(old.decisions) != 1 || old.decisions[0].ID != worker.ID {
		t.Fatal("decision moved to new Runtime")
	}
}

func TestConversationGuardDoesNotTreatWorkerApprovalAsConversationWork(t *testing.T) {
	e := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{Approvals: []api.Approval{{ID: "worker", Owner: "task", Status: "pending"}}}}
	a, _ := fixtureApp(t, e, Host{})
	startNoticeResident(t, a)
	if err := a.guardConversationChange(); err != nil {
		t.Fatal(err)
	}
	e.snapshot.Approvals = append(e.snapshot.Approvals, api.Approval{ID: "bot", Owner: "conversation", Status: "pending"})
	if err := a.guardConversationChange(); err == nil {
		t.Fatal("pending original conversation authority ignored")
	}
}

func (e *workerApprovalEngine) Snapshot() api.Snapshot { return e.snapshot }
func (e *workerApprovalEngine) Decide(_ context.Context, decision api.Decision) error {
	e.decisions = append(e.decisions, decision)
	for i := range e.snapshot.Approvals {
		if e.snapshot.Approvals[i].ID == decision.ID {
			e.snapshot.Approvals[i].Status = "sent"
		}
	}
	return nil
}
func (e *workerApprovalEngine) SubmitSystem(_ context.Context, input api.SystemMessage) (api.Receipt, error) {
	e.reports = append(e.reports, input.NativeInput())
	if e.reportOutcome != "" {
		return api.Receipt{ID: input.ID, Outcome: e.reportOutcome}, nil
	}
	return api.Receipt{ID: input.ID, Outcome: "accepted"}, nil
}

func TestWorkerApprovalGuardianKeepsOriginalChoiceAndBotSelfBoundary(t *testing.T) {
	worker := api.Approval{ID: "worker-original", Owner: "task", Status: "pending", Title: "cua_repl", Target: "Obsidian", Description: "Read only", Sections: []api.ApprovalSection{{Text: "App: Obsidian"}}, Choices: []api.Choice{{ID: "allowOnce", Label: "Allow", Scope: "once"}}, Questions: []api.Question{{ID: "reason", Title: "Reason", Type: "string", Required: true}}}
	self := api.Approval{ID: "bot-own", Owner: "conversation", Status: "pending", Choices: []api.Choice{{ID: "allowOnce"}}}
	e := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true, Approvals: []api.Approval{worker, self}}}
	a, _ := fixtureApp(t, e, Host{})
	startNoticeResident(t, a)
	if got := a.Backend.Snapshot().Approvals; len(got) != 2 {
		t.Fatalf("unreported Worker approval was hidden: %+v", got)
	}
	a.observeWorkerApprovalNotice(t.Context(), e.snapshot)
	visible := a.Backend.Snapshot().Approvals
	if len(visible) != 1 || visible[0].ID != "bot-own" {
		t.Fatalf("Worker card leaked into user-facing snapshot: %+v", visible)
	}
	listed := a.WorkerApprovals()
	if len(listed) != 1 || listed[0].ID != worker.ID || listed[0].Choices[0].ID != "allowOnce" || listed[0].Choices[0].Scope != "once" || listed[0].Sections[0].Text != "App: Obsidian" || len(listed[0].Questions) != 1 {
		t.Fatalf("guardian catalog lost authority or leaked payload: %+v", listed)
	}
	listed[0].Sections[0].Text = "changed"
	if e.snapshot.Approvals[0].Sections[0].Text != "App: Obsidian" {
		t.Fatal("guardian catalog mutated native approval")
	}
	if _, err := a.DecideWorkerApproval(t.Context(), api.Decision{ID: "bot-own", Choice: "allowOnce"}); err == nil {
		t.Fatal("Bot approved its own request")
	}
	if _, err := a.DecideWorkerApproval(t.Context(), api.Decision{ID: worker.ID, Choice: "made-up"}); err == nil {
		t.Fatal("Bot invented an option")
	}
	decision := api.Decision{ID: worker.ID, Choice: "allowOnce", Answers: map[string][]string{"reason": {"User requested a read-only view"}}}
	result, err := a.DecideWorkerApproval(t.Context(), decision)
	if err != nil || result.Status != "sent" || len(e.decisions) != 1 || e.decisions[0].ID != worker.ID || e.decisions[0].Answers["reason"][0] != decision.Answers["reason"][0] {
		t.Fatalf("original Worker decision missing: %+v %+v %v", result, e.decisions, err)
	}
	if _, err := a.DecideWorkerApproval(t.Context(), decision); err != nil || len(e.decisions) != 1 {
		t.Fatal("sent Worker decision repeated", err, e.decisions)
	}
}

func TestWorkerApprovalSecretAndURLNeedDirectUserPath(t *testing.T) {
	e := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{Approvals: []api.Approval{
		{ID: "secret", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "answer"}}, Questions: []api.Question{{ID: "token", Secret: true}}},
		{ID: "url", Owner: "task", Status: "pending", URL: "https://example.test/login", Choices: []api.Choice{{ID: "accept"}}},
		{ID: "opaque-diff", Owner: "task", Status: "pending", Details: "private diff", Choices: []api.Choice{{ID: "accept"}}},
		{ID: "rule", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "rule", Details: "private policy"}}},
	}}}
	a, _ := fixtureApp(t, e, Host{})
	startNoticeResident(t, a)
	if len(a.WorkerApprovals()) != 0 || len(a.Backend.Snapshot().Approvals) != 4 {
		t.Fatal("opaque Worker approval was hidden from direct user path")
	}
	for _, id := range []string{"secret", "url", "opaque-diff", "rule"} {
		if _, err := a.DecideWorkerApproval(t.Context(), api.Decision{ID: id, Choice: "accept"}); err == nil {
			t.Fatal("restricted Worker approval reached native Runtime", id)
		}
	}
	if len(e.decisions) != 0 {
		t.Fatal("restricted Worker approval submitted")
	}
}

func TestWorkerApprovalNoticesUseOneOriginalReportEach(t *testing.T) {
	e := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{CanSend: true, Approvals: []api.Approval{
		{ID: "worker-one", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "allow"}}},
		{ID: "worker-two", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "deny"}}},
		{ID: "bot-own", Owner: "conversation", Status: "pending", Choices: []api.Choice{{ID: "allow"}}},
	}}}
	a, _ := fixtureApp(t, e, Host{})
	startNoticeResident(t, a)
	a.observeWorkerApprovalNotice(t.Context(), e.snapshot)
	a.observeWorkerApprovalNotice(t.Context(), e.snapshot)
	if len(e.reports) != 2 || e.reports[0].ID == e.reports[1].ID {
		t.Fatalf("Worker notices were lost, duplicated, or included Bot self approval: %+v", e.reports)
	}
}

func TestWorkerApprovalUnacceptedNoticeRetainsOriginalUserCard(t *testing.T) {
	for _, outcome := range []string{"rejected", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			e := &workerApprovalEngine{testEngine: newTestEngine(), reportOutcome: outcome, snapshot: api.Snapshot{CanSend: true, Approvals: []api.Approval{{ID: "worker-original", Owner: "task", Status: "pending", Choices: []api.Choice{{ID: "allow"}}}}}}
			a, _ := fixtureApp(t, e, Host{})
			startNoticeResident(t, a)
			a.observeWorkerApprovalNotice(t.Context(), e.snapshot)
			if got := a.Backend.Snapshot().Approvals; len(got) != 1 || got[0].ID != "worker-original" {
				t.Fatalf("unaccepted Guardian notice hid original user card: %+v", got)
			}
			if outcome == "unknown" {
				a.observeWorkerApprovalNotice(t.Context(), e.snapshot)
				if len(e.reports) != 1 {
					t.Fatalf("unknown report was replayed: %+v", e.reports)
				}
			}
		})
	}
}

func (e *workerApprovalEngine) Revision() uint64 { return e.snapshot.Revision }

func TestRetainedApprovalChangesReachChatWithoutResidentRevision(t *testing.T) {
	current := &workerApprovalEngine{testEngine: newTestEngine(), snapshot: api.Snapshot{Revision: 10}}
	a, _ := fixtureApp(t, current, Host{})
	old := &workerApprovalEngine{testEngine: newTestEngine()}
	a.localWork.owners["codex"] = old
	first := a.Backend.ChatSnapshot(0, "")
	old.snapshot.Approvals = []api.Approval{{ID: "retained", Owner: "task", Status: "pending"}}
	next := a.Backend.ChatSnapshot(first.Snapshot.Revision, first.Snapshot.BotStatus)
	if !next.Changed || len(next.Snapshot.Approvals) != 1 {
		t.Fatal("retained approval did not wake chat", next)
	}
	if a.Backend.ChatSnapshot(next.Snapshot.Revision, next.Snapshot.BotStatus).Changed {
		t.Fatal("unchanged retained approvals keep waking chat")
	}
	old.snapshot.Approvals = nil
	if !a.Backend.ChatSnapshot(next.Snapshot.Revision, next.Snapshot.BotStatus).Changed {
		t.Fatal("resolved retained approval remained visible")
	}
}

func startNoticeResident(t *testing.T, a *Application) {
	t.Helper()
	if err := a.PreparePersonal(); err != nil {
		t.Fatal(err)
	}
	a.companion.ConfigureInitialization(nil)
	a.companion.Start(a.currentEngine())
}
