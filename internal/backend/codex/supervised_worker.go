//go:build darwin || linux

package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type supervisedSocketConnection struct {
	*socketConnection
	supervisor *SupervisedProcess
}

func (c *supervisedSocketConnection) freezeOwned() error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	return c.supervisor.Stop(ctx)
}
func (c *supervisedSocketConnection) forceKillOwned() error { return c.freezeOwned() }
func (c *supervisedSocketConnection) captureTools()         {}
func (c *supervisedSocketConnection) toolCleanupError() error {
	c.supervisor.mu.Lock()
	defer c.supervisor.mu.Unlock()
	return c.supervisor.stopErr
}
func (c *supervisedSocketConnection) renewOwnedLease(ctx context.Context, epoch string, deadline time.Time) error {
	return c.supervisor.Renew(ctx, epoch, time.Until(deadline))
}
func (c *supervisedSocketConnection) ownedSupervisorLive() bool { return c.supervisor.Live() }
func openSupervisedWorkerClient(ctx context.Context, opts Options, helper string) (*Client, func(), string, error) {
	if !OwnedRuntimeSupported() {
		return nil, nil, "", ErrOwnedRuntimeUnsupported
	}
	if opts.Socket != "" {
		return nil, nil, "", errors.New("supervised Worker cannot adopt an existing native endpoint")
	}
	dir, err := os.MkdirTemp("/tmp", "caelis-worker-")
	if err != nil {
		return nil, nil, "", err
	}
	socket := filepath.Join(dir, "runtime.sock")
	process, err := StartSupervisedProcess(ctx, SupervisedProcessOptions{HelperPath: helper, Binary: opts.Binary, Directory: opts.Directory, Socket: socket, Kind: SupervisedCodexUnix})
	if err != nil {
		os.RemoveAll(dir)
		return nil, nil, "", err
	}
	stop := func() {
		bounded, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_ = process.Stop(bounded)
		_ = os.RemoveAll(dir)
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if info, e := os.Stat(socket); e == nil && info.Mode()&os.ModeSocket != 0 {
			if e = os.Chmod(socket, 0600); e != nil {
				stop()
				return nil, nil, "", e
			}
			conn, e := dialExisting(ctx, socket)
			if e == nil {
				client, e := initializeClient(ctx, &supervisedSocketConnection{conn, process}, nil, opts)
				if e != nil {
					stop()
					return nil, nil, "", e
				}
				return client, stop, socket, nil
			}
		}
		select {
		case <-ctx.Done():
			stop()
			return nil, nil, "", ctx.Err()
		case <-process.exited:
			stop()
			return nil, nil, "", errors.New("Worker watchdog exited before native readiness")
		case <-tick.C:
		}
	}
}
