package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type memoryStore struct {
	value Placement
	err   error
	saves int
}

func (s *memoryStore) Load() (Placement, error) { return s.value, nil }
func (s *memoryStore) Save(p Placement) error   { s.value = p; s.saves++; return s.err }

type fakeDriver struct {
	displays     []Rect
	placement    Placement
	approvalOpen bool
	bubbleOpen   bool
	stopped      bool
	hit          []byte
}

func (d *fakeDriver) screens() []Rect   { return d.displays }
func (d *fakeDriver) apply(p Placement) { d.placement = p }
func (d *fakeDriver) prepareWindowRecall() bool {
	d.bubbleOpen = false
	return true
}
func (d *fakeDriver) approval()        { d.approvalOpen = true }
func (d *fakeDriver) bubble(open bool) { d.bubbleOpen = open }
func (d *fakeDriver) mask(b []byte)    { d.hit = b }
func (d *fakeDriver) stop()            { d.stopped = true }
func setup() (*Service, *fakeDriver, *memoryStore) {
	store := &memoryStore{value: defaults()}
	d := &fakeDriver{displays: []Rect{{0, 40, 1440, 860}}}
	s := newService(store)
	s.start(d)
	return s, d, store
}
func TestSurfaceLifetimeIsIndependent(t *testing.T) {
	s, d, store := setup()
	chatOpen := false
	s.openHistory = func() { chatOpen = true }
	s.closeHistory = func() { chatOpen = false }
	s.OpenHistory()
	if err := s.SetVisible(false); err != nil {
		t.Fatal(err)
	}
	if !chatOpen || d.stopped || d.placement.Visible {
		t.Fatal("hiding pet changed chat or lifetime")
	}
	s.CloseHistory()
	if chatOpen || d.stopped {
		t.Fatal("close must only hide chat")
	}
	s.OpenHistory()
	if !chatOpen {
		t.Fatal("hidden app cannot be recalled")
	}
	s.shutdown()
	s.shutdown()
	if !d.stopped || store.saves != 2 {
		t.Fatal("explicit shutdown must persist once and stop native resources")
	}
	if err := s.SetVisible(true); err == nil {
		t.Fatal("operation accepted after shutdown")
	}
}
func TestScaleDragAndDisplayRecovery(t *testing.T) {
	s, d, store := setup()
	s.moved(-900, 100) // unplugged/offscreen display
	p := s.Placement()
	if p.X < 0 || p.Y < 40 {
		t.Fatalf("offscreen: %+v", p)
	}
	s.moved(600, 100)
	p = s.Placement()
	center := p.X + BaseWidth*p.Scale/2
	if err := s.SetScale(MaxScale); err != nil {
		t.Fatal(err)
	}
	p = s.Placement()
	if math.Abs(p.X+BaseWidth*p.Scale/2-center) > 0.001 {
		t.Fatal("scale moved feet horizontally")
	}
	for _, invalid := range []float64{math.NaN(), math.Inf(1), 0, 2} {
		if s.SetScale(invalid) == nil {
			t.Fatal("invalid scale accepted")
		}
	}
	d.displays = []Rect{{-1200, 0, 1200, 800}}
	s.recoverDisplay()
	p = s.Placement()
	if p.X < -1200 || p.X+BaseWidth*p.Scale > 0 {
		t.Fatalf("removed display recovery failed: %+v", p)
	}
	restored := newService(store)
	restored.start(d)
	if restored.Placement() != p {
		t.Fatal("restart did not preserve position, scale and visibility")
	}
}

