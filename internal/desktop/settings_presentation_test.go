package desktop

import "testing"

func TestSettingsPresentationDoesNotReadServiceStateOnUIThread(t *testing.T) {
	for _, section := range []string{"general", "setup"} {
		t.Run(section, func(t *testing.T) {
			s, _, _ := setup()
			s.settingsSection = section
			shown := false
			s.presentSettings(func(present func()) {
				// A native update can own the service lock while queued behind this UI
				// callback. Presentation must finish without acquiring that lock.
				s.mu.Lock()
				defer s.mu.Unlock()
				present()
			}, func(welcome bool, title, destination string) {
				if welcome != (section == "setup") || title == "" || destination != section {
					t.Fatalf("invalid presentation: %v %q", welcome, title)
				}
				shown = true
			})
			if !shown {
				t.Fatal("settings were not presented")
			}
		})
	}
}

func TestOnboardingGuardsAllContextualEntryPoints(t *testing.T) {
	s, _, _ := setup()
	s.started = true
	required := true
	s.needsIntroduction = func() bool { return required }
	chats, settings := 0, 0
	s.openHistory = func() { chats++ }
	s.recallWindows = func() { chats++ }
	s.openSettings = func() { settings++ }
	for _, open := range []func(){s.OpenHistory, s.OpenSettings, s.OpenRuntimeSettings, s.RecallWindows} {
		open()
		if s.SettingsSection() != "setup" || chats != 0 {
			t.Fatal("setup bypassed", s.SettingsSection(), chats)
		}
	}
	if settings != 4 {
		t.Fatal("onboarding not recalled", settings)
	}
	required = false
	s.OpenHistory()
	s.OpenSettings()
	if chats != 1 || s.SettingsSection() != "general" {
		t.Fatal("completed introduction still blocked")
	}
}

func TestRepeatedSettingsOpenOnlyFocusesExistingWindow(t *testing.T) {
	s, _, _ := setup()
	s.started = true
	s.settingsSection = "runtime"
	focused, opened := 0, 0
	s.settingsVisible = func() bool { return true }
	s.focusSettings = func() { focused++ }
	s.openSettings = func() { opened++ }
	s.OpenSettings()
	s.OpenSettings()
	if focused != 2 || opened != 0 || s.SettingsSection() != "runtime" {
		t.Fatal(focused, opened, s.SettingsSection())
	}
	s.OpenRuntimeSettings()
	if opened != 1 {
		t.Fatal("explicit destination lost")
	}
}
