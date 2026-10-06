//go:build !windows

package taskterminal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type fakeWindow struct {
	confirms, releases int
}

func (w *fakeWindow) Confirm(_ context.Context, pid int) error {
	if pid <= 0 {
		return ErrWindowIdentity
	}
	w.confirms++
	return nil
}
func (w *fakeWindow) Release() { w.releases++ }

func TestManagedWindowKeepsAcceptedLaunchAfterLostCreationReply(t *testing.T) {
	w := &fakeWindow{}
	l := NewManaged(t.TempDir(), func(ctx context.Context, path string) (Window, error) {
		if err := exec.CommandContext(ctx, "/bin/sh", path).Run(); err != nil {
			t.Fatal(err)
		}
		return w, ErrWindowIdentity
	})
	target := api.TerminalTarget{Runtime: "codex", Binary: "/usr/bin/true", Directory: t.TempDir(), Endpoint: "unix:///tmp/fixture.sock", Thread: "owned"}
	if err := l.Open(t.Context(), "owned", target); !errors.Is(err, ErrWindowIdentity) {
		t.Fatal(err)
	}
	if w.confirms != 1 || l.windows["owned"] != w {
		t.Fatal("uncertain reply lost accepted binding")
	}
}

type focusWindow struct {
	fakeWindow
	confirmErr error
}

func (w *focusWindow) Confirm(ctx context.Context, pid int) error {
	if err := w.fakeWindow.Confirm(ctx, pid); err != nil {
		return err
	}
	return w.confirmErr
}
func TestManagedWindowRecoversOnlyConfirmedLaunchActivation(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		accepted              bool
		launchErr, confirmErr error
		wantErr               error
	}{
		{name: "confirmed activation deferred to controller", accepted: true, launchErr: ErrWindowActivation},
		{name: "receipt absent", launchErr: ErrWindowActivation, wantErr: ErrWindowActivation},
		{name: "identity uncertain", accepted: true, launchErr: ErrWindowActivation, confirmErr: ErrWindowIdentity, wantErr: ErrWindowActivation},
		{name: "creation unknown", accepted: true, launchErr: ErrWindowIdentity, wantErr: ErrWindowIdentity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &focusWindow{fakeWindow: fakeWindow{}, confirmErr: tc.confirmErr}
			launches := 0
			l := NewManaged(t.TempDir(), func(ctx context.Context, path string) (Window, error) {
				launches++
				if tc.accepted {
					if err := exec.CommandContext(ctx, "/bin/sh", path).Run(); err != nil {
						t.Fatal(err)
					}
				}
				return w, tc.launchErr
			})
			target := api.TerminalTarget{Runtime: "codex", Binary: "/usr/bin/true", Directory: t.TempDir(), Endpoint: "unix:///tmp/fixture.sock", Thread: "owned"}
			if err := l.Open(t.Context(), "owned", target); !errors.Is(err, tc.wantErr) {
				t.Fatalf("open: got %v want %v", err, tc.wantErr)
			}
			if launches != 1 {
				t.Fatalf("launches=%d", launches)
			}
			if tc.accepted && (w.confirms != 1 || w.releases != 0 || l.windows["owned"] != w) {
				t.Fatal("accepted window binding was lost")
			}
			if !tc.accepted && (w.confirms != 0 || w.releases != 1 || len(l.windows) != 0) {
				t.Fatal("unconfirmed launch survived")
			}
			l.Close()
		})
	}
}

func TestManagedWindowCancellationRevokesUnstartedWindow(t *testing.T) {
	w := &fakeWindow{}
	ctx, cancel := context.WithCancel(t.Context())
	l := NewManaged(t.TempDir(), func(context.Context, string) (Window, error) { cancel(); return w, nil })
	target := api.TerminalTarget{Runtime: "codex", Binary: "/usr/bin/true", Directory: t.TempDir(), Endpoint: "unix:///tmp/fixture.sock", Thread: "owned"}
	if err := l.Open(ctx, "owned", target); !errors.Is(err, ErrUnconfirmed) {
		t.Fatal(err)
	}
	if w.confirms != 0 || w.releases != 1 || len(l.windows) != 0 {
		t.Fatal("unstarted window survived cancellation")
	}
}

