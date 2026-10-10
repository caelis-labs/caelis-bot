package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
	"github.com/caelis-labs/caelis-bot/internal/sharedruntime"
)

var (
	errSharedStateUnknown = errors.New("shared Codex service missing or explicitly stopped; process state unconfirmed")
	errSharedStartFailed  = errors.New("shared Codex daemon start unavailable or failed")
)

// Public startup can remove a stale record before failing. Keep the confirmed
// absence through retries, but clear it as soon as the original socket opens.
// This is in-memory recovery evidence, never a lifecycle ownership lease.
var sharedRecoveryEvidence sync.Map

// The original standard endpoint is the authority for CODEX_HOME, including
// bindings saved before recovery support. Never turn a private socket into a
// new daemon or follow the current process's possibly different CODEX_HOME.
func sharedHome(socket string) string {
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket || filepath.Base(socket) != "app-server-control.sock" || filepath.Base(filepath.Dir(socket)) != "app-server-control" {
		return ""
	}
	return filepath.Dir(filepath.Dir(socket))
}

func recoverShared(ctx context.Context, opts Options) error {
	home := sharedHome(opts.Socket)
	if home == "" {
		return errSharedStateUnknown
	}
	return sharedruntime.Shared.Recover(ctx, "codex:"+home, func(ctx context.Context) error {
		// daemon version only succeeds for a ready service. A failed probe is
		// not an absence signal. Require retained native PID evidence and ESRCH;
		// live/reused PIDs, permissions, empty startup reservations and unknown
		// formats all fail closed. Native lifecycle owns locks and final races.
		_, confirmed := sharedRecoveryEvidence.Load(home)
		if !sharedDaemonAbsent(home, confirmed) {
			sharedRecoveryEvidence.Delete(home)
			return errSharedStateUnknown
		}
		sharedRecoveryEvidence.Store(home, true)
		binary, err := runtimeBinary(opts.Binary)
		if err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, binary, "app-server", "daemon", "start")
		cmd.Dir = opts.Directory
		cmd.Env = append(runtimeenv.Clean(cmd.Environ()), "CODEX_HOME="+home)
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Run(); err != nil {
			return errSharedStartFailed
		}
		return nil
	})
}

func sharedDaemonGone(home string) bool {
	return sharedDaemonAbsent(home, false)
}

func sharedDaemonAbsent(home string, confirmed bool) bool {
	// Both native layouts are read-only hints, never process-control authority.
	// An explicit daemon stop removes its record; no record means no auto-start.
	seen := false
	for _, name := range []string{"daemon.pid", "app-server.pid", "daemon-updater.pid", "app-server-updater.pid"} {
		path := filepath.Join(home, "app-server-daemon", name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
		f, err := os.Open(path)
		if err != nil {
			return false
		}
		b, err := io.ReadAll(io.LimitReader(f, 65537))
		_ = f.Close()
		var record struct {
			PID int `json:"pid"`
		}
		if err != nil || len(b) > 65536 || len(bytes.TrimSpace(b)) == 0 || json.Unmarshal(b, &record) != nil || record.PID <= 0 || record.PID > math.MaxInt32 || !processGone(record.PID) {
			return false
		}
		if name == "daemon.pid" || name == "app-server.pid" {
			seen = true
		}
	}
	return seen || confirmed
}

func missingSocket(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)
}
