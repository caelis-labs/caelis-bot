package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

// WorkerApprovals gives the resident Bot the exact offered choices for
// coordinatable Worker requests. Requests with opaque or marked-secret data
// stay on the user's native card instead.
func (a *Application) WorkerApprovals() []api.Approval {
	out := []api.Approval{}
	for _, native := range a.engine.Snapshot().Approvals {
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
	for _, native := range a.engine.Snapshot().Approvals {
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

type workerApprovalNotice struct {
	Version int               `json:"version"`
	States  map[string]string `json:"states"`
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
	a.interactionNoticeMu.Lock()
	defer a.interactionNoticeMu.Unlock()
	b, err := os.ReadFile(filepath.Join(a.root, "worker-interaction-notices.json"))
	if err != nil {
		return false
	}
	var ledger workerApprovalNotice
	if json.Unmarshal(b, &ledger) != nil || ledger.Version != 1 {
		return false
	}
	return ledger.States[kind+":"+nativeID] == "accepted"
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
		text := fmt.Sprintf("Worker has pending native approval %s for task %s. Inspect the exact offered request with bot_interactions and decide from the user's task intent when appropriate. If context is insufficient, ask the user. Do not infer permission from this notice; the Runtime must confirm the original decision.", approval.ID, approval.TaskTitle)
		a.reportWorkerInteraction(ctx, "approval", approval.ID, text)
	}
}

func (a *Application) reportWorkerInteraction(ctx context.Context, kind, nativeID, text string) {
	if !a.engine.Snapshot().CanSend {
		return
	}
	reporter, ok := a.engine.(api.ReportSubmitter)
	if !ok {
		return
	}
	a.interactionNoticeMu.Lock()
	defer a.interactionNoticeMu.Unlock()
	path := filepath.Join(a.root, "worker-interaction-notices.json")
	ledger := workerApprovalNotice{Version: 1, States: map[string]string{}}
	if b, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(b, &ledger) != nil || ledger.Version != 1 || ledger.States == nil {
			if a.host.ReportError != nil {
				a.host.ReportError(fmt.Errorf("worker interaction notice ledger invalid"))
			}
			return
		}
	} else if !os.IsNotExist(err) {
		if a.host.ReportError != nil {
			a.host.ReportError(err)
		}
		return
	}
	key := kind + ":" + nativeID
	if ledger.States[key] != "" {
		return
	}
	ledger.States[key] = "dispatching"
	if err := localstate.Write(path, ledger); err != nil {
		if a.host.ReportError != nil {
			a.host.ReportError(err)
		}
		return
	}
	sum := sha256.Sum256([]byte(key))
	id := fmt.Sprintf("worker-interaction-%x", sum[:])
	receipt, err := reporter.SubmitReport(ctx, api.Submission{ID: id, Text: text})
	if err == nil && receipt.ID == id && receipt.Outcome == "accepted" {
		ledger.States[key] = "accepted"
		_ = localstate.Write(path, ledger)
	} else if err == nil && receipt.ID == id && receipt.Outcome == "rejected" {
		delete(ledger.States, key)
		_ = localstate.Write(path, ledger)
	}
}
