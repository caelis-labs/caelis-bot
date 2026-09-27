package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
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
	root       string
	shortcuts  [2]ShortcutState
	files      [2]string
	mu         sync.Mutex
	refreshing bool
	sending    map[string]bool
	capability func(context.Context) (api.ImageInputCapability, error)
	submit     func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error)
	snapshot   func() api.Snapshot
}

func (s *Service) configureCaptureBackend(back *backend.Service) {
	s.capture.capability = back.ImageInput
	s.capture.snapshot = func() api.Snapshot { return backend.ScreenSnapshot(back) }
	s.capture.submit = func(ctx context.Context, input api.Submission, files []api.InputFile) (api.Receipt, error) {
		return backend.SubmitScreen(ctx, back, input, files)
	}
}

func (s *Service) configureCapture(root string) error {
	s.capture.root = root
	s.capture.sending = map[string]bool{}
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
	if err := d.registerCaptureShortcut(v, kind); err != nil {
		return old, err
	}
	if err := saveShortcut(s.capture.files[kind], v); err != nil {
		if rollback := d.registerCaptureShortcut(old.Shortcut, kind); rollback != nil {
			disabled := v
			disabled.Enabled = false
			_ = d.registerCaptureShortcut(disabled, kind)
			s.capture.shortcuts[kind].Registered = false
		}
		return s.capture.shortcuts[kind], err
	}
	s.capture.shortcuts[kind] = ShortcutState{Shortcut: v, Registered: v.Enabled}
	return s.capture.shortcuts[kind], nil
}
func (s *Service) CaptureScreen()   { s.captureCommand(0) }
func (s *Service) PasteImage()      { s.captureCommand(1) }
func (s *Service) ToggleImagePins() { s.captureCommand(2) }
func (s *Service) captureCommand(kind int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(captureDriver); ok && !s.stopped {
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
	stopped := s.stopped
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
	files, err := screeninput.Files(s.capture.root, r)
	if err != nil {
		s.failCapturePreflight(id)
		return
	}
	if s.capture.submit == nil {
		s.publishCaptureReceipt(id, "rejected", "native.capture.unavailable")
		return
	}
	r, err = screeninput.Begin(s.capture.root, r)
	if err != nil {
		s.publishCaptureReceipt(id, "rejected", "native.capture.saveFailed")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
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
