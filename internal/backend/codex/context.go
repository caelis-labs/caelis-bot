package codex

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/contextseed"
)

type dreamRecord struct {
	Thread string `json:"thread"`
}

func (s *Session) prepareContextLocked(ctx context.Context, id string, input []map[string]any) ([]map[string]any, error) {
	s.cleanupContextLocked()
	text, err := s.binding.Context.Prepare(ctx, s.opts.BotTools, id)
	if err != nil || text == "" {
		return input, err
	}
	if s.binding.ContextInputs == nil {
		s.binding.ContextInputs = map[string]int{}
	}
	s.binding.ContextInputs[id] = len(text)
	if len(input) > 0 && input[0]["type"] == "text" {
		input[0]["text"] = text + input[0]["text"].(string)
		return input, nil
	}
	return append([]map[string]any{{"type": "text", "text": text, "text_elements": []any{}}}, input...), nil
}

func (s *Session) cleanupContextLocked() {
	if p := s.binding.Context.Pending; p == nil || !p.Accepted {
		return
	}
	if s.save() != nil {
		return
	}
	if s.binding.Context.Cleanup(s.opts.BotTools) == nil {
		_ = s.save()
	}
}

func (s *Session) conversationLocked() api.ConversationState {
	return api.ConversationState{
		Session: s.binding.ThreadID, Turn: s.lastTurn, Status: s.runs[s.lastTurn],
		Observed: s.bound && !s.loading && s.state.Connection == "ready" && !s.closed && !s.closing,
		Idle:     s.state.CanSend && s.opts.Execution.ApprovalMode != "read-only",
	}
}
func (s *Session) ConversationState() api.ConversationState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conversationLocked()
}
func (s *Session) SubmitDream(ctx context.Context, in api.Submission) (api.Receipt, error) {
	in.Dream, in.Scheduled = true, true
	return s.submitWithSource(ctx, in, nil, true, true)
}
func (s *Session) DreamResult(id string) (api.Receipt, api.ConversationState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, exists := s.binding.Dreams[id]
	turn := s.binding.Scheduled[id]
	out := api.Receipt{ID: id, Outcome: "unknown"}
	if !exists {
		out.Outcome = "rejected"
	}
	if exists && turn != "" {
		out.Outcome = "accepted"
	}
	if s.binding.LastReceipt != nil && s.binding.LastReceipt.ID == id {
		out = *s.binding.LastReceipt
	}
	result := api.ConversationState{Session: d.Thread, Turn: turn, Status: s.runs[turn]}
	if result.Status == "completed" && !s.dreamRecapLocked(turn) {
		result.Status = "failed"
	}
	return out, result
}
func (s *Session) dreamRecapLocked(turn string) bool {
	for _, item := range s.state.Items {
		if item.TurnKey == opaque(turn) && item.Kind == "assistant" && strings.TrimSpace(item.Text) != "" {
			return true
		}
	}
	return false
}

// Interrupt exactly the maintenance turn, leaving workers and process ownership alone.
func (s *Session) CancelDream(ctx context.Context, id string) error {
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 15*time.Second)
	defer cancel()
	s.mu.Lock()
	d, exists := s.binding.Dreams[id]
	turn, c := s.binding.Scheduled[id], s.client
	if exists && turn != "" && terminal(s.runs[turn]) {
		s.mu.Unlock()
		return nil
	}
	if !exists || turn == "" || c == nil || d.Thread != s.binding.ThreadID || s.run != turn {
		s.mu.Unlock()
		return errors.New("整理请求尚未确认，请恢复连接核对")
	}
	s.mu.Unlock()
	if err := callDecode(ctx, c, "turn/interrupt", map[string]string{"threadId": d.Thread, "turnId": turn}, nil); err != nil {
		return err
	}
	for {
		s.mu.Lock()
		done, changed := terminal(s.runs[turn]), s.changed
		s.mu.Unlock()
		if done {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		case <-c.Done():
			return errors.New("整理停止结果尚未确认")
		}
	}
}

func (s *Session) RenewConversation(ctx context.Context, id, source string) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	ctx, cancel := s.operation(ctx, 30*time.Second)
	defer cancel()
	s.mu.Lock()
	if s.binding.RenewedBy == id {
		s.mu.Unlock()
		return nil
	}
	d, exists := s.binding.Dreams[id]
	turn := s.binding.Scheduled[id]
	if !exists || d.Thread != source || s.binding.ThreadID != source || s.lastTurn != turn || s.runs[turn] != "completed" || !s.state.CanSend {
		s.mu.Unlock()
		return errors.New("对话已有新活动，暂不能交接")
	}
	c := s.client
	s.mu.Unlock()
	var response threadExecutionResponse
	// This creates only an empty native thread. No prompt is sent until Submit.
	// A lost create response may leave an unused empty thread; the old binding stays authoritative.
	if err := callDecode(ctx, c, "thread/start", s.connectionParams(), &response); err != nil {
		return err
	}
	if response.Thread.ID == "" || response.Thread.ID == source {
		return ErrProtocol
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.binding.ThreadID != source || s.lastTurn != turn || s.run != "" || !s.state.CanSend {
		return errors.New("原对话已有新活动，交接未切换")
	}
	old := s.binding
	s.binding.ThreadID, s.binding.RenewedBy = response.Thread.ID, id
	s.binding.PastThreads = append(append([]string{}, old.PastThreads...), source)
	s.binding.Context, s.binding.Unsubmitted = contextseed.State{}, true
	if err := s.save(); err != nil {
		s.binding = old
		return err
	}
	s.residentExecution = *response.execution()
	s.lastTurn, s.run = "", ""
	s.state.Phase, s.state.Message = "idle", ""
	s.update()
	return nil
}

var _ api.ConversationRuntime = (*Session)(nil)
