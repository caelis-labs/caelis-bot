package desktop

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type recoveryFake struct {
	captureFake
	active   [4]Shortcut
	failures [4]error
	calls    [4]int
	block    func(int, Shortcut)
}

func (d *recoveryFake) register(v Shortcut, slot int) error {
	d.calls[slot]++
	if d.block != nil {
		d.block(slot, v)
	}
	if v.Enabled && d.failures[slot] != nil {
		return d.failures[slot]
	}
	d.active[slot] = v
	return nil
}
func (d *recoveryFake) registerShortcut(v Shortcut) error     { return d.register(v, 0) }
func (d *recoveryFake) toggleTaskDock()                       {}
func (d *recoveryFake) registerTaskShortcut(v Shortcut) error { return d.register(v, 1) }
func (d *recoveryFake) registerCaptureShortcut(v Shortcut, slot int) error {
	return d.register(v, slot+2)
}
func recoveryFixture(t *testing.T, failure error) (*Service, *recoveryFake) {
	t.Helper()
	s := newService(&memoryStore{value: defaults()})
	root := t.TempDir()
	s.configureShortcut(filepath.Join(root, "shortcut.json"))
	s.configureTaskShortcut(filepath.Join(root, "task-shortcut.json"))
	if err := s.configureCapture(filepath.Join(root, "Captures")); err != nil {
		t.Fatal(err)
	}
	d := &recoveryFake{}
	d.displays = []Rect{{0, 0, 1440, 900}}
	d.failures[2] = failure
	s.start(d)
	t.Cleanup(s.shutdown)
	return s, d
}
func TestShortcutRecoveryContinuesAtCappedIntervalAndResets(t *testing.T) {
	s, d := recoveryFixture(t, &shortcutRegistrationError{Code: -9878})
	now := time.Now()
	for i := 0; i < 12; i++ {
		s.recoverShortcuts(now, false)
		state := s.CaptureShortcutSettings()
		if state.Status != "conflict" || state.Registered {
			t.Fatalf("false success: %+v", state)
		}
		next := s.shortcutRetries[2].next
		if delay := next.Sub(now); delay <= 0 || delay > 8*time.Second {
			t.Fatal("unbounded backoff", delay)
		}
		calls := d.calls[2]
		s.recoverShortcuts(next.Add(-time.Nanosecond), false)
		if d.calls[2] != calls {
			t.Fatal("ignored retry deadline")
		}
		now = next
	}
	if d.calls[2] < 13 || d.calls[0] != 1 || d.calls[1] != 1 || d.calls[3] != 1 {
		t.Fatal("wrong recovery scope", d.calls)
	}
	d.failures[2] = nil
	s.recoverShortcuts(now, false)
	if state := s.CaptureShortcutSettings(); !state.Registered || state.Status != "available" || state.Message != "" {
		t.Fatal(state)
	}
	if s.shortcutRetries[2] != (shortcutRetry{}) {
		t.Fatal("success retained failure budget")
	}
	calls := d.calls
	s.recoverShortcuts(now.Add(time.Hour), true)
	if calls != d.calls {
		t.Fatal("recovery re-registered owned shortcuts")
	}
}
func TestShortcutConflictSaveUsesLatestDesiredConfiguration(t *testing.T) {
	s, d := recoveryFixture(t, nil)
	d.failures[2] = &shortcutRegistrationError{Code: -9878}
	next := Shortcut{Enabled: true, Key: "F2"}
	state, err := s.SaveCaptureShortcut(next)
	if err != nil || state.Shortcut != next || state.Registered || state.Status != "conflict" || d.active[2].Enabled {
		t.Fatal("conflicting edit kept old binding", state, err, d.active)
	}
	restarted := newService(&memoryStore{})
	if err := restarted.configureCapture(s.capture.root); err != nil {
		t.Fatal(err)
	}
	if restarted.CaptureShortcutSettings().Shortcut != next {
		t.Fatal("desired edit not persisted")
	}
	d.failures[2] = nil
	s.recoverShortcuts(time.Now(), true)
	if d.active[2] != next || !s.CaptureShortcutSettings().Registered {
		t.Fatal("recovered obsolete binding")
	}
	d.failures[2] = &shortcutRegistrationError{Code: -9878}
	s.capture.files[0] = filepath.Join(s.capture.files[0], "impossible.json")
	other := next
	other.Key = "F4"
	if _, err := s.SaveCaptureShortcut(other); err == nil || s.CaptureShortcutSettings().Shortcut != next || d.active[2] != next {
		t.Fatal("failed write replaced desired or native binding")
	}
}
func TestShortcutRecoveryHonorsDisableRemapGateAndShutdown(t *testing.T) {
	for _, action := range []string{"disable", "remap", "gate", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			s, d := recoveryFixture(t, &shortcutRegistrationError{Code: -9878})
			switch action {
			case "disable":
				if _, err := s.SaveCaptureShortcut(Shortcut{Key: "F1"}); err != nil {
					t.Fatal(err)
				}
			case "remap":
				if _, err := s.SaveCaptureShortcut(Shortcut{Enabled: true, Key: "F2"}); err != nil {
					t.Fatal(err)
				}
			case "gate":
				if _, err := s.SetCaptureEnabled(false); err != nil {
					t.Fatal(err)
				}
			case "shutdown":
				s.shutdown()
			}
			d.failures[2] = nil
			before := d.calls
			s.recoverShortcuts(time.Now(), true)
			if action == "remap" {
				if d.active[2].Key != "F2" || !d.active[2].Enabled {
					t.Fatal("remap lost")
				}
			} else if d.calls != before {
				t.Fatal("disabled shortcut resurrected", action, d.calls, before)
			}
		})
	}
}
func TestCaptureFeatureCanEnableWhileShortcutConflicts(t *testing.T) {
	s, d := recoveryFixture(t, &shortcutRegistrationError{Code: -9878})
	if _, err := s.SetCaptureEnabled(false); err != nil {
		t.Fatal(err)
	}
	if preferences, err := s.SetCaptureEnabled(true); err != nil || !preferences.Enabled {
		t.Fatal("key conflict disabled capture menus", preferences, err)
	}
	if s.CaptureShortcutSettings().Status != "conflict" || !s.PasteShortcutSettings().Registered {
		t.Fatal("shortcut isolation lost")
	}
	d.failures[2] = nil
	s.recoverShortcuts(time.Now(), true)
	if !s.CaptureShortcutSettings().Registered {
		t.Fatal("enabled feature did not recover")
	}
}
func TestShortcutRecoverySerializesWithDisable(t *testing.T) {
	s, d := recoveryFixture(t, &shortcutRegistrationError{Code: -9878})
	entered, release := make(chan struct{}), make(chan struct{})
	d.failures[2] = nil
	d.block = func(slot int, v Shortcut) {
		if slot == 2 && v.Enabled {
			close(entered)
			<-release
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.recoverShortcuts(time.Now(), true) }()
	<-entered
	result := make(chan error, 1)
	go func() { defer wg.Done(); _, err := s.SaveCaptureShortcut(Shortcut{Key: "F1"}); result <- err }()
	close(release)
	wg.Wait()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	s.recoverShortcuts(time.Now().Add(time.Hour), true)
	if d.active[2].Enabled || s.CaptureShortcutSettings().Registered {
		t.Fatal("late recovery re-enabled shortcut")
	}
}
func TestShortcutRegistrationFailureIsNotAlwaysConflict(t *testing.T) {
	s, _ := recoveryFixture(t, &shortcutRegistrationError{Code: -50})
	if state := s.CaptureShortcutSettings(); state.Status != "unavailable" || state.Registered {
		t.Fatal(state)
	}
}

type scheduledRecoveryFake struct{ recoveryFake }

func (d *scheduledRecoveryFake) shortcutRecoveryInterval() time.Duration { return 5 * time.Millisecond }
func TestShortcutRecoveryRunsWithoutReadingOrSavingSettings(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	if err := s.configureCapture(filepath.Join(t.TempDir(), "Captures")); err != nil {
		t.Fatal(err)
	}
	d := &scheduledRecoveryFake{}
	d.displays = []Rect{{0, 0, 1440, 900}}
	d.failures[2] = &shortcutRegistrationError{Code: -9878}
	s.start(d)
	defer s.shutdown()
	s.mu.Lock()
	d.failures[2] = nil
	s.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.CaptureShortcutSettings().Registered {
			s.shutdown()
			select {
			case <-s.shortcutRecoveryStop:
				return
			default:
				t.Fatal("recovery timer not stopped")
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no automatic registration after owner release")
}

func TestShortcutRecoveryRetainsConfigurationReadWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shortcut.json")
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	s := newService(&memoryStore{value: defaults()})
	s.configureShortcut(path)
	warning := s.ShortcutSettings().Message
	if warning == "" {
		t.Fatal("missing configuration warning")
	}
	d := &recoveryFake{}
	d.displays = []Rect{{0, 0, 1440, 900}}
	s.start(d)
	defer s.shutdown()
	if state := s.ShortcutSettings(); !state.Registered || state.Message != warning {
		t.Fatal("fallback registration erased config read failure", state)
	}
	if state, err := s.SaveShortcut(defaultShortcut()); err != nil || state.Message != "" {
		t.Fatal("explicit repair did not clear config warning", state, err)
	}
}
