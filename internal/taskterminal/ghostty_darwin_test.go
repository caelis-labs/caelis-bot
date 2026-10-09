//go:build darwin && cgo

package taskterminal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

type pendingGhosttyFixture struct{ *documentFixture }

func (w *pendingGhosttyFixture) Dismiss(ctx context.Context) error {
	if _, err := w.Observe(ctx); err != nil {
		return err
	}
	w.set(func(o *WindowObservation) { o.State = WindowClosed })
	return nil
}

func TestGhosttyCancellationBeforePIDRetainsOwnerAndReusesLateInstance(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w := &pendingGhosttyFixture{newDocumentFixture()}
	w.read = func() error { return ErrWindowObservationPending }
	w.open = func(ctx context.Context, path string) error { return exec.CommandContext(ctx, "/bin/sh", path).Run() }
	launches := 0
	var buffered []byte
	m := testManager(t, func(ctx context.Context, path string) (Window, error) {
		launches++
		var err error
		buffered, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return finishGhosttyOpen(ctx, w, func() (int, int, int64) {
			cancel()
			return 0, 0, 0 // LaunchServices has not returned any PID yet.
		})
	})
	if err := m.Click(ctx, "owned"); !errors.Is(err, context.Canceled) || w.releases != 0 {
		t.Fatal("cancellation discarded pending owner", err, w.releases)
	}
	if err := m.Dismiss(t.Context(), "owned"); !errors.Is(err, ErrWindowObservationPending) || w.releases != 0 {
		t.Fatal("pending native creation was reported as closed", err, w.releases)
	}
	e, _ := m.entry("owned")
	e.controller.timeout, e.controller.interval = 5*time.Millisecond, time.Millisecond
	if err := m.Click(t.Context(), "owned"); !errors.Is(err, ErrWindowUnsettled) || launches != 1 {
		t.Fatal("pending callback allowed replacement", err, launches)
	}
	w.mu.Lock()
	w.read = nil // A late callback has now resolved the same native owner.
	w.mu.Unlock()
	e.controller.timeout = 0
	if err := m.Click(t.Context(), "owned"); err != nil || launches != 1 || w.reuses != 1 || w.releases != 0 {
		t.Fatal("late instance was not reused", err, launches, w.reuses, w.releases)
	}
	cmd := exec.Command("/bin/sh")
	cmd.Stdin = strings.NewReader(string(buffered))
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "expired") {
		t.Fatal("cancelled script executed", string(out), err)
	}
	if err := m.Dismiss(t.Context(), "owned"); err != nil || w.releases != 1 {
		t.Fatal("late owner was not dismissible", err, w.releases)
	}
}

func TestGhosttyOnlyPreLaunchCapabilityRejectionDiscardsOwner(t *testing.T) {
	for _, status := range []int{-1, -2, -3, 1} {
		w := newDocumentFixture()
		owner, err := finishGhosttyOpen(t.Context(), w, func() (int, int, int64) { return status, 123, 0 })
		if status == -1 {
			if owner != nil || w.releases != 1 || !errors.Is(err, ErrLaunchNotSubmitted) || !errors.Is(err, ErrWindowUnsupported) {
				t.Fatal("unsupported path cannot safely fall back", owner, err, w.releases)
			}
		} else if owner != w || w.releases != 0 || errors.Is(err, ErrLaunchNotSubmitted) {
			t.Fatal("submitted outcome lost its owner", status, owner, err, w.releases)
		}
	}
}
