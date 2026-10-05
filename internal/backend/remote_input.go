package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// SubmitRemote uses the same resident submission and admission checks as the
// composer. Files are supplied by a native owner, never by a renderer. A second
// client must not consume or clear the desktop's unfinished draft.
func SubmitRemote(ctx context.Context, s *Service, in api.Submission, files []api.InputFile) (api.Receipt, error) {
	s.admission.RLock()
	defer s.admission.RUnlock()
	r := api.Receipt{ID: in.ID, Outcome: "rejected"}
	if s.restarting || s.setupRequired {
		return r, errors.New("runtime setup required")
	}
	s.mu.Lock()
	initializer := s.initializer
	s.mu.Unlock()
	if initializer != nil && initializer.Initialization().Status != "accepted" {
		return r, errors.New("Bot initialization required")
	}
	in.FileIDs, in.ReferenceIDs = nil, nil
	if err := s.retainMessageMedia(in, files); err != nil {
		r.Message = "图片预览存储暂不可用，消息未发送"
		return r, nil
	}
	s.stageOutgoing(in, files)
	r, err := s.submit(ctx, in, files)
	s.finishOutgoing(in.ID, r)
	return r, err
}

func ObserveSubmissions(s *Service, observer func(api.Submission, []api.InputFile, api.Receipt)) {
	s.mu.Lock()
	s.submissionObserver = observer
	s.mu.Unlock()
}

// ResolveRemoteArtifact keeps export restricted to artifacts already owned by
// the backend. A Telegram identifier cannot be turned into an arbitrary path.
func ResolveRemoteArtifact(s *Service, id string) (string, error) {
	s.admission.RLock()
	defer s.admission.RUnlock()
	if resolver, ok := s.engine.(api.ArtifactResolver); ok && !s.restarting && !s.setupRequired {
		return resolver.Artifact(id)
	}
	return "", errors.New("artifact unavailable")
}
