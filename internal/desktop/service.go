package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/contentpack"
)

// driver owns native interaction, coordinate conversion and OS-thread dispatch.
// It must preserve nonactivation/partial hit testing, not silently substitute a
// normal foreground window. Unsupported native hosts fail before service start.
type driver interface {
	screens() []Rect
	apply(Placement)
	panel(bool)
	prepareWindowRecall() bool
	approval()
	bubble(bool)
	togglePanel()
	panelHeight(int)
	mask([]byte)
	stop()
}

// Service owns surface state, never execution state. P2 attaches a separate backend service.
// All operations (including native drag/display callbacks) serialize through mu.
type Service struct {
	languageMu          sync.Mutex
	languageSaveMu      sync.Mutex
	languageFile        string
	systemLanguages     []string
	language            LanguageState
	languageChanged     func(LanguageState)
	taskPreviews        []api.TaskPreview
	taskPreviewJSON     string
	resolveTaskTerminal func(context.Context, string) (api.TerminalTarget, error)
	launchTaskTerminal  func(context.Context, string, api.TerminalTarget) error
	taskOpenMu          sync.Mutex
	taskError           func(string, error)
	needsIntroduction   func() bool
	content             *contentpack.Registry
	pickContentFile     func() (string, error)
	contentChanged      func(contentpack.Appearance)
	contentImportMu     sync.Mutex
	shortcutFile        string
	shortcut            ShortcutState
	ready               chan struct{}
	mu                  sync.Mutex
	native              driver
	store               preferences
	placement           Placement
	started             bool
	stopped             bool
	pickFiles           func() ([]string, error)
	picking             bool
	files               []draftFile
	nextFile            uint64
	selectionFile       string
	selectionError      error
	openHistory         func()
	recallWindows       func()
	closeHistory        func()
	historyVisible      func() bool
	historyCanHide      func() bool
	activate            func()
	restartRuntime      func() error
	openSettings        func()
	closeSettings       func()
	settingsSection     string
	characterActivity   string
	openReleasePage     func() error
	updatePreferences   func() UpdatePreferences
	setAutomaticUpdates func(bool) error
	checkNativeUpdates  func() error
	pickRuntimeCLI      func() (string, error)
	copyText            func(string) bool
	storage             func() (api.AttachmentStorage, error)
	cleanStorage        func(context.Context) (api.AttachmentStorage, error)
	diagnosticReport    func() ([]byte, error)
	saveDiagnosticPath  func() (string, error)
	exportMu            sync.Mutex
}

func (s *Service) CopyText(text string) error {
	if len(text) > 1024*1024 || s.copyText == nil || !s.copyText(text) {
		return errors.New("暂时无法复制")
	}
	return nil
}

func (s *Service) PickRuntimeCLI() (string, error) {
	if s.pickRuntimeCLI == nil {
		return "", errors.New("文件选择暂不可用")
	}
	return s.pickRuntimeCLI()
}

func newService(store preferences) *Service {
	p, err := store.Load()
	if err != nil {
		slog.Warn("Desktop preferences could not be read; using defaults", "error", err)
	}
	return &Service{store: store, placement: p, ready: make(chan struct{})}
}
func (s *Service) start(d driver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.native = d
	s.started = true
	if d, ok := d.(shortcutDriver); ok {
		if err := d.registerShortcut(s.shortcut.Shortcut); err != nil {
			s.shortcut.Message = err.Error()
		} else {
			s.shortcut.Registered = s.shortcut.Shortcut.Enabled
		}
	}
	s.placement = normalize(s.placement, d.screens())
	d.apply(s.placement)
	close(s.ready)
	slog.Info("Desktop native host ready")
}
func (s *Service) persist() error {
	err := s.store.Save(s.placement)
	if err != nil {
		slog.Error("Desktop preferences could not be saved", "error", err)
	}
	return err
}
func (s *Service) SetScale(scale float64) error { return s.setScale(scale, true) }

