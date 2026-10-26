//go:build webfixture

package main

// This fixture replaces only the Runtime Engine contract. UI, Web operations,
// backend.Service, textchannel control and SQLite transcript are production code.
// It has no external model, account, commands or filesystem effects.
import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

type interactionReceipt struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Input   string `json:"input,omitempty"`
	Choice  string `json:"choice,omitempty"`
	Outcome string `json:"outcome"`
	Writes  int    `json:"writes"`
}
type interactionEngine struct {
	*engine
	receipts []interactionReceipt
}

func (e *interactionEngine) Snapshot() api.Snapshot {
	value := e.engine.Snapshot()
	value.RuntimeOwner = "controlled-runtime-original-owner"
	return value
}
func (e *interactionEngine) journal() {
	data, _ := json.MarshalIndent(e.receipts, "", "  ")
	if err := os.WriteFile(filepath.Join(e.root, "original-runtime-receipts.json"), data, 0600); err != nil {
		panic(err)
	}
	e.save()
}
func (e *interactionEngine) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.receipts {
		if r.ID == in.ID {
			return api.Receipt{ID: r.ID, Outcome: r.Outcome}, nil
		}
	}
	outcome := "accepted"
	if strings.Contains(in.ModelInputOverride, "明确拒绝") {
		outcome = "rejected"
	}
	if strings.Contains(in.ModelInputOverride, "待核对") {
		outcome = "unknown"
	}
	if strings.Contains(in.ModelInputOverride, "待核对后拒绝") {
		for _, old := range e.receipts {
			if old.Kind == "answer" && old.Input == in.Text && old.Outcome == "rejected" {
				outcome = "rejected"
			}
		}
	}
	e.receipts = append(e.receipts, interactionReceipt{ID: in.ID, Kind: "answer", Input: in.Text, Outcome: outcome, Writes: 1})
	e.state.LastReceipt = api.Receipt{ID: in.ID, Outcome: outcome}
	// Deliberately retain the raw native echo; the real controller/chatlog must
	// keep it out of the human display, including on re-open.
	e.state.Items = append(e.state.Items, api.Item{ID: "native:" + in.ID, RequestID: in.ID, Kind: "user", Text: in.Text, Status: "completed"})
	e.journal()
	return e.state.LastReceipt, nil
}
func (e *interactionEngine) Decide(_ context.Context, d api.Decision) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for n := range e.state.Approvals {
		a := &e.state.Approvals[n]
		if a.ID != d.ID || a.Status != "pending" {
			continue
		}
		if d.Choice != "accept" && d.Choice != "decline" {
			return api.DecisionValidationError{Message: "invalid choice"}
		}
		if d.Choice == "accept" && (len(d.Answers["value"]) != 1 || strings.TrimSpace(d.Answers["value"][0]) == "") {
			e.receipts = append(e.receipts, interactionReceipt{ID: d.ID, Kind: "validation", Outcome: "not_dispatched", Writes: 0})
			e.journal()
			return api.DecisionValidationError{Message: "required value"}
		}
		outcome := "allowed"
		if d.Choice == "decline" {
			outcome = "declined"
		}
		if d.ID == "fixture-approval-unknown" {
			outcome = "unknown"
			a.Status = "unknown"
		} else {
			a.Status = "resolved"
			a.Resolution = &api.ApprovalResolution{ChoiceID: d.Choice, Outcome: outcome, Scope: "once"}
		}
		e.receipts = append(e.receipts, interactionReceipt{ID: d.ID, Kind: "approval", Choice: d.Choice, Outcome: outcome, Writes: 1})
		e.journal()
		if outcome == "unknown" {
			return errors.New("controlled Runtime response unavailable")
		}
		return nil
	}
	return api.DecisionValidationError{Message: "original request unavailable"}
}
func newInteractionEngine(e *engine) *interactionEngine {
	r := &interactionEngine{engine: e}
	e.state.RuntimeOwner = "controlled-runtime-original-owner"
	data, err := os.ReadFile(filepath.Join(e.root, "original-runtime-receipts.json"))
	if err == nil {
		if json.Unmarshal(data, &r.receipts) != nil {
			panic("invalid original receipts")
		}
	}
	if len(e.state.Approvals) == 0 {
		for _, id := range []string{"fixture-approval-accepted", "fixture-approval-unknown"} {
			title := "可控 Runtime 审批"
			if strings.HasSuffix(id, "unknown") {
				title = "可控 Runtime 审批待核对"
			}
			e.state.Approvals = append(e.state.Approvals, api.Approval{ID: id, Owner: "bot", Title: title, Status: "pending", Choices: []api.Choice{{ID: "accept", Label: "允许一次", Scope: "once"}, {ID: "decline", Label: "拒绝", Scope: "once"}}, Questions: []api.Question{{ID: "value", Title: "验收值", Type: "text", Required: true, Options: []api.Choice{}}}})
		}
	}
	r.journal()
	return r
}
func configureInteractions(e *interactionEngine, b *backend.Service) func() {
	control, err := textchannel.Open(e.root, b.Decide)
	if err != nil {
		panic(err)
	}
	control.SetQuestionReplyOutcomeObserver(b.ResolveQuestionReply)
	observe := func(item api.Item) { b.ObserveChat(api.Snapshot{Items: []api.Item{item}}) }
	control.SetQuestionReplyObserver(observe)
	control.SetAsyncUpdateObserver(observe)
	control.SetAsyncAnswerer(func(ctx context.Context, in textchannel.Inbound, model string) (api.Receipt, error) {
		return backend.SubmitRemote(ctx, b, api.Submission{ID: in.ID, Text: in.Text, ModelInputOverride: model}, nil)
	})
	b.SetQuestionLifecycle(control.ProjectQuestions, control.Revision, control.DismissQuestion)
	b.SetQuestionAnswerer(func(ctx context.Context, id, binding, value, request string) string {
		return control.HandleDesktopQuestion(ctx, request, id, binding, value, e.Snapshot())
	})
	projectNativeQuestions := func() {
		for _, label := range []string{"成功回答", "明确拒绝", "持续待核对", "待核对后拒绝"} {
			item, err := control.AsyncCard(api.Item{ID: "fixture-question-" + label, TurnKey: "fixture-turn-" + label, Kind: "assistant", AsyncCallID: "fixture-call-" + label, AsyncQuestions: []api.AsyncQuestion{{Title: "可控 Runtime：" + label, Options: []string{"保留本机", "使用远端"}}}}, e.state.RuntimeOwner)
			if err != nil {
				panic(err)
			}
			observe(item)
		}
	}
	projectNativeQuestions()
	return func() {
		// A test driver may deliver a late original receipt to the controlled
		// Runtime. This never patches the application's control or transcript DB.
		path := filepath.Join(e.root, "fixture-receipt-command.json")
		if data, err := os.ReadFile(path); err == nil {
			var cmd struct{ ID, Outcome string }
			if json.Unmarshal(data, &cmd) == nil && (cmd.Outcome == "accepted" || cmd.Outcome == "rejected") {
				e.mu.Lock()
				for i := range e.receipts {
					r := &e.receipts[i]
					if r.ID == cmd.ID && r.Kind == "answer" && r.Outcome == "unknown" {
						r.Outcome = cmd.Outcome
						e.state.LastReceipt = api.Receipt{ID: r.ID, Outcome: r.Outcome}
						e.journal()
					}
				}
				e.mu.Unlock()
			}
			_ = os.Remove(path)
		}
		snap := e.Snapshot()
		_ = control.ReconcileAsync(snap, nil)
		projectNativeQuestions()
		b.ObserveChat(snap)
	}
}
