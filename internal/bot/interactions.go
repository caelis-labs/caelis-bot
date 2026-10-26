package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (r *Runtime) callCompactInteractions(ctx context.Context, q map[string]any) api.ToolResult {
	r.mu.Lock()
	port := r.interactions
	r.mu.Unlock()
	if port == nil {
		return compactError("unavailable", errors.New("Worker interactions unavailable"))
	}
	switch str(q, "type") {
	case "list":
		return interactionPage(port, q)
	case "read":
		return readInteraction(port, q)
	case "decide":
		decision := api.Decision{ID: str(q, "approval"), Choice: str(q, "choice")}
		if raw, ok := q["answers"]; ok {
			fields, ok := raw.(map[string]any)
			if !ok {
				return compactError("invalid_arguments", errors.New("answers must map question IDs to string arrays"))
			}
			decision.Answers = make(map[string][]string, len(fields))
			for id, rawValues := range fields {
				values, ok := rawValues.([]any)
				if !ok {
					return compactError("invalid_arguments", errors.New("answers must map question IDs to string arrays"))
				}
				for _, rawValue := range values {
					value, ok := rawValue.(string)
					if !ok {
						return compactError("invalid_arguments", errors.New("answer values must be strings"))
					}
					decision.Answers[id] = append(decision.Answers[id], value)
				}
			}
		}
		approval, err := port.DecideWorkerApproval(ctx, decision)
		if err != nil {
			return compactValue(map[string]any{"ok": false, "outcome": "unknown", "data": approval, "error": map[string]string{"code": "native_decision", "message": err.Error()}}, true)
		}
		return compactValue(map[string]any{"ok": true, "outcome": approval.Status, "data": approval, "next": map[string]any{"tool": "bot_interactions", "request": map[string]any{"type": "list"}}}, false)
	case "present":
		presenter, ok := port.(api.WorkerQuestionPresenter)
		if !ok {
			return compactError("unavailable", errors.New("original Worker question presentation unavailable"))
		}
		var presentation *api.QuestionPresentation
		if raw, ok := q["presentation"]; ok {
			encoded, err := json.Marshal(raw)
			if err != nil {
				return compactError("invalid_arguments", err)
			}
			presentation = &api.QuestionPresentation{}
			if err := json.Unmarshal(encoded, presentation); err != nil {
				return compactError("invalid_arguments", err)
			}
		}
		receipt, err := presenter.PresentWorkerQuestion(ctx, str(q, "question"), presentation)
		if err != nil {
			return compactError("question_presentation", err)
		}
		return compactValue(map[string]any{"ok": receipt.Outcome == "accepted", "outcome": receipt.Outcome, "data": receipt}, receipt.Outcome != "accepted")
	case "answer":
		receipt, err := port.AnswerWorkerQuestion(ctx, str(q, "question")+"@"+str(q, "binding"), str(q, "value"), str(q, "requestId"))
		if err != nil {
			return compactValue(map[string]any{"ok": false, "outcome": "unknown", "data": receipt, "error": map[string]string{"code": "worker_answer", "message": err.Error()}}, true)
		}
		return compactValue(map[string]any{"ok": receipt.Outcome == "accepted", "outcome": receipt.Outcome, "data": receipt}, receipt.Outcome != "accepted")
	}
	return compactError("invalid_arguments", errors.New("unknown Worker interaction action"))
}
