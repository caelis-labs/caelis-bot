package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) ConfigureInitialization(i api.BotInitializer) {
	s.mu.Lock()
	s.initializer = i
	s.mu.Unlock()
}
func (s *Service) BotInitialization() api.BotInitialization {
	s.mu.Lock()
	i := s.initializer
	s.mu.Unlock()
	if i == nil {
		return api.BotInitialization{Required: true, Status: "loading", Message: "正在准备 Bot…"}
	}
	return i.Initialization()
}
func (s *Service) InitializeBot(ctx context.Context, in api.BotIntroduction) (api.BotInitialization, error) {
	s.mu.Lock()
	i := s.initializer
	s.mu.Unlock()
	if i == nil {
		return api.BotInitialization{}, errors.New("Bot 正在准备，请稍后重试")
	}
	return i.Initialize(ctx, in)
}

func (s *Service) RetryBotIntroduction(ctx context.Context) (api.BotInitialization, error) {
	s.mu.Lock()
	i := s.initializer
	s.mu.Unlock()
	if i == nil {
		return api.BotInitialization{}, errors.New("Bot 尚未准备好")
	}
	return i.RetryInitialization(ctx)
}

// Project the durable, not-yet-accepted introduction without creating another
// submission. Native acceptance replaces it by the same request identity.
func (s *Service) projectIntroduction(v *api.Snapshot) {
	s.mu.Lock()
	initializer := s.initializer
	s.mu.Unlock()
	if initializer == nil {
		return
	}
	intro := initializer.Initialization()
	if intro.Prompt == "" {
		return
	}
	found := false
	for _, item := range v.Items {
		if item.RequestID == intro.RequestID {
			found = true
			break
		}
	}
	if !found {
		v.Items = append(v.Items, api.Item{ID: "outgoing:" + intro.RequestID, RequestID: intro.RequestID, Kind: "user", Text: intro.Prompt})
	}
	v.IntroductionPending = (intro.Status == "pending" || intro.Status == "dispatching") && (v.Connection == "ready" || v.Connection == "connecting")
	if v.IntroductionPending && (v.Connection == "connecting" || v.Message == intro.Message) {
		v.Message = ""
	}
}
