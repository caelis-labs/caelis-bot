package desktop

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/updates"
)

// Contextual connection actions and menu commands share one settings window.
func (s *Service) showSettings(section string) {
	if s.needsIntroduction != nil && s.needsIntroduction() {
		section = "setup"
	}
	s.mu.Lock()
	s.settingsSection = section
	f := s.openSettings
	ready := s.started && !s.stopped
	s.mu.Unlock()
	if ready && f != nil {
		f()
	}
}
func (s *Service) OpenSettings() {
	if s.needsIntroduction != nil && s.needsIntroduction() {
		s.showSettings("setup")
		return
	}
	s.showSettings("general")
}
func (s *Service) OpenRuntimeSettings() { s.showSettings("runtime") }
func (s *Service) OpenUpdates() {
	s.showSettings("updates")
	if s.UpdatePreferences().Available {
		_ = s.CheckUpdates(context.Background())
	}
}
func (s *Service) CloseSettings() {
	if s.closeSettings != nil {
		s.closeSettings()
	}
}
func (s *Service) SettingsVisible() bool {
	s.mu.Lock()
	f := s.settingsVisible
	s.mu.Unlock()
	return f != nil && f()
}
func (s *Service) SettingsSection() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settingsSection == "" {
		return "general"
	}
	return s.settingsSection
}
func (s *Service) AppVersion() string { return updates.Version }
func (s *Service) CheckUpdates(ctx context.Context) updates.Result {
	s.mu.Lock()
	f := s.checkNativeUpdates
	p := s.updatePreferences
	s.mu.Unlock()
	if f != nil && p != nil && p().Available {
		if err := f(); err != nil {
			return updates.Result{State: "unavailable", Current: updates.Version, Message: err.Error()}
		}
		return updates.Result{State: "native", Current: updates.Version, Message: s.text("native.checkProgressInUpdateWindow", nil)}
	}
	return updates.Check(ctx, s.LanguagePreferences().Locale)
}

type UpdatePreferences struct {
	Available bool   `json:"available"`
	Automatic bool   `json:"automatic"`
	Waiting   bool   `json:"waiting"`
	State     string `json:"state,omitempty"`
	Message   string `json:"message,omitempty"`
	Format    string `json:"format,omitempty"`
	Latest    string `json:"latest,omitempty"`
}

func (s *Service) InstallUpdate() error {
	s.mu.Lock()
	f := s.installNativeUpdate
	s.mu.Unlock()
	if f == nil {
		return errors.New(s.text("native.autoUpdateUnavailable", nil))
	}
	return f()
}

func (s *Service) UpdatePreferences() UpdatePreferences {
	s.mu.Lock()
	f := s.updatePreferences
	s.mu.Unlock()
	if f == nil {
		return UpdatePreferences{}
	}
	return f()
}

func (s *Service) SetAutomaticUpdates(enabled bool) error {
	s.mu.Lock()
	f := s.setAutomaticUpdates
	s.mu.Unlock()
	if f == nil {
		return errors.New(s.text("native.autoUpdateUnavailable", nil))
	}
	return f(enabled)
}
func (s *Service) OpenReleasePage() error {
	if s.openReleasePage == nil {
		return errors.New(s.text("native.releasePageUnavailable", nil))
	}
	return s.openReleasePage()
}

// Snapshot service state before entering the UI thread. Native updates can hold
// mu while waiting for that thread, so its presentation callback must not read
// service preferences or translations under mu.
func (s *Service) presentSettings(dispatch func(func()), present func(bool, string)) {
	welcome := s.SettingsSection() == "setup"
	key := "native.settingsTitle"
	if welcome {
		key = "native.welcomeTitle"
	}
	title := s.text(key, nil)
	dispatch(func() { present(welcome, title) })
}

// SurfaceReady acknowledges the requested renderer layout before native presentation.
func (s *Service) SurfaceReady(surface string, token uint64) {
	if (surface == "chat" || surface == "settings") && s.surfaceReady != nil {
		s.surfaceReady(surface, token)
	}
}
