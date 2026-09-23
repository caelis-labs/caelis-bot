package desktop

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/updates"
)

// Contextual connection actions and menu commands share one settings window.
func (s *Service) showSettings(section string) {
	s.mu.Lock()
	s.settingsSection = section
	f := s.openSettings
	ready := s.started && !s.stopped
	s.mu.Unlock()
	if ready && f != nil {
		f()
	}
}
func (s *Service) OpenSettings()        { s.showSettings("general") }
func (s *Service) OpenRuntimeSettings() { s.showSettings("runtime") }
func (s *Service) OpenUpdates()         { s.showSettings("updates") }
func (s *Service) CloseSettings() {
	if s.closeSettings != nil {
		s.closeSettings()
	}
}
func (s *Service) SettingsSection() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settingsSection == "" {
		return "general"
	}
	return s.settingsSection
}
func (s *Service) AppVersion() string                              { return updates.Version }
func (s *Service) CheckUpdates(ctx context.Context) updates.Result { return updates.Check(ctx) }
func (s *Service) OpenReleasePage() error {
	if s.openReleasePage == nil {
		return errors.New("发布页暂不可用")
	}
	return s.openReleasePage()
}

func (s *Service) RestartForRuntime() error {
	if s.restartRuntime == nil {
		return errors.New("重新启动暂不可用，请退出后重新打开 Caelis Bot")
	}
	return s.restartRuntime()
}
