// Package taskterminal launches a standard native TUI for a host-resolved task.
package taskterminal

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Launcher struct {
	mu                sync.Mutex
	observationMu     sync.Mutex
	observationCancel context.CancelFunc
	directory         string
	open              func(context.Context, string) error
	openWindow        func(context.Context, string) (Window, error)
	windows           map[string]Window
	unmanaged         map[string]bool
	reconnect         map[string]bool // Only after atomically revoking an unclaimed script.
	waitMu            sync.Mutex
	waitCancel        context.CancelFunc
	waitResult        func(context.Context) (bool, error)
	receiptGrace      time.Duration // After a document reply, not while its dialog is open.
}

var ErrUnconfirmed = errors.New("terminal launch was not confirmed")

func New(directory string, open func(context.Context, string) error) *Launcher {
	return &Launcher{directory: directory, open: open}
}
func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
func Script(t api.TerminalTarget) (string, error) {
	if !filepath.IsAbs(t.Binary) || !filepath.IsAbs(t.Directory) {
		return "", errors.New("invalid native terminal target")
	}
	for _, v := range []string{t.Binary, t.Directory, t.Endpoint, t.Thread, t.CodexHome, t.Session, t.Store, t.TokenFile} {
		if strings.ContainsRune(v, 0) {
			return "", errors.New("invalid terminal argument")
		}
	}
	script := "#!/bin/sh\nunset NO_COLOR\n"
	switch t.Runtime {
	case "codex":
		if !strings.HasPrefix(t.Endpoint, "unix:///") || t.Thread == "" || strings.HasPrefix(t.Thread, "-") {
			return "", errors.New("invalid Codex terminal target")
		}
		if t.CodexHome != "" {
			script += "export CODEX_HOME=" + quote(t.CodexHome) + "\n"
		}
		script += "cd " + quote(t.Directory) + " || exit 1\nexec " + quote(t.Binary) + " --remote " + quote(t.Endpoint) + " resume " + quote(t.Thread) + "\n"
	case "caelis":
		u, err := url.Parse(t.Endpoint)
		if err != nil || u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || t.Session == "" || strings.HasPrefix(t.Session, "-") || !filepath.IsAbs(t.Store) || !filepath.IsAbs(t.TokenFile) {
			return "", errors.New("invalid Caelis terminal target")
		}
		script += "unset CAELIS_CONTROL_URL CAELIS_CONTROL_TOKEN CAELIS_CONTROL_TOKEN_FILE CAELIS_CONTROL_EMBEDDED\n"
		script += "cd " + quote(t.Directory) + " || exit 1\nexec " + quote(t.Binary) + " attach --control-url " + quote(t.Endpoint) + " --session " + quote(t.Session) + " --store-dir " + quote(t.Store) + " --control-token-file " + quote(t.TokenFile) + "\n"
	default:
		return "", errors.New("unsupported terminal runtime")
	}
	return script, nil
}
func (l *Launcher) Open(ctx context.Context, id string, t api.TerminalTarget) error {
	l.lockOperation()
	defer l.mu.Unlock()
	// Some terminals quit just after their last client ends. Reuse can race that
	// exit. Only an already-ended client, a revoked command and a proven closed
	// original app allow this same gesture to continue with one replacement.
	// A dialog cancellation in a living app never takes this path.
	previous := l.windows[id]
	cw, controlled := previous.(ControlledWindow)
	ended := false
	if controlled {
		if o, err := cw.Observe(ctx); err == nil {
			ended = o.ClientEnded && o.State != WindowClosed
		}
	}
	err := l.openConnection(ctx, id, t)
	if ended && errors.Is(err, ErrWindowOpenCancelled) && l.reconnect[id] && l.windows[id] == previous && ctx.Err() == nil {
		if o, observed := cw.Observe(ctx); observed == nil && o.State == WindowClosed {
			return l.openConnection(ctx, id, t)
		}
	}
	return err
}

