package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/screeninput"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type captureDriver interface {
	registerCaptureShortcut(Shortcut, int) error
	captureCommand(int)
	captureAvailability(any)
	captureReceipt(any)
	restoreCaptures([]screeninput.Record)
}
type captureState struct {
	preferences    CapturePreferences
	preferenceFile string
	imageBytes     func(string) ([]byte, error)
	root           string
	shortcuts      [2]ShortcutState
	files          [2]string
	mu             sync.Mutex
	refreshing     bool
	sending        map[string]bool
	active         map[string]context.CancelFunc // admitted sends; guarded by Service.mu
	capability     func(context.Context) (api.ImageInputCapability, error)
	submit         func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error)
	snapshot       func() api.Snapshot
}

func (s *Service) configureCaptureBackend(back *backend.Service) {
	s.capture.imageBytes = func(id string) ([]byte, error) { return backend.ScreenImageBytes(back, id) }
	s.capture.capability = back.ImageInput
	s.capture.snapshot = func() api.Snapshot { return backend.ScreenSnapshot(back) }
	s.capture.submit = func(ctx context.Context, input api.Submission, files []api.InputFile) (api.Receipt, error) {
		return backend.SubmitScreen(ctx, back, input, files)
	}
}

func (s *Service) configureCapture(root string) error {
	s.capture.root = root
	s.capture.preferenceFile = filepath.Join(filepath.Dir(root), "screen-input.json")
	s.capture.preferences = CapturePreferences{Enabled: true, IncludeBackground: true}
	if data, err := os.ReadFile(s.capture.preferenceFile); err == nil {
		if json.Unmarshal(data, &s.capture.preferences) != nil {
			s.capture.preferences = CapturePreferences{Notice: "unreadable"}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		s.capture.preferences = CapturePreferences{Notice: "unreadable"}
	}
	s.capture.sending = map[string]bool{}
	s.capture.active = map[string]context.CancelFunc{}
	for i, key := range []string{"F1", "F3"} {
		s.capture.files[i] = filepath.Join(filepath.Dir(root), []string{"capture-shortcut.json", "paste-shortcut.json"}[i])
		v := Shortcut{Enabled: true, Key: key}
		if data, err := os.ReadFile(s.capture.files[i]); err == nil {
			var saved Shortcut
			if json.Unmarshal(data, &saved) == nil && validateCaptureShortcut(saved) == nil {
				v = saved
			}
		}
		s.capture.shortcuts[i].Shortcut = v
	}
	return os.MkdirAll(root, 0700)
}

type CapturePreferences struct {
	Enabled           bool   `json:"enabled"`
	IncludeBackground bool   `json:"includeBackground"`
	Notice            string `json:"notice"`
}
type capturePreferencesDriver interface {
	capturePreferences(bool)
	copyCaptureImage([]byte) bool
}
type captureEnabledDriver interface{ captureEnabled(bool) }

func (s *Service) CapturePreferences() CapturePreferences {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capture.preferences
}
func (s *Service) SaveCapturePreferences(v CapturePreferences) (CapturePreferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v.Notice = ""
	v.Enabled = s.capture.preferences.Enabled // Context cannot change the feature gate.
	if err := localstate.Write(s.capture.preferenceFile, v); err != nil {
		return s.capture.preferences, err
	}
	s.capture.preferences = v
	if d, ok := s.native.(capturePreferencesDriver); ok && !s.stopped {
		d.capturePreferences(v.IncludeBackground)
	}
	return v, nil
}

// SetCaptureEnabled owns the complete manual capture/pin tool. It does not change
// ordinary chat attachments or Desktop World task capture.
func (s *Service) SetCaptureEnabled(enabled bool) (CapturePreferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.capture.preferences
	if old.Enabled == enabled {
		if err := localstate.Write(s.capture.preferenceFile, old); err != nil {
			return old, err
		}
		return old, nil
	}
	d, ok := s.native.(captureDriver)
	if !ok || !s.started || s.stopped {
		return old, errors.New(s.text("native.shortcutUnavailable", nil))
	}
	registered := [2]bool{s.capture.shortcuts[0].Registered, s.capture.shortcuts[1].Registered}
	for i := range s.capture.shortcuts {
		v := s.capture.shortcuts[i].Shortcut
		v.Enabled = enabled && v.Enabled
		if v.Enabled && s.captureShortcutConflict(v, i+2) {
			s.rollbackCaptureRegistration(d, registered)
			return old, errors.New(s.text("native.shortcutConflict", nil))
		}
		if err := d.registerCaptureShortcut(v, i); err != nil {
			s.rollbackCaptureRegistration(d, registered)
			return old, err
		}
		s.capture.shortcuts[i].Registered = v.Enabled
	}
	next := old
	next.Enabled = enabled
	next.Notice = ""
	if err := localstate.Write(s.capture.preferenceFile, next); err != nil {
		s.rollbackCaptureRegistration(d, registered)
		return old, err
	}
	s.capture.preferences = next
	if !enabled {
		for _, cancel := range s.capture.active {
			cancel()
		}
	}
	if gate, ok := s.native.(captureEnabledDriver); ok {
		gate.captureEnabled(enabled)
	}
	if enabled {
		d.restoreCaptures(screeninput.Pending(s.capture.root))
	}
	return next, nil
}
func (s *Service) rollbackCaptureRegistration(d captureDriver, registered [2]bool) {
	for i := range s.capture.shortcuts {
		v := s.capture.shortcuts[i].Shortcut
		v.Enabled = registered[i]
		if err := d.registerCaptureShortcut(v, i); err != nil {
			v.Enabled = false
			_ = d.registerCaptureShortcut(v, i)
			s.capture.shortcuts[i].Registered = false
		} else {
			s.capture.shortcuts[i].Registered = registered[i]
		}
	}
}
func (s *Service) CopyScreenImage(id string) error {
	s.mu.Lock()
	read := s.capture.imageBytes
	s.mu.Unlock()
	if read == nil {
		return errors.New("screen image unavailable")
	}
	data, err := read(id)
	if err != nil {
		return errors.New("screen image unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(capturePreferencesDriver); ok && !s.stopped && d.copyCaptureImage(data) {
		return nil
	}
	return errors.New("could not copy screen image")
}
func validateCaptureShortcut(v Shortcut) error {
	if !shortcutKey.MatchString(v.Key) || (!v.Control && !v.Alt && !v.Meta && v.Key[0] != 'F') {
		return errors.New("use a function key or a key with Control, Option or Command")
	}
	return nil
}
func sameShortcut(a, b Shortcut) bool {
	return a.Key == b.Key && a.Control == b.Control && a.Alt == b.Alt && a.Shift == b.Shift && a.Meta == b.Meta
}
func (s *Service) captureShortcutConflict(v Shortcut, exclude int) bool {
	if !v.Enabled {
		return false
	}
	all := []ShortcutState{s.shortcut, s.taskShortcut, s.capture.shortcuts[0], s.capture.shortcuts[1]}
	for i, state := range all {
		if i != exclude && state.Registered && state.Shortcut.Enabled && sameShortcut(state.Shortcut, v) {
			return true
		}
	}
	return false
}
func (s *Service) CaptureShortcutSettings() ShortcutState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capture.shortcuts[0]
}
func (s *Service) PasteShortcutSettings() ShortcutState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capture.shortcuts[1]
}
func (s *Service) SaveCaptureShortcut(v Shortcut) (ShortcutState, error) {
	return s.saveCaptureShortcut(v, 0)
}
func (s *Service) SavePasteShortcut(v Shortcut) (ShortcutState, error) {
	return s.saveCaptureShortcut(v, 1)
}
func (s *Service) saveCaptureShortcut(v Shortcut, kind int) (ShortcutState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.capture.shortcuts[kind]
	if err := validateCaptureShortcut(v); err != nil {
		return old, err
	}
	d, ok := s.native.(captureDriver)
	if !ok || s.stopped {
		return old, errors.New(s.text("native.shortcutUnavailable", nil))
	}
	if s.captureShortcutConflict(v, kind+2) {
		return old, errors.New(s.text("native.shortcutConflict", nil))
	}
	active := v
	active.Enabled = v.Enabled && s.capture.preferences.Enabled
	if err := d.registerCaptureShortcut(active, kind); err != nil {
		return old, err
	}
	if err := saveShortcut(s.capture.files[kind], v); err != nil {
		previous := old.Shortcut
		previous.Enabled = old.Registered
		if rollback := d.registerCaptureShortcut(previous, kind); rollback != nil {
			disabled := v
			disabled.Enabled = false
			_ = d.registerCaptureShortcut(disabled, kind)
			s.capture.shortcuts[kind].Registered = false
		}
		return s.capture.shortcuts[kind], err
	}
	s.capture.shortcuts[kind] = ShortcutState{Shortcut: v, Registered: active.Enabled}
	return s.capture.shortcuts[kind], nil
}
func (s *Service) CaptureScreen()   { s.captureCommand(0) }
func (s *Service) PasteImage()      { s.captureCommand(1) }
func (s *Service) ToggleImagePins() { s.captureCommand(2) }
func (s *Service) captureCommand(kind int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(captureDriver); ok && !s.stopped && s.capture.preferences.Enabled {
		d.captureCommand(kind)
	}
}
func (s *Service) refreshCapture() {
	s.capture.mu.Lock()
	if s.capture.refreshing {
		s.capture.mu.Unlock()
		return
	}
	s.capture.refreshing = true
	s.capture.mu.Unlock()
	defer func() { s.capture.mu.Lock(); s.capture.refreshing = false; s.capture.mu.Unlock() }()
	state := api.ImageInputCapability{State: "unknown"}
	if s.capture.capability != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if value, err := s.capture.capability(ctx); err == nil {
			state = value
		}
		cancel()
	}
	message := s.text("native.capture.imageUnknown", nil)
	if state.State == "unsupported" {
		message = s.text("native.capture.imageUnsupported", nil)
	}
	s.mu.Lock()
	if d, ok := s.native.(captureDriver); ok && !s.stopped {
		d.captureAvailability(map[string]string{"state": state.State, "message": message})
	}
	s.mu.Unlock()
	// Observe existing request identities only. Never dispatch from a timer.
	if s.capture.snapshot == nil {
		return
	}
	snapshot := s.capture.snapshot()
	for _, r := range screeninput.Pending(s.capture.root) {
		if r.Outcome != "unknown" {
			continue
		}
		outcome := ""
		if snapshot.LastReceipt.ID == r.RequestID {
			outcome = snapshot.LastReceipt.Outcome
		}
		for _, item := range snapshot.Items {
			if item.Kind == "user" && item.RequestID == r.RequestID {
				outcome = "accepted"
				break
			}
		}
		if outcome == "accepted" || outcome == "rejected" {
			s.finishCapture(r, outcome)
		}
	}
}
func (s *Service) sendCapture(id string) {
	s.mu.Lock()
	stopped := s.stopped || !s.capture.preferences.Enabled
	s.mu.Unlock()
	if stopped {
		return
	}
	s.capture.mu.Lock()
	if s.capture.sending[id] {
		s.capture.mu.Unlock()
		return
	}
	s.capture.sending[id] = true
	s.capture.mu.Unlock()
	defer func() {
		s.capture.mu.Lock()
		delete(s.capture.sending, id)
		r, err := screeninput.Load(s.capture.root, id)
		accepted := err == nil && r.Outcome == "accepted"
		s.capture.mu.Unlock()
		if accepted {
			s.discardCapture(id)
		}
	}()
	r, err := screeninput.Load(s.capture.root, id)
	if err != nil {
		s.failCapturePreflight(id)
		return
	}
	if r.Outcome == "unknown" || r.Outcome == "accepted" {
		s.finishCapture(r, r.Outcome)
		return
	}
	s.mu.Lock()
	active := !s.stopped && s.capture.preferences.Enabled
	s.mu.Unlock()
	if !active {
		s.publishCaptureReceipt(id, "draft", "native.capture.unavailable")
		return
	}
	files, err := screeninput.Files(s.capture.root, r)
	if err != nil {
		s.failCapturePreflight(id)
		return
	}
	if s.capture.submit == nil {
		s.publishCaptureReceipt(id, "rejected", "native.capture.unavailable")
		return
	}
	s.mu.Lock()
	if s.stopped || !s.capture.preferences.Enabled {
		s.mu.Unlock()
		s.publishCaptureReceipt(id, "draft", "native.capture.unavailable")
		return
	}
	r, err = screeninput.Begin(s.capture.root, r)
	if err != nil {
		s.mu.Unlock()
		s.publishCaptureReceipt(id, "rejected", "native.capture.saveFailed")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	s.capture.active[id] = cancel
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.capture.active, id); s.mu.Unlock(); cancel() }()
	receipt, _ := s.capture.submit(ctx, api.Submission{ID: r.RequestID, Text: screeninput.Prompt(r.Snapshot), ScreenInput: true}, files)
	outcome := receipt.Outcome
	if outcome != "accepted" && outcome != "rejected" {
		outcome = "unknown"
	}
	s.finishCapture(r, outcome)
}