// PreviewScale updates native geometry without writing a preference per pixel.
func (s *Service) PreviewScale(scale float64) error { return s.setScale(scale, false) }
func (s *Service) CommitScale() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return errors.New("desktop is not ready")
	}
	return s.persist()
}
func (s *Service) setScale(scale float64, persist bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return errors.New("desktop is not ready")
	}
	if !finite(scale) || scale < MinScale || scale > MaxScale {
		return errors.New("invalid pet size")
	}
	// Keep the feet centered when changing scale.
	s.placement.X += BaseWidth * (s.placement.Scale - scale) / 2
	s.placement.Y += 44 * (s.placement.Scale - scale)
	s.placement.Scale = scale
	s.placement = normalize(s.placement, s.native.screens())
	s.native.apply(s.placement)
	if persist {
		return s.persist()
	}
	return nil
}

// resized commits the final native slider geometry as one ordered mutation.
// Preview stays on AppKit's tracking loop; no renderer/Go roundtrip per pixel.
func (s *Service) resized(x, y, scale float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return errors.New("desktop is not ready")
	}
	if !finite(x) || !finite(y) || !finite(scale) || scale < MinScale || scale > MaxScale {
		return errors.New("invalid pet geometry")
	}
	s.placement.X, s.placement.Y, s.placement.Scale = x, y, scale
	s.placement = normalize(s.placement, s.native.screens())
	s.native.apply(s.placement)
	return s.persist()
}

func (s *Service) SetVisible(visible bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return errors.New("desktop is not ready")
	}
	s.placement.Visible = visible
	s.placement = normalize(s.placement, s.native.screens())
	s.native.apply(s.placement)
	return s.persist()
}
func (s *Service) OpenPanel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return
	}
	s.native.panel(true)
}
func (s *Service) ClosePanel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return
	}
	s.native.panel(false)
}

// A transition to another Bot window must not restore focus to another app.
// Call this before entering an AppKit transaction, never while on its UI thread:
// other service operations may already hold mu while waiting for AppKit.
func (s *Service) prepareWindowRecall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started && !s.stopped && s.native.prepareWindowRecall()
}
func (s *Service) OpenApproval() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started && !s.stopped {
		s.native.approval()
	}
}

// Bubble visibility is presentation only; it never owns or stops backend work.
func (s *Service) SetBubbleVisible(ctx context.Context, visible bool) error {
	select {
	case <-s.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return errors.New("desktop is not ready")
	}
	s.native.bubble(visible)
	return nil
}
func (s *Service) TogglePanel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return
	}
	// Outside clicks can close the native surface without a Go call. AppKit's
	// actual visibility is authoritative; do not mirror a stale boolean here.
	s.native.togglePanel()
}
func (s *Service) SetPanelHeight(ctx context.Context, height int) error {
	if height < 64 || height > 500 {
		return errors.New("invalid panel height")
	}
	// ResizeObserver may report the first layout before native ownership is ready.
	// Keep that layout pending rather than dropping it or showing a false UI error.
	select {
	case <-s.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return errors.New("desktop is not ready")
	}
	s.native.panelHeight(height)
	return nil
}

// SetPanelMenu changes only the hosting envelope, never the editor's size.
// Native activation fencing prevents an old renderer from moving a newer panel.
func (s *Service) SetPanelMenu(height, activation int) error {
	if height < 0 || height > 324 || activation < 1 || activation > 2147483647 {
		return errors.New("invalid panel menu layout")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return errors.New("desktop is not ready")
	}
	native, ok := s.native.(panelMenuDriver)
	if !ok {
		return errors.New("panel menu is unavailable")
	}
	native.panelMenu(height, activation)
	return nil
}
func (s *Service) SetHitMask(ctx context.Context, encoded string) error {
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(b) != int(BaseWidth*BaseHeight) {
		return errors.New("invalid hit mask")
	}
	return s.setHitMask(ctx, b)
}

