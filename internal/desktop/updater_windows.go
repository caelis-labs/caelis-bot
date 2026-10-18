package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/updates"
	"github.com/caelis-labs/caelis-bot/internal/windowsupdate"
)

// Only a clean exact release-tag build enables public update subscriptions.
var windowsReleaseUpdates = "false"

type windowsUpdateCore interface {
	PrepareUpdate() error
	CancelUpdate()
	Close() error
}

type windowsUpdater struct {
	mu                                      sync.Mutex
	service                                 *Service
	core                                    windowsUpdateCore
	root, target, identity, channel, format string
	client                                  *windowsupdate.Client
	state                                   UpdatePreferences
	offer                                   *windowsupdate.Offer
	archive                                 string
	context                                 context.Context
	quit                                    func()
	notify                                  func()
	pauseTerminals                          func() func()
	closeTerminals                          func(context.Context) error
}

func (s *Service) startWindowsUpdater(root string, core windowsUpdateCore, pauseTerminals func() func(), closeTerminals func(context.Context) error, quit func(), notify func()) func() {
	exe, e := os.Executable()
	if e != nil {
		return func() {}
	}
	_, id := applicationIdentity()
	u := &windowsUpdater{service: s, core: core, root: root, target: filepath.Dir(exe), identity: id, channel: "preview", state: UpdatePreferences{Automatic: true}, quit: quit, notify: notify, pauseTerminals: pauseTerminals, closeTerminals: closeTerminals}
	u.format, e = windowsupdate.Installation(u.target, id)
	if e != nil {
		return func() {}
	}
	if windowsReleaseUpdates != "true" || buildChannel == "development" {
		return func() {}
	}
	if buildChannel == "release" {
		u.channel = "stable"
	}
	u.client, e = windowsupdate.ProductionClient()
	if e != nil {
		return func() {}
	}
	u.state.Available = true
	u.state.Format = u.format
	u.state.State = "idle"
	var stored struct {
		Automatic bool `json:"automatic"`
	}
	if b, e := os.ReadFile(filepath.Join(root, "windows-updates.json")); !os.IsNotExist(e) {
		if e != nil || json.Unmarshal(b, &stored) != nil {
			u.state.Automatic = false
			u.state.State = "failed"
			u.state.Message = s.text("native.windowsUpdateRecovery", nil)
		} else {
			u.state.Automatic = stored.Automatic
		}
	}
	// Report the exact last native installation receipt. Never infer success from
	// a changed executable or automatically replay an interrupted transaction.
	var pending struct {
		Directory string `json:"directory"`
	}
	if b, e := os.ReadFile(filepath.Join(root, "windows-update-pending.json")); !os.IsNotExist(e) {
		var receipt windowsupdate.ApplyResult
		if e == nil && json.Unmarshal(b, &pending) == nil && filepath.Dir(pending.Directory) == filepath.Join(root, "Updates") {
			b, e = os.ReadFile(filepath.Join(pending.Directory, "result.json"))
		} else {
			e = errors.New("update receipt owner unavailable")
		}
		if e != nil || json.Unmarshal(b, &receipt) != nil || receipt.State != "installed" {
			u.state.State = "failed"
			u.state.Message = s.text("native.windowsUpdateRecovery", nil)
		} else if receipt.Version != updates.Version {
			u.state.State = "failed"
			u.state.Message = s.text("native.windowsUpdateRecovery", nil)
		} else {
			_ = os.Remove(filepath.Join(root, "windows-update-pending.json"))
		}
	}
	s.updatePreferences = func() UpdatePreferences { u.mu.Lock(); defer u.mu.Unlock(); return u.state }
	s.setAutomaticUpdates = func(enabled bool) error {
		u.mu.Lock()
		defer u.mu.Unlock()
		if e := localstate.Write(filepath.Join(root, "windows-updates.json"), map[string]bool{"automatic": enabled}); e != nil {
			return e
		}
		u.state.Automatic = enabled
		return nil
	}
	s.checkNativeUpdates = func() error { go u.check(true); return nil }
	s.installNativeUpdate = u.install
	ctx, cancel := context.WithCancel(context.Background())
	u.context = ctx
	go func() {
		timer := time.NewTimer(45 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				u.check(false)
				timer.Reset(24 * time.Hour)
			}
		}
	}()
	return cancel
}
func (u *windowsUpdater) check(manual bool) {
	u.mu.Lock()
	if u.state.Waiting || u.state.State == "ready" || (!manual && !u.state.Automatic) || u.state.State == "closing" {
		u.mu.Unlock()
		return
	}
	if _, e := os.Stat(filepath.Join(u.root, "windows-update-pending.json")); !os.IsNotExist(e) {
		// A user-requested check can start a fresh transaction after an explicit
		// failure receipt. Missing/waiting/installing receipts stay unresolved;
		// never replay their possibly completed effects.
		if !manual || retireFailedWindowsUpdate(u.root) != nil {
			u.state.State = "failed"
			u.state.Message = u.service.text("native.windowsUpdateRecovery", nil)
			u.mu.Unlock()
			return
		}
	}
	u.state.Waiting = true
	u.state.State = "checking"
	u.state.Message = u.service.text("native.windowsUpdateChecking", nil)
	u.mu.Unlock()
	ctx, cancel := context.WithTimeout(u.context, 10*time.Minute)
	defer cancel()
	// Shutdown cancels transfer via the controller's lifetime context below.
	offer, e := u.client.Check(ctx, updates.Version, u.channel, u.format)
	if e != nil {
		u.finish("failed", u.service.text("native.windowsUpdateCheckFailed", nil))
		return
	}
	if offer == nil {
		u.finish("current", u.service.text("native.windowsUpdateCurrent", nil))
		return
	}
	u.mu.Lock()
	u.state.State = "downloading"
	u.state.Latest = offer.Manifest.Version
	u.state.Message = u.service.text("native.windowsUpdateDownloading", nil)
	u.mu.Unlock()
	dir, e := os.MkdirTemp(filepath.Join(u.root, "Updates"), "download-")
	if os.IsNotExist(e) {
		if e = os.MkdirAll(filepath.Join(u.root, "Updates"), 0700); e == nil {
			dir, e = os.MkdirTemp(filepath.Join(u.root, "Updates"), "download-")
		}
	}
	if e != nil {
		u.finish("failed", u.service.text("native.windowsUpdateDownloadFailed", nil))
		return
	}
	if e = localstate.ProtectDirectory(dir); e != nil {
		u.finish("failed", u.service.text("native.windowsUpdateDownloadFailed", nil))
		return
	}
	archive, e := u.client.Download(ctx, offer, dir)
	if e != nil {
		u.finish("failed", u.service.text("native.windowsUpdateDownloadFailed", nil))
		return
	}
	u.mu.Lock()
	u.offer = offer
	u.archive = archive
	u.state.Waiting = false
	u.state.State = "ready"
	u.state.Message = u.service.text("native.windowsUpdateReady", nil)
	u.mu.Unlock()
	if u.notify != nil {
		u.notify()
	}
}

