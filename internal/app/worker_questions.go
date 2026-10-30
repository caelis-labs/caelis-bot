package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/tasks"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

// The Worker owner persists native async question identities. The resident Bot
// receives the question through its private host report and asks the user in
// ordinary chat only when it needs more information.
func (a *Application) observeWorkerQuestions(ctx context.Context) {
	a.observeWorkerApprovalNotice(ctx, a.allApprovalSnapshot())
	a.reconcileAnswerReceipt(ctx)
	states := a.workerStates()
	for _, state := range states {
		for _, item := range state.AsyncQuestions {
			_, err := a.textControl.AsyncWorkCard(item, state.Runtime, state.Task.ID)
			if err != nil {
				if a.host.ReportError != nil {
					a.host.ReportError(err)
				}
				continue
			}
		}
	}
	if err := a.textControl.ReconcileAsync(a.currentEngine().Snapshot(), states); err != nil {
		if a.host.ReportError != nil {
			a.host.ReportError(err)
		}
		return
	}
	for _, state := range states {
		for _, item := range state.AsyncQuestions {
			for _, q := range a.textControl.AsyncButtonQuestions(item.ID) {
				if q.State == "" {
					text := fmt.Sprintf("Worker question %s is pending. Read the original question with bot_interactions.", q.ShortID)
					a.deliverWorkerInteraction(ctx, "question", item.ID+":"+q.Fingerprint, text)
					break
				}
			}
		}
	}

}

func (a *Application) WorkerQuestions() []api.WorkerQuestion {
	titles := map[string]string{}
	activeItems := map[string]bool{}
	for _, state := range a.workerStates() {
		titles[state.Task.ID] = state.Task.Title
		for _, item := range state.AsyncQuestions {
			activeItems[item.ID] = true
		}
	}
	out := []api.WorkerQuestion{}
	for _, q := range a.textControl.WorkerAsyncQuestions() {
		// Uncertain answers reconcile their original input receipt even after
		// native temporary question payloads leave the observation window.
		if !activeItems[q.ItemID] && q.State != "unknown" && q.State != "dispatching" {
			continue
		}
		out = append(out, api.WorkerQuestion{ID: q.ShortID, Binding: q.Binding, OperationID: q.OperationID, TaskID: q.WorkID, TaskTitle: titles[q.WorkID], Title: q.Title, Options: q.Options, State: q.State, Reason: q.Reason, ExpiresAt: q.ExpiresAt})
	}
	return out
}

func (a *Application) AnswerWorkerQuestion(ctx context.Context, shortID, value, requestID string) (api.Receipt, error) {
	if len(requestID) < 8 || len(requestID) > 128 || shortID == "" || value == "" {
		return api.Receipt{ID: requestID, Outcome: "rejected", Message: "question, answer, and stable request ID required"}, nil
	}
	id, binding, _ := strings.Cut(shortID, "@")
	for _, q := range a.WorkerQuestions() {
		if q.ID == id && binding != "" && binding == q.Binding {
			return a.textControl.HandleBotWorkAnswer(ctx, requestID, shortID, value), nil
		}
	}
	return api.Receipt{ID: requestID, Outcome: "rejected", Message: "original Worker question unavailable"}, nil
}

func (a *Application) answerWorkerQuestion(ctx context.Context, in textchannel.Inbound, workID, owner, itemID, modelInput string) (api.Receipt, error) {
	if in.Channel != "bot" {
		defer a.beginUserActivity()()
	}
	for _, state := range a.workerStates() {
		if state.Task.ID != workID || state.Runtime != owner {
			continue
		}
		for _, question := range state.AsyncQuestions {
			if question.ID != itemID {
				continue
			}
			a.mu.Lock()
			manager := a.tasks
			a.mu.Unlock()
			if manager == nil {
				return api.Receipt{ID: in.ID, Outcome: "rejected", Message: "Worker 管理器未就绪。"}, nil
			}
			requestID := textchannel.WorkerAnswerRequestID(in, workID, itemID)
			// This reply has no file transfer. Keep a slow peer inside the
			// existing Worker admission budget, not the general Web lifetime.
			if state.Task.Machine != "" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, api.WorkerAdmissionTimeout)
				defer cancel()
			}
			result, err := manager.SendTask(api.WithWorkerQuestionAnswer(ctx, question.AsyncCallID), api.TaskMessage{ID: workID, RequestID: requestID, Prompt: modelInput})
			outcome := result.Outcome
			// Question receipts retain their accepted/rejected/unknown contract.
			if outcome == "conflicted" {
				outcome = "rejected"
			}
			if outcome != "accepted" && outcome != "rejected" {
				outcome = "unknown"
			}
			message := ""
			if err != nil {
				reason := "native_failure"
				var rejected *api.WorkMessageRejection
				if errors.As(err, &rejected) {
					reason = "input_rejected"
					if rejected.Code == "owner_unavailable" || rejected.Code == "original_receipt_unresolved" || rejected.Code == "original_question_unavailable" || rejected.Code == "activation_required" {
						reason = rejected.Code
					}
				}
				if errors.Is(err, context.DeadlineExceeded) {
					reason = "timeout"
				}
				if errors.Is(err, context.Canceled) {
					reason = "cancelled"
				}
				a.host.Diagnostics.Write(diagnosticlog.Record{Level: "warn", Component: "worker_question", Code: "answer_" + outcome,
					Reason: reason, Item: requestID, Fingerprint: diagnosticlog.Fingerprint([]byte(err.Error()))})
				if outcome == "rejected" {
					message = "请确认原任务已连接，再用此问题卡片提交回答。"
				} else {
					message = "请先核对原任务是否收到回答，暂勿重复提交。"
				}
			}
			return api.Receipt{ID: in.ID, Outcome: outcome, Message: message}, err
		}
	}
	return api.Receipt{ID: in.ID, Outcome: "rejected", Message: "原 Worker 问题已失效。"}, nil
}

