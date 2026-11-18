package codex

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
	"github.com/caelis-labs/caelis-bot/internal/sharedruntime"
)

var errSharedStartFailed = errors.New("shared Codex daemon start unavailable or failed")

// The original standard endpoint is the authority for CODEX_HOME, including
// bindings saved before recovery support. Never turn a private socket into a
// new daemon or follow the current process's possibly different CODEX_HOME.
func sharedHome(socket string) string {
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket || filepath.Base(socket) != "app-server-control.sock" || filepath.Base(filepath.Dir(socket)) != "app-server-control" {
		return ""
	}
	return filepath.Dir(filepath.Dir(socket))
}

// A reachable, incompatible protocol or a local resource/permission failure
// needs a different remedy. Other transport failures may reflect an unready
// daemon; its public start operation arbitrates reuse, waiting and startup.
func canStartSharedDaemon(err error) bool {
	return err != errExistingServer && !incompatibleProtocol(err) && !resourceExhausted(err) && !errors.Is(err, os.ErrPermission)
}

func recoverShared(ctx context.Context, opts Options, home string) error {
	return sharedruntime.Shared.Recover(ctx, "codex:"+home, func(ctx context.Context) error {
		binary, err := runtimeBinary(opts.Binary)
		if err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, binary, "app-server", "daemon", "start")
		runtimeenv.BackgroundCommand(cmd)
		cmd.Dir = opts.Directory
		cmd.Env = append(runtimeenv.Clean(cmd.Environ()), "CODEX_HOME="+home)
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		if err := cmd.Run(); err != nil {
			return errSharedStartFailed
		}
		return nil
	})
}
