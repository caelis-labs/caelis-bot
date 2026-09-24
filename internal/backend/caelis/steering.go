package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (s *Session) isSteeringRetry(op string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.HasSuffix(s.state.Operations[op].Path, "/steer")
}

// Freeze the expected Turn in the durable outbound intent. A retry must never
// acquire a later Turn's identity or become a prompt after the old Turn ends.
func (s *Session) submitNativeInput(ctx context.Context, sid, op, text string, parts []wire.PromptContentPart, steer bool) (wire.CommandResult, error) {
	s.mu.Lock()
	previous, exists := s.state.Operations[op]
	v := s.state.Views[sid]
	var target wire.TurnTarget
	if v != nil {
		target = wire.TurnTarget{HandleId: value(v.State.Run.HandleId), RunId: value(v.State.Run.RunId), TurnId: value(v.State.Run.TurnId)}
	}
	s.mu.Unlock()
	if exists {
		steer = strings.HasSuffix(previous.Path, "/steer")
		if len(previous.Body) > 0 {
			var saved wire.SteerRequest
			if err := json.Unmarshal(previous.Body, &saved); err != nil {
				return wire.CommandResult{}, err
			}
			if value(saved.Input) != text || !reflect.DeepEqual(saved.ContentParts, parts) {
				return wire.CommandResult{OperationId: op, Outcome: "conflicted"}, errors.New("操作标识已用于不同消息")
			}
			target = saved.Target
		}
	}
	if steer {
		if target.HandleId == "" || target.RunId == "" || target.TurnId == "" {
			return wire.CommandResult{OperationId: op, Outcome: "rejected"}, errors.New("运行目标未确认，请等待恢复")
		}
		return s.command(ctx, op, "/sessions/"+idPath(sid)+"/steer", wire.SteerRequest{OperationId: &op, SessionId: &sid, Target: target, Input: &text, ContentParts: parts})
	}
	return s.command(ctx, op, "/sessions/"+idPath(sid)+"/prompt", wire.PromptRequest{OperationId: &op, SessionId: &sid, Input: &text, ContentParts: parts})
}