func (a *Application) PresentWorkerQuestion(ctx context.Context, id string, presentation *api.QuestionPresentation) (api.Receipt, error) {
	a.observeWorkerQuestions(ctx)
	item, err := a.textControl.PresentWorkerQuestion(id, presentation)
	if err != nil {
		return api.Receipt{ID: id, Outcome: "rejected", Message: err.Error()}, nil
	}
	a.Backend.ObserveChat(api.Snapshot{Items: []api.Item{item}})
	return api.Receipt{ID: id, Outcome: "accepted", Message: "Original Worker question presented; no answer supplied."}, nil
}
func (w *localWorkers) ReceiveQuestionClosure(ctx context.Context, in api.QuestionClosure) error {
	w.mu.Lock()
	runtime := w.routes[in.WorkID]
	owner := w.resident
	if runtime != w.active {
		owner = w.owners[runtime]
	}
	w.mu.Unlock()
	if runtime != in.Owner || owner == nil {
		return errors.New("original question owner unavailable")
	}
	receiver, ok := owner.(api.QuestionClosureReceiver)
	if !ok {
		return errors.New("original owner cannot retain question closure")
	}
	return receiver.ReceiveQuestionClosure(ctx, in)
}

// Finish is a host persistence boundary. Register the freshly inspected source
// items before closing them, so a fast continuation cannot skip their closure.
func (a *Application) reconcileFinishedQuestions(state api.WorkState) error {
	for _, item := range state.AsyncQuestions {
		if _, err := a.textControl.AsyncWorkCard(item, state.Runtime, state.Task.ID); err != nil {
			return err
		}
	}
	return a.textControl.ReconcileAsync(api.Snapshot{}, []api.WorkState{state})
}

func (a *Application) configureInteractionLifecycle(manager *tasks.Manager) {
	manager.SetFinishedObserver(a.reconcileFinishedQuestions)
	manager.SetOperationObserver(func(v api.WorkOperation) error {
		outcome := v.Outcome
		if outcome == "conflicted" {
			outcome = "rejected"
		}
		return a.textControl.SettleAsyncOperation(v.TaskID, v.RequestID, outcome)
	})
}

// At most one original receipt read per host tick, three attempts per operation
// in this process. Failed reads remain unknown; no answer/input is replayed.
func (a *Application) reconcileAnswerReceipt(ctx context.Context) {
	receipts := a.textControl.AsyncAnswerReceipts()
	live := map[string]bool{}
	for _, q := range receipts {
		live[q.WorkID+"\x00"+q.OperationID] = true
	}
	a.mu.Lock()
	for key := range a.questionReceiptAttempts {
		if !live[key] {
			delete(a.questionReceiptAttempts, key)
		}
	}
	a.mu.Unlock()
	for _, q := range receipts {
		key := q.WorkID + "\x00" + q.OperationID
		a.mu.Lock()
		manager := a.tasks
		if a.questionReceiptAttempts == nil {
			a.questionReceiptAttempts = map[string]int{}
		}
		attempts := a.questionReceiptAttempts[key]
		if manager != nil && attempts < 3 {
			a.questionReceiptAttempts[key] = attempts + 1
		}
		a.mu.Unlock()
		if manager == nil || attempts >= 3 {
			continue
		}
		bounded, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
		_, _ = manager.ReconcileTaskRequest(bounded, q.WorkID, q.OperationID)
		cancel()
		return
	}
}
