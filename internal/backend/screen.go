package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/screeninput"
)

func ConfigureScreenMedia(s *Service, path string) error {
	s.screenMedia, s.screenMediaError = screeninput.OpenMedia(path)
	return s.screenMediaError
}
func (s *Service) ScreenImage(id string, thumbnail bool) (string, error) {
	return s.screenMedia.DataURL(id, thumbnail)
}
func ScreenImageBytes(s *Service, id string) ([]byte, error) {
	data, _, err := s.screenMedia.Bytes(id, false)
	return data, err
}
func ScreenMediaStorage(s *Service, clean bool, trash func(string) error) (api.AttachmentStorage, error) {
	return s.screenMedia.Storage(clean, trash)
}

// ScreenSnapshot exposes only authoritative runtime facts to native capture
// recovery. The presentation outbox in Service.Snapshot is not delivery evidence.
func ScreenSnapshot(s *Service) api.Snapshot {
	s.admission.RLock()
	defer s.admission.RUnlock()
	if s.restarting || s.setupRequired {
		return api.Snapshot{}
	}
	return s.engine.Snapshot()
}

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
	if s.screenMediaError != nil {
		return r, errors.New("screen image storage unavailable")
	}
	if s.screenMedia != nil {
		if err := s.screenMedia.Save(input.ID, files); err != nil {
			return r, err
		}
	}
	s.stageOutgoing(input, files)
	r, err = s.engine.Submit(ctx, input, files)
	s.finishOutgoing(input.ID, r)
	return r, err
}