// Animated silhouettes use one bit per logical pixel over the renderer bridge.
// Expand only at the native boundary; the driver retains its byte-mask contract.
func (s *Service) SetPackedHitMask(ctx context.Context, encoded string) error {
	const pixels = int(BaseWidth * BaseHeight)
	if len(encoded) != (pixels/8+2)/3*4 {
		return errors.New("invalid hit mask")
	}
	packed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(packed) != pixels/8 {
		return errors.New("invalid hit mask")
	}
	b := make([]byte, pixels)
	for i := range b {
		b[i] = (packed[i/8] >> (i % 8)) & 1
	}
	return s.setHitMask(ctx, b)
}

func (s *Service) setHitMask(ctx context.Context, b []byte) error {
	// A fast renderer may load before ApplicationStarted finishes native setup.
	// Wait for the ownership handoff, never retry on a timer or lose its mask.
	select {
	case <-s.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return errors.New("desktop is not ready")
	}
	s.native.mask(b)
	return nil
}
func (s *Service) Placement() Placement { s.mu.Lock(); defer s.mu.Unlock(); return s.placement }
func (s *Service) moved(x, y float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return
	}
	s.placement.X, s.placement.Y = x, y
	s.placement = normalize(s.placement, s.native.screens())
	s.native.apply(s.placement)
	_ = s.persist()
}
func (s *Service) recoverDisplay() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return
	}
	s.placement = normalize(s.placement, s.native.screens())
	s.native.apply(s.placement)
	_ = s.persist()
}
func (s *Service) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return
	}
	s.stopped = true
	_ = s.persist()
	s.native.stop()
}

// Chat is an optional interactive surface. Its visibility never owns a task.
func (s *Service) RecallWindows() {
	s.mu.Lock()
	f := s.recallWindows
	ready := s.started && !s.stopped
	s.mu.Unlock()
	if ready && f != nil {
		f()
	}
}

func (s *Service) OpenHistory() {
	s.mu.Lock()
	f := s.openHistory
	ready := s.started && !s.stopped
	s.mu.Unlock()
	if ready && f != nil {
		f()
	}
}

// Only the global shortcut toggles chat. Menus and pet double-click still recall
// it unconditionally. Read native state so close/minimise never leaves a cache.
func (s *Service) ToggleHistory() {
	s.mu.Lock()
	canHide := s.historyCanHide
	ready := s.started && !s.stopped
	s.mu.Unlock()
	if !ready {
		return
	}
	if canHide != nil && canHide() {
		s.CloseHistory()
	} else {
		s.OpenHistory()
	}
}

func (s *Service) CloseHistory() {
	s.mu.Lock()
	f := s.closeHistory
	s.mu.Unlock()
	if f != nil {
		f()
	}
}
func (s *Service) HistoryVisible() bool {
	s.mu.Lock()
	f := s.historyVisible
	s.mu.Unlock()
	return f != nil && f()
}

// Activate routes a pet click using authoritative backend state. Native visibility
// still decides whether an idle composer should toggle closed.
func (s *Service) Activate() {
	if s.activate != nil {
		s.activate()
	} else {
		s.TogglePanel()
	}
}
func (s *Service) CollapseBubble() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(bubbleDriver); ok && !s.stopped {
		d.expandBubble(false)
	}
}
func (s *Service) SetBubbleHeight(ctx context.Context, height int) error {
	if height < 68 || height > 480 {
		return errors.New("invalid bubble height")
	}
	select {
	case <-s.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.native.(bubbleDriver); ok && !s.stopped {
		d.bubbleHeight(height)
	}
	return nil
}

// Gesture is a bounded character action. It respects placement/visibility and
// cannot approve backend work or activate another application.
func (s *Service) Gesture(action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.stopped {
		return errors.New("角色暂不可用")
	}
	if !s.placement.Visible {
		return nil
	}
	if d, ok := s.native.(gestureDriver); ok {
		d.gesture(action)
		return nil
	}
	return errors.New("角色动作暂不可用")
}