func TestDuplicateLaunchCannotOverwriteAcceptedClient(t *testing.T) {
	w := &fakeWindow{}
	l := NewManaged(t.TempDir(), func(ctx context.Context, path string) (Window, error) {
		if err := exec.CommandContext(ctx, "/bin/sh", path).Run(); err != nil {
			return nil, err
		}
		receipt := func() []string {
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			var out []string
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "accepted-") {
					out = append(out, e.Name())
				}
			}
			return out
		}
		before := receipt()
		if len(before) != 1 {
			t.Fatal("missing atomic receipt")
		}
		if err := exec.CommandContext(ctx, "/bin/sh", path).Run(); err == nil {
			t.Fatal("duplicate script ran")
		}
		if !reflect.DeepEqual(before, receipt()) {
			t.Fatal("duplicate replaced original client identity")
		}
		return w, nil
	})
	target := api.TerminalTarget{Runtime: "codex", Binary: "/usr/bin/true", Directory: t.TempDir(), Endpoint: "unix:///tmp/fixture.sock", Thread: "owned"}
	if err := l.Open(t.Context(), "owned", target); err != nil {
		t.Fatal(err)
	}
	if w.confirms != 1 {
		t.Fatal("winning client was not bound exactly once")
	}
}

// Minimal hosts may attach without any window management capability.
type observationOnly struct{ released bool }

func (w *observationOnly) Confirm(context.Context, int) error { return nil }
func (w *observationOnly) Release()                           { w.released = true }
func TestUnsupportedWindowActionsKeepObservation(t *testing.T) {
	w := &observationOnly{}
	l := NewManaged(t.TempDir(), nil)
	l.windows["owned"] = w

	if err := l.Dismiss(t.Context(), "owned"); !errors.Is(err, ErrWindowUnsupported) {
		t.Fatal(err)
	}
	if w.released || len(l.windows) != 1 {
		t.Fatal("unsupported action erased binding")
	}
	l.Close()
	if !w.released {
		t.Fatal("handle was not released")
	}
}

// Closing does not require the platform to implement screenshots or placement.
type dismissOnly struct {
	observationOnly
	dismissed bool
}

func (w *dismissOnly) Dismiss(context.Context) error { w.dismissed = true; return nil }
func TestDismissCapabilityIndependentOfOtherWindowFeatures(t *testing.T) {
	w := &dismissOnly{}
	l := NewManaged(t.TempDir(), nil)
	l.windows["owned"] = w
	if err := l.Dismiss(t.Context(), "owned"); err != nil || !w.dismissed || !w.released {
		t.Fatal(err, w)
	}
}

type ownedInstance struct{ fakeWindow }

func (*ownedInstance) keepUnconfirmedLaunch() bool { return true }

func TestOwnedInstanceSurvivesMissingReceiptWithoutReplayingScript(t *testing.T) {
	w := &ownedInstance{}
	ctx, cancel := context.WithCancel(t.Context())
	var script string
	l := NewManaged(t.TempDir(), func(_ context.Context, path string) (Window, error) { script = path; cancel(); return w, nil })
	target := api.TerminalTarget{Runtime: "codex", Binary: "/usr/bin/true", Directory: t.TempDir(), Endpoint: "unix:///tmp/fixture.sock", Thread: "owned"}
	if err := l.Open(ctx, "owned", target); !errors.Is(err, ErrUnconfirmed) {
		t.Fatal(err)
	}
	if l.windows["owned"] != w || w.releases != 0 {
		t.Fatal("lost launched app just because its script was not confirmed")
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Fatal("late command was not revoked", err)
	}
	// Keeping the handle allows later close/reconciliation; releasing it is not quit.
	l.Close()
	if w.releases != 1 {
		t.Fatal("owned handle was not released")
	}
}