func (s *Service) failCapturePreflight(id string) {
	s.capture.mu.Lock()
	err := screeninput.DiscardUnsubmitted(s.capture.root, id)
	s.capture.mu.Unlock()
	if err == nil {
		// No dispatch receipt existed. The native document can be edited and
		// exported again, even if the invalid on-disk metadata could not load.
		s.publishCaptureReceipt(id, "draft", "native.capture.invalidInput")
	} else {
		// An unreadable receipt must not become permission to resend.
		s.publishCaptureReceipt(id, "unknown", "native.capture.saveFailed")
	}
}
func (s *Service) finishCapture(r screeninput.Record, outcome string) {
	s.capture.mu.Lock()
	current, err := screeninput.Load(s.capture.root, r.Snapshot.ID)
	if err != nil || current.RequestID != r.RequestID {
		s.capture.mu.Unlock()
		return
	}
	// Reconciliation can confirm delivery before the original call returns.
	// A late timeout must never downgrade that confirmation or a newer retry.
	if current.Outcome == "accepted" || current.Outcome == "rejected" && outcome == "unknown" {
		outcome = current.Outcome
	}
	r.Outcome = outcome
	if err := screeninput.SaveReceipt(s.capture.root, r); err != nil {
		outcome = "unknown"
	}
	s.capture.mu.Unlock()
	key := map[string]string{"accepted": "native.capture.accepted", "rejected": "native.capture.rejected", "unknown": "native.capture.unknown"}[outcome]
	s.publishCaptureReceipt(r.Snapshot.ID, outcome, key)
	if outcome == "accepted" {
		s.discardCapture(r.Snapshot.ID)
	}
}
func (s *Service) publishCaptureReceipt(id, outcome, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(captureDriver); ok && !s.stopped {
		d.captureReceipt(map[string]string{"id": id, "outcome": outcome, "message": s.text(key, nil)})
	}
}
func (s *Service) discardCapture(id string) {
	s.capture.mu.Lock()
	defer s.capture.mu.Unlock()
	if s.capture.sending[id] {
		return
	}
	r, err := screeninput.Load(s.capture.root, id)
	if err != nil {
		_ = screeninput.DiscardUnsubmitted(s.capture.root, id)
		return
	}
	if r.Outcome == "unknown" {
		return
	}
	if dir, err := screeninput.Directory(s.capture.root, id); err == nil {
		_ = os.RemoveAll(dir)
	}
}