func TestSettingsScalePreviewAndCommit(t *testing.T) {
	s, d, store := setup()
	for _, scale := range []float64{.9, 1.1, 1.234567} {
		if err := s.PreviewScale(scale); err != nil {
			t.Fatal(err)
		}
		if store.saves != 0 || d.placement.Scale != scale {
			t.Fatal("preview persisted or lost continuous value")
		}
	}
	if err := s.CommitScale(); err != nil {
		t.Fatal(err)
	}
	if store.saves != 1 || store.value.Scale != 1.234567 {
		t.Fatal("final value was not saved once")
	}
	opened := 0
	s.openSettings = func() { opened++ }
	s.OpenRuntimeSettings()
	if s.SettingsSection() != "runtime" {
		t.Fatal("contextual runtime entry lost")
	}
	s.OpenUpdates()
	if s.SettingsSection() != "updates" {
		t.Fatal("update menu did not route to settings")
	}
	s.OpenSettings()
	if opened != 3 || s.SettingsSection() != "general" {
		t.Fatal("settings routing changed")
	}
}
func TestMaskValidationAndPersistenceFailure(t *testing.T) {
	s, d, store := setup()
	if s.SetHitMask(context.Background(), "bad") == nil || s.SetHitMask(context.Background(), base64.StdEncoding.EncodeToString([]byte{1})) == nil {
		t.Fatal("malformed mask accepted")
	}
	b := make([]byte, int(BaseWidth*BaseHeight))
	b[500] = 1
	if err := s.SetHitMask(context.Background(), base64.StdEncoding.EncodeToString(b)); err != nil {
		t.Fatal(err)
	}
	if d.hit[500] != 1 {
		t.Fatal("mask lost")
	}
	store.err = errors.New("disk full")
	if err := s.SetVisible(false); err == nil {
		t.Fatal("save failure swallowed")
	}
}
func TestFileStoreRecovery(t *testing.T) {
	store := fileStore{filepath.Join(t.TempDir(), "preferences", "placement.json")}
	p, err := store.Load()
	if err != nil || p != defaults() {
		t.Fatal("missing preferences must default")
	}
	p = Placement{X: -900, Y: 100, Scale: 1.6, Visible: false, Positioned: true}
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	actual, err := store.Load()
	if err != nil || actual != p {
		t.Fatal("roundtrip failed")
	}
	// Windows uses ACLs, not POSIX mode bits. Its native host remains gated on
	// separate user-profile ACL verification; do not claim this check covers it.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.path)
		if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
			t.Fatal("preferences permissions too broad", err)
		}
	}
	p.Scale = 0.731234
	if err := store.Save(p); err != nil {
		t.Fatal("replacing existing preferences failed", err)
	}
	if actual, err := store.Load(); err != nil || actual != p {
		t.Fatal("replacement lost latest placement", actual, err)
	}
	if err := os.WriteFile(store.path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	actual, err = store.Load()
	if err == nil || actual != defaults() {
		t.Fatal("corrupt preferences must recover with a reported error")
	}
}
func TestNormalizeNonFiniteAndSmallScreen(t *testing.T) {
	p := normalize(Placement{X: math.Inf(1), Y: math.NaN(), Scale: math.Inf(1), Positioned: true}, []Rect{{0, 0, 800, 600}})
	if !finite(p.X) || !finite(p.Y) || p.Scale != 1 {
		t.Fatalf("invalid normalized placement: %+v", p)
	}
}

func TestRendererMaskWaitsForNativeOwnership(t *testing.T) {
	s := newService(&memoryStore{value: defaults()})
	mask := base64.StdEncoding.EncodeToString(make([]byte, int(BaseWidth*BaseHeight)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.SetHitMask(ctx, mask); !errors.Is(err, context.Canceled) {
		t.Fatalf("unready call: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.SetHitMask(context.Background(), mask) }()
	d := &fakeDriver{displays: []Rect{{0, 0, 1440, 900}}}
	s.start(d)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(d.hit) != int(BaseWidth*BaseHeight) {
		t.Fatal("renderer mask lost during startup")
	}
}

func TestNativeResizeCommitsExactGeometry(t *testing.T) {
	s, d, store := setup()
	before := store.saves
	// A menu gesture sends only its final native position/scale. The commit must
	// not recompute the anchor from stale pre-gesture Go geometry.
	if err := s.resized(600.25, 300.75, 1.234567); err != nil {
		t.Fatal(err)
	}
	p := s.Placement()
	if p.X != 600.25 || p.Y != 300.75 || p.Scale != 1.234567 || d.placement != p || store.value != p || store.saves != before+1 {
		t.Fatalf("native resize lost geometry or changed focus: %+v", p)
	}
	for _, invalid := range []float64{math.NaN(), math.Inf(1), 0, 2} {
		if s.resized(600, 300, invalid) == nil {
			t.Fatal("invalid size accepted")
		}
	}
	if s.resized(math.NaN(), 300, 1) == nil {
		t.Fatal("invalid position accepted")
	}
	s.shutdown()
	if s.resized(600, 300, 1) == nil {
		t.Fatal("late gesture accepted after shutdown")
	}
}

func TestPetToggleUsesCurrentChatVisibility(t *testing.T) {
	s, d, store := setup()
	open := false
	s.openHistory = func() { open = true }
	s.closeHistory = func() { open = false }
	s.historyCanHide = func() bool { return false } // A background chat is still closed by a pet click.
	s.petChatCanHide = func() bool { return open }
	s.Activate()
	if !open {
		t.Fatal("pet click did not open chat")
	}
	s.Activate()
	if open || d.stopped || store.saves != 0 {
		t.Fatal("pet click must only close chat")
	}
	s.OpenHistory()
	open = false // Native window dismissal does not pass through Go.
	s.Activate()
	if !open {
		t.Fatal("toggle used stale Go visibility after native dismissal")
	}
	s.OpenHistory() // Tray Open is idempotent, not a toggle.
	if !open {
		t.Fatal("tray open closed chat")
	}
	s.CloseHistory()
	s.shutdown()
	s.Activate()
	if open {
		t.Fatal("late pet click reopened after shutdown")
	}
}

func TestBubbleDoesNotOpenKeyboardOrOwnLifetime(t *testing.T) {
	s, d, store := setup()
	if err := s.SetBubbleVisible(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !d.bubbleOpen || store.saves != 0 {
		t.Fatal("background message activated keyboard or persisted UI")
	}
	s.OpenApproval()
	if !d.approvalOpen {
		t.Fatal("explicit review must open decision surface")
	}
	if !d.bubbleOpen || d.stopped {
		t.Fatal("opening decision changed task or bubble lifetime")
	}
	s.shutdown()
	if s.SetBubbleVisible(context.Background(), false) == nil {
		t.Fatal("late message accepted after shutdown")
	}
}
