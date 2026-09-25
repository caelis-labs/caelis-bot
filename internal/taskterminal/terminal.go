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
	"strings"
	"sync"
	"time"
)

type Launcher struct {
	mu        sync.Mutex
	directory string
	open      func(context.Context, string) error
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
	script := "#!/bin/sh\n"
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
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	script, err := Script(t)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(l.directory) || l.open == nil {
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
	pending, accepted := filepath.Join(directory, "pending"), filepath.Join(directory, "accepted")
	if err = os.WriteFile(pending, []byte("pending"), 0600); err != nil {
		return err
	}
	guard := "#!/bin/sh\nif ! /bin/mv " + quote(pending) + " " + quote(accepted) + " 2>/dev/null; then\n  printf '%s\\n' 'This terminal request has expired. Open the task again from Caelis Bot.'\n  exit 1\nfi\n"
	if err = writeScript(directory, path, guard+strings.TrimPrefix(script, "#!/bin/sh\n")); err != nil {
		return err
	}
	if err = l.open(ctx, path); err != nil {
		return err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err = os.Stat(accepted); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			// Revoking the pending token fences a delayed confirmation. If the
			// script already claimed it, acknowledge that execution did begin.
			_ = os.Remove(pending)
			if _, err = os.Stat(accepted); err == nil {
				return nil
			}
			return errors.Join(ErrUnconfirmed, ctx.Err())
		case <-ticker.C:
		}
	}
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
