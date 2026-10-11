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
	// Read the Control generation and native owner in one recovery observation.
	// The adapter rechecks NativeIngressFence under its submission lock, closing
	// the race between this preflight and native dispatch.
	s.recoveryMu.Lock()
	state, nativeFence := s.recoveryStateWithNativeLocked()
	s.recoveryMu.Unlock()
	if state.Automatic || state.InProgress || in.IngressFence != "" && in.IngressFence != state.Fence {
		return api.Receipt{}, api.ErrRecoveryPending
	}
	in.NativeIngressFence = nativeFence
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
	staged := s.stageOutgoing(in, files)
	r, err := s.submit(ctx, in, files)
	if errors.Is(err, api.ErrRecoveryPending) {
		if staged {
			s.discardOutgoing(in.ID)
		}
		return api.Receipt{}, err
	}
	s.finishOutgoing(in.ID, r)
	s.recordInput(in, files, r)
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
