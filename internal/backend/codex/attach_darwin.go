package codex

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type ownedSocketConnection struct {
	*socketConnection
	tools *ownedTools
}

func (c *ownedSocketConnection) captureTools()           { c.tools.capture() }
func (c *ownedSocketConnection) toolCleanupError() error { return c.tools.failure() }
func (c *ownedSocketConnection) Close() error            { c.tools.capture(); return c.socketConnection.Close() }

// A private socket makes the native TUI a second standard client. Its lifetime
// belongs to the Bot, not to the observer terminal. No TCP listener or private IPC.
func startAttachableProcess(ctx context.Context, opts Options) (connection, func(), error) {
	binary, err := runtimeBinary(opts.Binary)
	if err != nil {
		return nil, nil, err
	}
	// macOS sockaddr_un is short; the user data directory can exceed that limit.
	dir, err := os.MkdirTemp("/tmp", "caelis-bot-")
	if err != nil {
		return nil, nil, errors.New("could not create private runtime socket directory")
	}
	path := filepath.Join(dir, "runtime.sock")
	cmd := exec.Command(binary, "app-server", "--listen", "unix://"+path)
	cmd.Dir = opts.Directory
	cmd.Env = ownedEnvironment(cmd.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = ctx.Err(); err == nil {
		err = cmd.Start()
	}
	if err != nil {
		os.RemoveAll(dir)
		return nil, nil, errors.New("could not start attachable Codex App Server")
	}
	owned := newOwnedTools(cmd.Process.Pid)
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			owned.terminate()
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-exited:
			case <-time.After(2 * time.Second):
				_ = cmd.Process.Kill()
				<-exited
			}
			_ = os.RemoveAll(dir)
		})
	}
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for {
		if info, e := os.Stat(path); e == nil && info.Mode()&os.ModeSocket != 0 {
			if e = os.Chmod(path, 0600); e != nil {
				stop()
				return nil, nil, errors.New("could not protect runtime socket")
			}
			conn, e := dialExisting(ctx, path)
			if e == nil {
				return &ownedSocketConnection{conn, owned}, stop, nil
			}
		}
		select {
		case <-ctx.Done():
			stop()
			return nil, nil, ctx.Err()
		case <-exited:
			stop()
			return nil, nil, errors.New("Codex App Server did not expose a local attach endpoint")
		case <-ticker.C:
		}
	}
}