func retireFailedWindowsUpdate(root string) error {
	path := filepath.Join(root, "windows-update-pending.json")
	var pending struct {
		Directory string `json:"directory"`
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &pending) != nil || filepath.Dir(pending.Directory) != filepath.Join(root, "Updates") {
		return errors.New("unconfirmed update owner")
	}
	var receipt windowsupdate.ApplyResult
	data, err = os.ReadFile(filepath.Join(pending.Directory, "result.json"))
	if err != nil || json.Unmarshal(data, &receipt) != nil || receipt.State != "failed" {
		return errors.New("original update outcome remains unresolved")
	}
	// Keep the full old transaction for diagnostics; only release the pointer.
	return os.Remove(path)
}
func (u *windowsUpdater) finish(state, message string) {
	u.mu.Lock()
	u.state.Waiting = false
	u.state.State = state
	u.state.Message = message
	u.mu.Unlock()
}
func (u *windowsUpdater) install() error {
	u.mu.Lock()
	if u.state.State != "ready" || u.offer == nil || u.state.Waiting {
		u.mu.Unlock()
		return errors.New("no downloaded Windows update")
	}
	offer, archive := u.offer, u.archive
	u.state.State = "preparing"
	u.state.Waiting = true
	u.mu.Unlock()
	go u.apply(offer, archive)
	return nil
}

// Run shutdown away from the UI/RPC thread. Download never authorizes install;
// the explicit InstallUpdate action claims one original offer.
func (u *windowsUpdater) apply(offer *windowsupdate.Offer, archive string) {
	resumeTerminals := func() {}
	if u.pauseTerminals != nil {
		resumeTerminals = u.pauseTerminals()
	}
	handedOff := false
	defer func() {
		if !handedOff {
			resumeTerminals()
		}
	}()
	if e := u.core.PrepareUpdate(); e != nil {
		u.finish("ready", u.service.text("native.windowsUpdateRecovery", nil))
		return
	}
	ctx, cancel := context.WithTimeout(u.context, 15*time.Second)
	defer cancel()
	var closeError error
	if u.closeTerminals != nil {
		closeError = u.closeTerminals(ctx)
	}
	if closeError == nil {
		closeError = windowsupdate.CloseTerminalHelpers(ctx, u.target)
	}
	if closeError != nil {
		u.core.CancelUpdate()
		u.finish("ready", u.service.text("native.windowsUpdateTerminalCloseFailed", nil))
		return
	}
	pending := filepath.Join(u.root, "windows-update-pending.json")
	if e := localstate.Write(pending, map[string]string{"directory": filepath.Dir(archive)}); e != nil {
		u.core.CancelUpdate()
		u.finish("ready", u.service.text("native.windowsUpdateRecovery", nil))
		return
	}
	transaction, e := windowsupdate.Start(u.root, u.target, u.identity, updates.Version, offer, archive)
	if e != nil {
		_ = os.Remove(pending)
		u.core.CancelUpdate()
		u.finish("failed", u.service.text("native.windowsUpdateRecovery", nil))
		return
	}
	u.mu.Lock()
	u.state.State = "closing"
	u.state.Message = u.service.text("native.windowsUpdateClosing", nil)
	u.mu.Unlock()
	handedOff = true
	// The current assembly chooses ShutdownForUpdate after PrepareUpdate. A
	// failed close must never become an install just because this process exits.
	if e = u.core.Close(); e == nil {
		e = windowsupdate.Commit(transaction, offer.Manifest.Version)
	}
	if e != nil {
		u.finish("failed", u.service.text("native.windowsUpdateRecovery", nil))
	}
	u.quit()
}