// Called with the operation lease held. A failed attempt fences its script
// before returning; another attempt cannot overlap command delivery.
func (l *Launcher) openConnection(ctx context.Context, id string, t api.TerminalTarget) (result error) {
	submitted := false
	defer func() {
		if result != nil && !submitted {
			result = NotLaunched(result)
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	script, err := Script(t)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(l.directory) || l.open == nil && l.openWindow == nil {
		return errors.New("terminal launcher unavailable")
	}
	if err = privateDirectory(l.directory); err != nil {
		return err
	}
	// Each explicit click gets a distinct receipt. A terminal may hold its
	// consent dialog open after Launch Services has already returned success.
	directory, err := os.MkdirTemp(l.directory, ".launch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "Caelis Bot.command")
	pending := filepath.Join(directory, "pending")
	acceptedPrefix := filepath.Join(directory, "accepted-")
	if err = os.WriteFile(pending, []byte("pending"), 0600); err != nil {
		return err
	}
	// Publish acceptance and its client PID in the same rename. A duplicate
	// script cannot overwrite the winning client's identity before Confirm.
	guard := "#!/bin/sh\nif ! /bin/mv " + quote(pending) + " " + quote(acceptedPrefix) + "\"$$\" 2>/dev/null; then\n  printf '%s\\n' 'This terminal request has expired. Open the task again from Caelis Bot.'\n  exit 1\nfi\n"

	if err = writeScript(directory, path, guard+strings.TrimPrefix(script, "#!/bin/sh\n")); err != nil {
		return err
	}
	var window Window
	if previous := l.windows[id]; previous != nil {
		cw, ok := previous.(ControlledWindow)
		if !ok {
			return ErrWindowUnsupported
		}
		o, err := cw.Observe(ctx)
		if err != nil {
			return err
		}
		if o.State != WindowClosed && !o.ClientEnded && !l.reconnect[id] {
			return ErrWindowIdentity
		}
		if _, ok := previous.(DocumentWindow); ok && o.State != WindowClosed {
			window = previous
		} else {
			// An unsupported/closed owner may be replaced, never force quit.
			previous.Release()
			delete(l.windows, id)
		}
	}
	confirmed := false
	defer func() {
		if window != nil && !confirmed {
			if _, ok := window.(DocumentWindow); ok {
				return
			}
			if owned, ok := window.(interface{ keepUnconfirmedLaunch() bool }); ok && owned.keepUnconfirmedLaunch() {
				return
			}
			window.Release()
			delete(l.windows, id)
		}
	}()
	submitted = true // A callback may submit before returning an error or cancellation.
	if window != nil {
		err = window.(DocumentWindow).OpenDocument(ctx, path)
	} else if l.openWindow != nil {
		window, err = l.openWindow(ctx, path)
	} else {
		err = l.open(ctx, path)
	}
	// An uncertain open must not become an automatic second launch. Keep an
	// exact returned window even when the caller could not confirm execution.
	if window != nil {
		if previous := l.windows[id]; previous != nil && previous != window {
			previous.Release()
		}
		l.windows[id] = window
	}
	confirm := func() error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		pid := 0
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "accepted-") {
				continue
			}
			confirmed = true
			delete(l.reconnect, id)
			next, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), "accepted-"))
			if err != nil || next <= 0 || pid != 0 {
				return ErrWindowIdentity
			}
			pid = next
		}
		if pid == 0 {
			return os.ErrNotExist
		}
		if window == nil {
			if l.unmanaged == nil {
				l.unmanaged = map[string]bool{}
			}
			l.unmanaged[id] = true
			return nil
		}
		return window.Confirm(context.WithoutCancel(ctx), pid)
	}
	// Rename in the script and removal here race on one path. A successful
	// removal proves the old command cannot execute, even from a buffered copy.
	// Missing/unknown receipts alone never authorize resubmission.
	revoke := func(reason error) error {
		removed := os.Remove(pending)
		check := confirm()
		if !errors.Is(check, os.ErrNotExist) {
			if check != nil {
				return errors.Join(reason, check)
			}
			if errors.Is(reason, ErrWindowActivation) || errors.Is(reason, ErrWindowOpenCancelled) || errors.Is(reason, ErrWindowNotConnected) || errors.Is(reason, context.Canceled) || errors.Is(reason, context.DeadlineExceeded) {
				return nil
			}
			return reason
		}
		if removed != nil {
			return errors.Join(reason, removed)
		}
		if l.reconnect == nil {
			l.reconnect = map[string]bool{}
		}
		l.reconnect[id] = true
		return reason
	}

	if err != nil {
		return revoke(err)
	}
	ctx, cancelWait := context.WithCancel(ctx)
	defer cancelWait()
	l.waitMu.Lock()
	l.waitCancel = cancelWait
	if dw, ok := window.(DocumentWindow); ok {
		l.waitResult = dw.DocumentResult
	}
	l.waitMu.Unlock()
	defer func() { l.waitMu.Lock(); l.waitCancel = nil; l.waitResult = nil; l.waitMu.Unlock() }()
	var repliedAt time.Time
	grace := l.receiptGrace
	if grace == 0 {
		grace = 3 * time.Second
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := confirm(); !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if cw, ok := window.(ControlledWindow); ok {
			if o, e := cw.Observe(ctx); e == nil && o.State == WindowClosed {
				return revoke(ErrWindowOpenCancelled)
			}
		}
		if dw, ok := window.(DocumentWindow); ok && ctx.Err() == nil {
			complete, e := dw.DocumentResult(ctx)
			if e != nil {
				return revoke(e)
			}
			if complete {
				if repliedAt.IsZero() {
					repliedAt = time.Now()
				}
				if time.Since(repliedAt) >= grace {
					return revoke(ErrWindowNotConnected)
				}
			}
		}
		select {
		case <-ctx.Done():
			return revoke(errors.Join(ErrUnconfirmed, ctx.Err()))
		case <-ticker.C:
		}
	}
}

// Explicit input can retire a receipt wait. The caller waits for its controller
// to finish revocation before admitting a replacement document request.
func (l *Launcher) retryPending(ctx context.Context) (waiting, retired bool) {
	l.waitMu.Lock()
	defer l.waitMu.Unlock()
	if l.waitCancel == nil {
		return false, false
	}
	// Do not stack document requests while a native dialog still awaits input.
	// Once it has replied, fresh input can revoke any unclaimed script early.
	if l.waitResult != nil {
		complete, err := l.waitResult(ctx)
		if !complete && err == nil {
			return true, false
		}
	}
	l.waitCancel()
	l.waitCancel = nil
	return true, true
}

func privateDirectory(directory string) error {
	if !filepath.IsAbs(directory) {
		return errors.New("terminal directory must be absolute")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("terminal directory is not a private directory")
	}
	return os.Chmod(directory, 0700)
}

func writeScript(directory, path, script string) error {
	f, err := os.CreateTemp(directory, ".attach-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0700); err == nil {
		_, err = f.WriteString(script)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
