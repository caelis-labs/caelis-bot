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
func (e *workerApprovalEngine) SubmitReport(_ context.Context, input api.Submission) (api.Receipt, error) {
	e.reports = append(e.reports, input)
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
