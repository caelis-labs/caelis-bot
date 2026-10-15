package caelisruntime

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/i18n"
	"github.com/caelis-labs/caelis-bot/internal/runtimeenv"
)

// The official installer starts download children. On cancellation, terminate
// its private process group; CommandContext's default kills only /bin/sh and
// can leave curl writing an unowned archive after the UI reports failure.
// Shared Host service commands deliberately do not use this process group.
func runInstaller(ctx context.Context, loc i18n.Locale, script, releaseBaseURL string) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 3 * time.Second
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	for _, entry := range runtimeenv.Clean(os.Environ()) {
		if !strings.HasPrefix(entry, "CAELIS_RELEASES_BASE_URL=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "CAELIS_RELEASES_BASE_URL="+releaseBaseURL)
	return cmd.Run()
}
