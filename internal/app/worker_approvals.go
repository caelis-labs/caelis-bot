package app

import (
	"context"
	"fmt"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// WorkerApprovals gives the resident Bot the exact offered choices for
// coordinatable Worker requests. Requests with opaque or marked-secret data
// stay on the user's native card instead.
func (a *Application) WorkerApprovals() []api.Approval {
	out := []api.Approval{}
	for _, native := range a.allApprovalSnapshot().Approvals {
		if !native.GuardianEligible() || native.ID == "" {
			continue
		}
		v := native
		v.Details, v.URL = "", ""
		v.Sections = append([]api.ApprovalSection(nil), native.Sections...)
		v.Choices = append([]api.Choice(nil), native.Choices...)
		for i := range v.Choices {
			v.Choices[i].Details = ""
		}
		v.Questions = append([]api.Question(nil), native.Questions...)
		for i := range v.Questions {
			v.Questions[i].Options = append([]api.Choice(nil), native.Questions[i].Options...)
			for j := range v.Questions[i].Options {
				v.Questions[i].Options[j].Details = ""
			}
		}
		out = append(out, v)
	}
	return out
}

func (a *Application) DecideWorkerApproval(ctx context.Context, d api.Decision) (api.Approval, error) {
	var current *api.Approval
	for _, native := range a.allApprovalSnapshot().Approvals {
		if native.ID == d.ID && native.GuardianEligible() {
			copy := native
			current = &copy
			break
		}
	}
	if current == nil {
		return api.Approval{}, fmt.Errorf("original Worker approval is unavailable")
	}
	if current.Status != "pending" {
		for _, v := range a.WorkerApprovals() {
			if v.ID == d.ID {
				return v, nil
			}
		}
		return api.Approval{}, fmt.Errorf("original Worker approval is unavailable")
	}
	if current.URL != "" {
		return api.Approval{}, fmt.Errorf("this approval requires direct user interaction")
	}
	for _, q := range current.Questions {
		if q.Secret {
			return api.Approval{}, fmt.Errorf("this approval contains a marked-secret field and requires direct user interaction")
		}
	}
	offered := false
	for _, c := range current.Choices {
		if c.ID == d.Choice {
			offered = true
			break
		}
	}
	if !offered {
		return api.Approval{}, fmt.Errorf("choice is not offered by the original request")
	}
	err := a.Backend.Decide(ctx, d)
	for _, v := range a.WorkerApprovals() {
		if v.ID == d.ID {
			return v, err
		}
	}
	if err == nil {
		// The native request may disappear between the committed write and the
		// next snapshot. Only the write receipt is known at this point.
		return api.Approval{ID: d.ID, Owner: "task", Status: "sent"}, nil
	}
	return api.Approval{ID: d.ID, Owner: "task", Status: "unknown"}, err
}

func (a *Application) userVisibleApprovals(snapshot api.Snapshot) api.Snapshot {
	visible := make([]api.Approval, 0, len(snapshot.Approvals))
	for _, approval := range snapshot.Approvals {
		if !approval.GuardianEligible() || !a.workerInteractionNoticeAccepted("approval", approval.ID) {
			visible = append(visible, approval)
		}
	}
	snapshot.Approvals = visible
	return snapshot
}

func (a *Application) workerInteractionNoticeAccepted(kind, nativeID string) bool {
	a.mu.Lock()
	resident := a.companion
	a.mu.Unlock()
	return resident != nil && resident.SystemReceipt(api.WorkerInteractionMessageID(kind, nativeID)).Outcome == "accepted"
}

// A host report wakes the resident Bot once per original pending Worker
// request. A dispatching/unknown receipt is never replayed after a crash.
func (a *Application) observeWorkerApprovalNotice(ctx context.Context, snapshot api.Snapshot) {
	if !snapshot.CanSend {
		return
	}
	for _, approval := range snapshot.Approvals {
		if !approval.GuardianEligible() || approval.Status != "pending" {
			continue
		}
		text := fmt.Sprintf("Worker approval %s is pending for task %s. Read the original request with bot_interactions.", approval.ID, approval.TaskTitle)
		a.deliverWorkerInteraction(ctx, "approval", approval.ID, text)
	}
}

func (a *Application) deliverWorkerInteraction(ctx context.Context, kind, nativeID, text string) {
	a.mu.Lock()
	resident := a.companion
	a.mu.Unlock()
	if resident == nil {
		return
	}
	source := api.SystemWorkerApproval
	if kind == "question" {
		source = api.SystemWorkerQuestion
	}
	_, err := resident.SubmitSystem(ctx, api.SystemMessage{ID: api.WorkerInteractionMessageID(kind, nativeID), Source: source, Text: text})
	if err != nil && a.host.ReportError != nil {
		a.host.ReportError(err)
	}
}
