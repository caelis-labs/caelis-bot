// Package taskterminal launches a standard native TUI for a host-resolved task.
package taskterminal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Launcher struct {
	mu        sync.Mutex
	directory string
	open      func(context.Context, string) error
}

func New(directory string, open func(context.Context, string) error) *Launcher {
	return &Launcher{directory: directory, open: open}
}
func quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
func Script(t api.TerminalTarget) (string, error) {
	if !filepath.IsAbs(t.Binary) || !filepath.IsAbs(t.Directory) || !strings.HasPrefix(t.Endpoint, "unix:///") || t.Thread == "" || strings.HasPrefix(t.Thread, "-") {
		return "", errors.New("invalid native terminal target")
	}
	for _, v := range []string{t.Binary, t.Directory, t.Endpoint, t.Thread, t.CodexHome} {
		if strings.ContainsRune(v, 0) {
			return "", errors.New("invalid terminal argument")
		}
	}
	script := "#!/bin/sh\n"
	if t.CodexHome != "" {
		script += "export CODEX_HOME=" + quote(t.CodexHome) + "\n"
	}
	script += "cd " + quote(t.Directory) + " || exit 1\nexec " + quote(t.Binary) + " --remote " + quote(t.Endpoint) + " resume " + quote(t.Thread) + "\n"
	return script, nil
}
func (l *Launcher) Open(ctx context.Context, id string, t api.TerminalTarget) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	script, err := Script(t)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(l.directory) || l.open == nil {
		return errors.New("terminal launcher unavailable")
	}
	if err = os.MkdirAll(l.directory, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(l.directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("terminal directory is not a private directory")
	}
	if err = os.Chmod(l.directory, 0700); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(id))
	path := filepath.Join(l.directory, "task-"+hex.EncodeToString(sum[:16])+".command")
	// One atomically replaced file per ledger task (ledger capped at 100). No
	// conversation text or credentials are written into the launch command.
	f, err := os.CreateTemp(l.directory, ".attach-")
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
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return l.open(ctx, path)
}
