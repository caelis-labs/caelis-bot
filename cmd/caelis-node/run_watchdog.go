//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"io"
	"os"
)

// The helper is launched only with an inherited private descriptor by the native
// foreground owner. It starts no service and accepts no argv payload or PID.
func runOwnedWatchdog(ctx context.Context, args []string, _ io.Writer) error {
	if len(args) != 0 {
		return errors.New("owned watchdog accepts only its inherited private channel")
	}
	f := os.NewFile(3, "owned-watchdog-control")
	if f == nil {
		return errors.New("owned watchdog control unavailable")
	}
	return codex.RunSupervisedRuntime(ctx, f)
}
