package codex

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type retainedQuestionClosure struct {
	Closure api.QuestionClosure `json:"closure"`
	Thread  string              `json:"thread"`
	// Claimed before steer. An unknown delivery is never retried.
	Attempted bool `json:"attempted"`
}

// ReceiveQuestionClosure retains host context, without turn/start, task
// continuation or a question-reply envelope. The exact active turn can receive
// one steer. An idle owner receives the context at its next authorized input.
func (s *Session) ReceiveQuestionClosure(ctx context.Context, in api.QuestionClosure) error {
	if !s.op.TryLock() {
		return errors.New("original owner is busy; closure remains queued")
	}
	defer s.op.Unlock()
	s.mu.Lock()
	if _, ok := s.binding.QuestionClosures[in.ID]; ok {
		s.mu.Unlock()
		return nil
	}
	thread, run := s.binding.ThreadID, s.run
	if in.WorkID != "" {
		task := s.binding.Tasks[in.WorkID]
		if task == nil || s.binding.LostOwnerTasks[in.WorkID] {
			s.mu.Unlock()
			return errors.New("original Worker unavailable")
		}
		found := false
		for _, q := range task.AsyncQuestions {
			if q.CallID == in.CallID && in.Index >= 0 && in.Index < len(q.Questions) {
				found = true
			}
		}
		if !found {
			s.mu.Unlock()
			return errors.New("original question unavailable")
		}
		thread, run = task.Thread, s.childRuns[task.Thread]
		if terminal(task.View.Status) || task.SuppressReport {
			run = ""
		}
	} else if in.Owner != opaque("codex", thread) {
		s.mu.Unlock()
		return errors.New("original question owner replaced")
	}
	if in.ID == "" || thread == "" || (in.State != "dismissed" && in.State != "expired" && in.State != "superseded") {
		s.mu.Unlock()
		return errors.New("invalid question closure")
	}
	if s.binding.QuestionClosures == nil {
		s.binding.QuestionClosures = map[string]retainedQuestionClosure{}
	}
	c := s.client
	attempt := c != nil && s.state.Connection == "ready" && run != ""
	s.binding.QuestionClosures[in.ID] = retainedQuestionClosure{Closure: in, Thread: thread, Attempted: attempt}
	clientID := "question-closed-" + opaque(in.ID)
	if attempt && in.WorkID == "" {
		if s.binding.HostReports == nil {
			s.binding.HostReports = map[string]bool{}
		}
		s.binding.HostReports[clientID] = true
	}
	if err := s.save(); err != nil {
		delete(s.binding.QuestionClosures, in.ID)
		if in.WorkID == "" {
			delete(s.binding.HostReports, clientID)
		}
		s.mu.Unlock()
		return err
	}
	text := questionClosureText([]api.QuestionClosure{in})
	s.mu.Unlock()
	if attempt {
		// A terminal race rejects expectedTurnId. Do not fall back to turn/start or
		// retry: the durable context remains available for a later authorized input.
		var response struct {
			TurnID string `json:"turnId"`
		}
		_ = callDecode(ctx, c, "turn/steer", map[string]any{"threadId": thread, "expectedTurnId": run, "clientUserMessageId": clientID, "input": []nativeInput{{Type: "text", Text: text, TextElements: []any{}}}}, &response)
	}
	return nil
}
func (w *WorkOwner) ReceiveQuestionClosure(ctx context.Context, in api.QuestionClosure) error {
	return w.engine.ReceiveQuestionClosure(ctx, in)
}

func questionClosureText(notices []api.QuestionClosure) string {
	type entry struct {
		Call   string `json:"questionCallId"`
		Index  int    `json:"index"`
		State  string `json:"state"`
		Reason string `json:"reason"`
	}
	values := make([]entry, 0, len(notices))
	for _, q := range notices {
		values = append(values, entry{q.CallID, q.Index, q.State, q.Reason})
	}
	body, _ := json.Marshal(values)
	return "<host_question_lifecycle>\n" + string(body) + "\nThese questions are closed without an answer. This is host lifecycle context, not a user request, answer, or permission. Keep any steps requiring the missing input blocked and report the blocker. Do not choose a default or restart completed work.\n</host_question_lifecycle>\n\n"
}
func (s *Session) questionClosureContextLocked(thread string) string {
	var notices []api.QuestionClosure
	for _, record := range s.binding.QuestionClosures {
		if record.Thread == thread {
			notices = append(notices, record.Closure)
		}
	}
	if len(notices) == 0 {
		return ""
	}
	slices.SortFunc(notices, func(a, b api.QuestionClosure) int { return strings.Compare(a.ID, b.ID) })
	return questionClosureText(notices)
}
