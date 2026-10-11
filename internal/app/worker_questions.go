package app

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/textchannel"
)

// The Worker owner persists native async question identities. The resident Bot
// receives the question through its private host report and asks the user in
// ordinary chat only when it needs more information.
func (a *Application) observeWorkerQuestions(ctx context.Context) {
	for _, state := range a.localWork.WorkStates() {
		for _, item := range state.AsyncQuestions {
			_, err := a.textControl.AsyncWorkCard(item, state.Runtime, state.Task.ID)
			if err != nil {
				if a.host.ReportError != nil {
					a.host.ReportError(err)
				}
				continue
			}
			for _, q := range a.textControl.AsyncButtonQuestions(item.ID) {
				if q.State == "" {
					text := fmt.Sprintf("Worker task %s has an asynchronous question %s. Inspect bot_interactions, answer it from the user's task context if clear, or ask the user naturally when information is missing. The original Worker must receive the exact reply.", state.Task.Title, q.ShortID)
					a.reportWorkerInteraction(ctx, "question", item.ID+":"+q.Fingerprint, text)
					break
				}
			}
		}
	}
}

func (a *Application) WorkerQuestions() []api.WorkerQuestion {
	titles := map[string]string{}
	activeItems := map[string]bool{}
	for _, state := range a.localWork.WorkStates() {
		titles[state.Task.ID] = state.Task.Title
		for _, item := range state.AsyncQuestions {
			activeItems[item.ID] = true
		}
	}
	out := []api.WorkerQuestion{}
	for _, q := range a.textControl.WorkerAsyncQuestions() {
		if !activeItems[q.ItemID] {
			continue
		}
		out = append(out, api.WorkerQuestion{ID: q.ShortID, TaskID: q.WorkID, TaskTitle: titles[q.WorkID], Title: q.Title, Options: q.Options, State: q.State})
	}
	return out
}

func (a *Application) AnswerWorkerQuestion(ctx context.Context, shortID, value, requestID string) (api.Receipt, error) {
	if len(requestID) < 8 || len(requestID) > 128 || shortID == "" || value == "" {
		return api.Receipt{ID: requestID, Outcome: "rejected", Message: "question, answer, and stable request ID required"}, nil
	}
	for _, q := range a.WorkerQuestions() {
		if q.ID == shortID {
			return a.textControl.HandleBotWorkAnswer(ctx, requestID, shortID, value), nil
		}
	}
	return api.Receipt{ID: requestID, Outcome: "rejected", Message: "original Worker question unavailable"}, nil
}

func (a *Application) answerWorkerQuestion(ctx context.Context, in textchannel.Inbound, workID, owner, itemID, modelInput string) (api.Receipt, error) {
	for _, state := range a.localWork.WorkStates() {
		if state.Task.ID != workID || state.Runtime != owner || state.Task.Status == "unavailable" {
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
			sum := sha256.Sum256([]byte(in.Channel + "\x00" + in.Conversation + "\x00" + in.ID + "\x00" + workID + "\x00" + itemID))
			requestID := fmt.Sprintf("worker-answer-%x", sum[:])
			result, err := manager.SendTask(ctx, api.TaskMessage{ID: workID, RequestID: requestID, Prompt: modelInput})
			outcome := result.Outcome
			if outcome == "" {
				outcome = "unknown"
			}
			message := ""
			if err != nil {
				message = err.Error()
			}
			return api.Receipt{ID: in.ID, Outcome: outcome, Message: message}, err
		}
	}
	return api.Receipt{ID: in.ID, Outcome: "rejected", Message: "原 Worker 问题已失效。"}, nil
}
