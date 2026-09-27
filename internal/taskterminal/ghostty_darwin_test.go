//go:build darwin && cgo

package taskterminal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGhosttyOpenDoesNotRetryDeniedOrUnknownCreation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   error
	}{
		{"accepted", 1, nil},
		{"unsupported", -1, ErrWindowUnsupported},
		{"denied", -2, ErrWindowPermission},
		{"unknown", -3, nil},
		{"focusFailed", -4, ErrWindowActivation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			polls := 0
			pid, err := waitGhosttyOpen(t.Context(), func() (int, int, int64) {
				polls++
				return tc.status, 123, -1712
			})
			if pid != 123 || polls != 1 {
				t.Fatalf("identity/retry: pid=%d polls=%d", pid, polls)
			}
			if tc.status == -3 {
				if err == nil {
					t.Fatal("unknown outcome became success")
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
		})
	}
}

func TestGhosttyInteractiveCommandPreservesTerminalColorsAndQuotes(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "space ' $(not-executed)")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "Caelis Bot.command")
	if err := os.WriteFile(path, []byte("printf '%s|%s|%s' \"${NO_COLOR-unset}\" \"$TERM\" \"$COLORTERM\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/sh", "-c", ghosttyCommand(path))
	command.Env = []string{"PATH=/usr/bin:/bin", "NO_COLOR=1", "TERM=xterm-ghostty", "COLORTERM=truecolor"}
	out, err := command.CombinedOutput()
	if err != nil || string(out) != "unset|xterm-ghostty|truecolor" {
		t.Fatal(string(out), err)
	}
}

func TestGhosttyCancellationKeepsLaunchedIdentityForReceiptReconciliation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	pid, err := waitGhosttyOpen(ctx, func() (int, int, int64) {
		cancel()
		return 0, 123, 0
	})
	if pid != 123 || !errors.Is(err, context.Canceled) {
		t.Fatal(pid, err)
	}
}
