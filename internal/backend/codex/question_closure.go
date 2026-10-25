package codex

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// ReceiveQuestionClosure acknowledges legacy peer requests without model input
// or retained context. Question expiry and dismissal belong to the host UI.
// Keep this no-effect compatibility port until the peer contract is retired.
func (s *Session) ReceiveQuestionClosure(context.Context, api.QuestionClosure) error   { return nil }
func (w *WorkOwner) ReceiveQuestionClosure(context.Context, api.QuestionClosure) error { return nil }
