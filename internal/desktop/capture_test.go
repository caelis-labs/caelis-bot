package desktop

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/screeninput"
)

type captureFake struct {
	shortcutFake
	keys         [2]Shortcut
	receipts     []map[string]string
	restored     []screeninput.Record
	availability string
}

func (d *captureFake) registerCaptureShortcut(v Shortcut, k int) error { d.keys[k] = v; return nil }
func (d *captureFake) captureCommand(int)                              {}
func (d *captureFake) captureAvailability(v any)                       { d.availability = v.(map[string]string)["state"] }
func (d *captureFake) captureReceipt(v any)                            { d.receipts = append(d.receipts, v.(map[string]string)) }
func (d *captureFake) restoreCaptures(v []screeninput.Record)          { d.restored = v }
func captureFixture(t *testing.T) (*Service, *captureFake, string) {
	t.Helper()
	s := newService(&memoryStore{value: defaults()})
	root := filepath.Join(t.TempDir(), "Captures")
	if err := s.configureCapture(root); err != nil {
		t.Fatal(err)
	}
	d := &captureFake{shortcutFake: shortcutFake{fakeDriver: fakeDriver{displays: []Rect{{0, 0, 1440, 900}}}}}
	s.start(d)
	id := "capture-fixture-1"
	dir := filepath.Join(root, id)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(screeninput.Snapshot{ID: id, Version: 1, Source: "clipboard"})
	if err := os.WriteFile(filepath.Join(dir, "capture.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "selection.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return s, d, id
}
func TestCaptureDefaultsAndShortcutCollisions(t *testing.T) {
	s, d, _ := captureFixture(t)
	if d.keys[0].Key != "F1" || d.keys[1].Key != "F3" {
		t.Fatal("wrong screenshot defaults")
	}
	if _, err := s.SavePasteShortcut(d.keys[0]); err == nil {
		t.Fatal("paste displaced screenshot shortcut")
	}
	for _, v := range []Shortcut{{Key: "KeyA"}, {Key: "KeyA", Shift: true}, {Key: ""}, {Key: "F13"}} {
		if validateCaptureShortcut(v) == nil {
			t.Fatal("unsafe shortcut", v)
		}
	}
	if validateCaptureShortcut(Shortcut{Key: "F1"}) != nil {
		t.Fatal("function key rejected")
	}
	old := d.keys[0]
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, nil, 0600)
	s.capture.files[0] = filepath.Join(blocker, "settings.json")
	if _, err := s.SaveCaptureShortcut(Shortcut{Enabled: true, Key: "F2"}); err == nil || d.keys[0] != old {
		t.Fatal("failed preference save displaced shortcut")
	}
}
func TestCaptureUnknownNeverResendsAndReconcilesExactIdentity(t *testing.T) {
	s, d, id := captureFixture(t)
	calls := 0
	request := ""
	s.capture.capability = func(context.Context) (api.ImageInputCapability, error) {
		return api.ImageInputCapability{State: "supported"}, nil
	}
	s.capture.submit = func(_ context.Context, in api.Submission, files []api.InputFile) (api.Receipt, error) {
		calls++
		request = in.ID
		if !in.ScreenInput || len(files) != 1 {
			t.Fatal("wrong capture envelope")
		}
		return api.Receipt{ID: in.ID, Outcome: "unknown"}, nil
	}
	s.sendCapture(id)
	s.sendCapture(id)
	if calls != 1 {
		t.Fatal("uncertain input replayed")
	}
	s.discardCapture(id)
	if _, err := screeninput.Load(s.capture.root, id); err != nil {
		t.Fatal("uncertainty discarded")
	}
	s.capture.snapshot = func() api.Snapshot {
		return api.Snapshot{LastReceipt: api.Receipt{ID: "unrelated", Outcome: "accepted"}}
	}
	s.refreshCapture()
	if len(screeninput.Pending(s.capture.root)) != 1 || d.availability != "supported" {
		t.Fatal("unrelated receipt consumed capture")
	}
	s.capture.snapshot = func() api.Snapshot { return api.Snapshot{Items: []api.Item{{Kind: "user", RequestID: request}}} }
	s.refreshCapture()
	if _, err := os.Stat(filepath.Join(s.capture.root, id)); !os.IsNotExist(err) {
		t.Fatal("confirmed capture not cleaned", err)
	}
	if d.receipts[len(d.receipts)-1]["outcome"] != "accepted" {
		t.Fatal("late acceptance not presented")
	}
}
func TestCaptureLateTimeoutCannotDowngradeAcceptance(t *testing.T) {
	s, d, id := captureFixture(t)
	s.capture.submit = func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		r, err := screeninput.Load(s.capture.root, id)
		if err != nil || r.RequestID != in.ID {
			t.Fatal("dispatch without durable receipt", err)
		}
		s.finishCapture(r, "accepted") // Native event arrives before call timeout.
		return api.Receipt{ID: in.ID, Outcome: "unknown"}, nil
	}
	s.sendCapture(id)
	if _, err := os.Stat(filepath.Join(s.capture.root, id)); !os.IsNotExist(err) {
		t.Fatal("sender left confirmed image files", err)
	}
	for _, r := range d.receipts {
		if r["outcome"] != "accepted" {
			t.Fatal("late timeout downgraded confirmation")
		}
	}
}
func TestCaptureRejectedRetryAndRestartRetainImages(t *testing.T) {
	s, _, id := captureFixture(t)
	ids := []string{}
	s.capture.submit = func(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		ids = append(ids, in.ID)
		return api.Receipt{ID: in.ID, Outcome: "rejected"}, nil
	}
	s.sendCapture(id)
	if len(screeninput.Pending(s.capture.root)) != 1 {
		t.Fatal("rejected draft lost")
	}
	next := newService(&memoryStore{value: defaults()})
	if err := next.configureCapture(s.capture.root); err != nil {
		t.Fatal(err)
	}
	d := &captureFake{shortcutFake: shortcutFake{fakeDriver: fakeDriver{displays: []Rect{{0, 0, 1440, 900}}}}}
	next.start(d)
	if len(d.restored) != 1 || d.restored[0].Outcome != "rejected" || len(ids) != 1 {
		t.Fatal("restore sent or lost input")
	}
	s.sendCapture(id)
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatal("explicit retry reused rejected identity")
	}
	s.discardCapture(id)
	if _, err := os.Stat(filepath.Join(s.capture.root, id)); !os.IsNotExist(err) {
		t.Fatal("discard failed")
	}
}

func TestCaptureStorageFailureDoesNotRemoveLocalShortcuts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(root, nil, 0600); err != nil {
		t.Fatal(err)
	}
	s := newService(&memoryStore{value: defaults()})
	if err := s.configureCapture(root); err == nil {
		t.Fatal("expected capture storage failure")
	}
	if s.capture.shortcuts[0].Shortcut.Key != "F1" || s.capture.shortcuts[1].Shortcut.Key != "F3" {
		t.Fatal("Ask Bot storage failure disabled capture/clipboard")
	}
}
