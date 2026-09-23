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
