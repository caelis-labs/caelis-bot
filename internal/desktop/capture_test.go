package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/screeninput"
)

type captureFake struct {
	shortcutFake
	keys         [2]Shortcut
	receipts     []map[string]string
	restored     []screeninput.Record
	availability string
	commands     []int
	enabled      bool
	failKind     int
}

func (d *captureFake) registerCaptureShortcut(v Shortcut, k int) error {
	if d.failKind == k+1 {
		d.failKind = 0
		return errors.New("registration failed")
	}
	d.keys[k] = v
	return nil
}
func (d *captureFake) captureCommand(k int)                   { d.commands = append(d.commands, k) }
func (d *captureFake) captureEnabled(v bool)                  { d.enabled = v }
func (d *captureFake) captureAvailability(v any)              { d.availability = v.(map[string]string)["state"] }
func (d *captureFake) captureReceipt(v any)                   { d.receipts = append(d.receipts, v.(map[string]string)) }
func (d *captureFake) restoreCaptures(v []screeninput.Record) { d.restored = v }
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
func TestCaptureFeatureGatePersistsAndRetainsHistory(t *testing.T) {
	s, d, id := captureFixture(t)
	if !s.CapturePreferences().Enabled {
		t.Fatal("new install should be enabled")
	}
	s.CaptureScreen()
	s.PasteImage()
	s.ToggleImagePins()
	if len(d.commands) != 3 {
		t.Fatal("enabled commands missing", d.commands)
	}
	if _, err := s.SetCaptureEnabled(false); err != nil {
		t.Fatal(err)
	}
	if d.enabled || d.keys[0].Enabled || d.keys[1].Enabled || s.CapturePreferences().Enabled {
		t.Fatal("native capture still enabled")
	}
	s.CaptureScreen()
	s.PasteImage()
	s.ToggleImagePins()
	if len(d.commands) != 3 {
		t.Fatal("disabled command reached native")
	}
	s.capture.submit = func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
		t.Fatal("disabled capture dispatched")
		return api.Receipt{}, nil
	}
	s.sendCapture(id)
	if _, err := os.Stat(filepath.Join(s.capture.root, id)); err != nil {
		t.Fatal("unsent document removed", err)
	}
	if _, err := s.SaveCaptureShortcut(Shortcut{Enabled: true, Key: "F2"}); err != nil {
		t.Fatal(err)
	}
	if d.keys[0].Enabled {
		t.Fatal("shortcut reactivated through editor")
	}
	restarted := newService(&memoryStore{value: defaults()})
	if err := restarted.configureCapture(s.capture.root); err != nil {
		t.Fatal(err)
	}
	next := &captureFake{shortcutFake: shortcutFake{fakeDriver: fakeDriver{displays: []Rect{{0, 0, 1440, 900}}}}}
	restarted.start(next)
	if restarted.CapturePreferences().Enabled || next.keys[0].Enabled || next.keys[1].Enabled {
		t.Fatal("disabled restart restored entry")
	}
	if _, err := restarted.SetCaptureEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !next.enabled || !next.keys[0].Enabled || !next.keys[1].Enabled || next.keys[0].Key != "F2" {
		t.Fatal("reopen did not restore saved shortcuts", next.keys)
	}
}
func TestCaptureEnableFailureRollsBack(t *testing.T) {
	s, d, _ := captureFixture(t)
	if _, err := s.SetCaptureEnabled(false); err != nil {
		t.Fatal(err)
	}
	d.failKind = 2
	if _, err := s.SetCaptureEnabled(true); err == nil {
		t.Fatal("expected native registration failure")
	}
	if s.CapturePreferences().Enabled || d.keys[0].Enabled || d.keys[1].Enabled {
		t.Fatal("failed enable leaked registration")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	s.capture.preferenceFile = filepath.Join(blocker, "settings.json")
	if _, err := s.SetCaptureEnabled(true); err == nil {
		t.Fatal("expected durable save failure")
	}
	if s.CapturePreferences().Enabled || d.keys[0].Enabled || d.keys[1].Enabled {
		t.Fatal("failed save enabled capture")
	}
}
func TestCaptureDisableCancelsAdmittedSendWithoutRetry(t *testing.T) {
	s, _, id := captureFixture(t)
	s.capture.capability = func(context.Context) (api.ImageInputCapability, error) {
		return api.ImageInputCapability{State: "supported"}, nil
	}
	entered := make(chan struct{})
	done := make(chan struct{})
	calls := 0
	s.capture.submit = func(ctx context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
		calls++
		close(entered)
		<-ctx.Done()
		return api.Receipt{ID: in.ID, Outcome: "unknown"}, ctx.Err()
	}
	go func() { s.sendCapture(id); close(done) }()
	<-entered
	if _, err := s.SetCaptureEnabled(false); err != nil {
		t.Fatal(err)
	}
	<-done
	r, err := screeninput.Load(s.capture.root, id)
	if err != nil || r.Outcome != "unknown" || r.RequestID == "" {
		t.Fatal("cancelled remote write lost its uncertain receipt", r, err)
	}
	s.sendCapture(id)
	if calls != 1 {
		t.Fatal("disabled send replayed uncertain write", calls)
	}
}
func TestCaptureLegacyPreferenceRetainsEnablement(t *testing.T) {
	s, _, _ := captureFixture(t)
	if err := os.WriteFile(s.capture.preferenceFile, []byte(`{"includeBackground":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	restored := newService(&memoryStore{value: defaults()})
	if err := restored.configureCapture(s.capture.root); err != nil {
		t.Fatal(err)
	}
	if !restored.CapturePreferences().Enabled || restored.CapturePreferences().IncludeBackground {
		t.Fatal("legacy preference changed", restored.CapturePreferences())
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

// Exercise the same backend wiring as the native product, including its outbox.
type captureRuntime struct {
	api.Engine
	snapshot api.Snapshot
	submit   func(api.Submission) api.Receipt
}

func (e *captureRuntime) Snapshot() api.Snapshot { return e.snapshot }
func (e *captureRuntime) ImageInput(context.Context) (api.ImageInputCapability, error) {
	return api.ImageInputCapability{State: "supported"}, nil
}
func (e *captureRuntime) Submit(_ context.Context, in api.Submission, _ []api.InputFile) (api.Receipt, error) {
	return e.submit(in), nil
}
func TestCaptureReconciliationIgnoresBackendOutbox(t *testing.T) {
	for _, outcome := range []string{"unknown", "rejected"} {
		t.Run(outcome, func(t *testing.T) {
			s, d, id := captureFixture(t)
			engine := &captureRuntime{}
			back := backend.NewService(engine, nil, nil, nil, nil)
			s.configureCaptureBackend(back)
			var request string
			checkKept := func(want string) {
				t.Helper()
				r, err := screeninput.Load(s.capture.root, id)
				if err != nil || r.Outcome != want {
					t.Fatalf("capture lost authority boundary: %+v %v, want %s", r, err, want)
				}
				for _, receipt := range d.receipts {
					if receipt["outcome"] == "accepted" {
						t.Fatal("optimistic item closed capture")
					}
				}
			}
			engine.submit = func(in api.Submission) api.Receipt {
				request = in.ID
				projected := back.Snapshot()
				if len(projected.Items) != 1 || projected.Items[0].ID != "outgoing:"+in.ID || projected.Items[0].Status != "sending" {
					t.Fatal("fixture missed real staged outbox", projected.Items)
				}
				s.refreshCapture()
				checkKept("unknown")
				return api.Receipt{ID: in.ID, Outcome: outcome}
			}
			s.sendCapture(id)
			checkKept(outcome)
			s.refreshCapture()
			checkKept(outcome)
			if outcome == "unknown" {
				engine.snapshot.Items = []api.Item{{ID: "native-input", Kind: "user", RequestID: request}}
				s.refreshCapture()
				if _, err := os.Stat(filepath.Join(s.capture.root, id)); !os.IsNotExist(err) {
					t.Fatal("native acceptance did not clean capture", err)
				}
				if d.receipts[len(d.receipts)-1]["outcome"] != "accepted" {
					t.Fatal("native acceptance not presented")
				}
			}
		})
	}
}

func TestCaptureInvalidExportReturnsEditableDraft(t *testing.T) {
	for _, invalid := range []string{"note", "image"} {
		for _, receipt := range []string{"", "unknown", "corrupt"} {
			t.Run(invalid+"/"+receipt, func(t *testing.T) {
				s, d, id := captureFixture(t)
				dir := filepath.Join(s.capture.root, id)
				if receipt != "" {
					data := []byte(`{"requestId":"screen-uncertain","outcome":"unknown"}`)
					if receipt == "corrupt" {
						data = []byte("corrupt")
					}
					if err := os.WriteFile(filepath.Join(dir, "receipt.json"), data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if invalid == "note" {
					data, _ := json.Marshal(screeninput.Snapshot{Version: 1, ID: id, Source: "clipboard", Note: strings.Repeat("汉", 1366)})
					if err := os.WriteFile(filepath.Join(dir, "capture.json"), data, 0600); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Remove(filepath.Join(dir, "selection.png")); err != nil {
					t.Fatal(err)
				}
				s.capture.submit = func(context.Context, api.Submission, []api.InputFile) (api.Receipt, error) {
					t.Fatal("invalid or uncertain capture dispatched")
					return api.Receipt{}, nil
				}
				s.sendCapture(id)
				want := "draft"
				if receipt != "" {
					want = "unknown"
				}
				if len(d.receipts) != 1 || d.receipts[0]["outcome"] != want {
					t.Fatalf("receipt = %v, want %s", d.receipts, want)
				}
				s.discardCapture(id)
				_, err := os.Stat(dir)
				if receipt == "" && !os.IsNotExist(err) || receipt != "" && err != nil {
					t.Fatal("wrong invalid-export retention", err)
				}
			})
		}
	}
}

func TestDiscardInvalidUnsubmittedCapture(t *testing.T) {
	s, _, id := captureFixture(t)
	dir := filepath.Join(s.capture.root, id)
	if err := os.WriteFile(filepath.Join(dir, "capture.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	s.discardCapture(id)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid unsubmitted export orphaned", err)
	}
}

func TestCaptureContextPreferencePersistsAndFailsClosed(t *testing.T) {
	s, _, _ := captureFixture(t)
	if !s.CapturePreferences().IncludeBackground {
		t.Fatal("context should default on")
	}
	if _, err := s.SaveCapturePreferences(CapturePreferences{}); err != nil {
		t.Fatal(err)
	}
	restored := newService(&memoryStore{value: defaults()})
	if err := restored.configureCapture(s.capture.root); err != nil {
		t.Fatal(err)
	}
	if restored.CapturePreferences().IncludeBackground {
		t.Fatal("preference lost on restart")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, nil, 0600)
	restored.capture.preferenceFile = filepath.Join(blocker, "setting")
	if _, err := restored.SaveCapturePreferences(CapturePreferences{IncludeBackground: true}); err == nil || restored.CapturePreferences().IncludeBackground {
		t.Fatal("failed persistence enabled background")
	}
	if err := os.WriteFile(s.capture.preferenceFile, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restored.configureCapture(s.capture.root); err != nil {
		t.Fatal(err)
	}
	if v := restored.CapturePreferences(); v.IncludeBackground || v.Notice == "" {
		t.Fatal("unreadable preference did not fail closed", v)
	}
}
