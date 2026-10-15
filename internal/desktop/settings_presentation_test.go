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
			}, func(welcome bool, title string) {
				if welcome != (section == "setup") || title == "" {
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
