package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) ImageInput(ctx context.Context) (api.ImageInputCapability, error) {
	s.admission.RLock()
	defer s.admission.RUnlock()
	if s.restarting || s.setupRequired {
		return api.ImageInputCapability{State: "unknown"}, nil
	}
	if p, ok := s.engine.(api.ImageInputProvider); ok {
		return p.ImageInput(ctx)
	}
	return api.ImageInputCapability{State: "unknown"}, nil
}

// SubmitScreen is wired only to the native capture owner; no renderer-supplied
// path is accepted. It does not consume or replace the ordinary composer draft.
func SubmitScreen(ctx context.Context, s *Service, input api.Submission, files []api.InputFile) (api.Receipt, error) {
	s.admission.RLock()
	defer s.admission.RUnlock()
	r := api.Receipt{ID: input.ID, Outcome: "rejected"}
	if s.restarting || s.setupRequired {
		return r, errors.New("runtime setup required")
	}
	s.mu.Lock()
	initializer := s.initializer
	s.mu.Unlock()
	if initializer != nil && initializer.Initialization().Status != "accepted" {
		return r, errors.New("Bot initialization required")
	}
	p, ok := s.engine.(api.ImageInputProvider)
	if !ok {
		return r, errors.New("image input capability unavailable")
	}
	capability, err := p.ImageInput(ctx)
	if err != nil || capability.State != "supported" {
		return r, errors.New("the current Bot model does not confirm image input support")
	}
	input.ScreenInput = true
	input.FileIDs, input.ReferenceIDs = nil, nil
	s.stageOutgoing(input, files)
	r, err = s.engine.Submit(ctx, input, files)
	s.finishOutgoing(input.ID, r)
	return r, err
}
