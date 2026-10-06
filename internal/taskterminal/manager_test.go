//go:build !windows

package taskterminal

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testManager(t *testing.T, open func(context.Context, string) (Window, error)) *WindowManager {
	t.Helper()
	m := NewWindowManager(t.TempDir(), open, func(context.Context, string) (api.TerminalTarget, error) {
		return api.TerminalTarget{Runtime: "codex", Binary: "/usr/bin/true", Directory: t.TempDir(), Endpoint: "unix:///tmp/fixture.sock", Thread: "owned"}, nil
	}, nil, nil)
	t.Cleanup(m.Close)
	return m
}
func TestManagerSlowWindowDoesNotBlockOtherTask(t *testing.T) {
	m := testManager(t, nil)
	a, _ := m.entry("a")
	b, _ := m.entry("b")
	ca, wa := controllerFixture(WindowBackground)
	cb, wb := controllerFixture(WindowBackground)
	immediateWindow(wb)
	a.controller = ca
	b.controller = cb
	done := asyncClick(t, ca)
	nextCommand(t, wa, FocusWindow)
	if err := m.Click(t.Context(), "b"); err != nil {
		t.Fatal(err)
	}
	ca.stop(false)
	_ = result(t, done)
}
func TestManagerOneLaunchWithClicksDuringReceipt(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var launches atomic.Int32
	_, w := controllerFixture(WindowForeground)
	immediateWindow(w)
	m := testManager(t, func(ctx context.Context, path string) (Window, error) {
		launches.Add(1)
		close(entered)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
		}
		return w, exec.CommandContext(ctx, "/bin/sh", path).Run()
	})
	done := make(chan error, 1)
	go func() { done <- m.Click(t.Context(), "a") }()
	<-entered
	// Initial click means show. Two more clicks still mean show, never two clients.
	for range 2 {
		if err := m.Click(t.Context(), "a"); err != nil {
			t.Fatal(err)
		}
	}
	close(release)
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
	if launches.Load() != 1 || len(w.calls()) != 0 {
		t.Fatal("duplicate launch or wrong final destination", launches.Load(), w.calls())
	}
}
func TestManagerUnknownCreationIsNotRetried(t *testing.T) {
	for _, outcome := range []error{ErrWindowIdentity, context.Canceled, ErrWindowPermission, ErrWindowUnsupported, ErrWindowOpenCancelled} {
		t.Run(outcome.Error(), func(t *testing.T) {
			launches := 0
			m := testManager(t, func(context.Context, string) (Window, error) { launches++; return nil, outcome })
			for range 2 {
				if err := m.Click(t.Context(), "a"); !errors.Is(err, outcome) {
					t.Fatal(err)
				}
			}
			if launches != 1 {
				t.Fatal("uncertain creation retried", launches)
			}
		})
	}
}

func TestManagerRetriesAfterTerminalConfigurationRepaired(t *testing.T) {
	for _, cause := range []error{errors.New("preferred terminal unavailable"), ErrUnsupportedDefault} {
		t.Run(cause.Error(), func(t *testing.T) {
			configured, launches := false, 0
			m := testManager(t, func(ctx context.Context, path string) (Window, error) {
				if !configured {
					return nil, NotLaunched(cause)
				}
				launches++
				w := newDocumentFixture()
				return w, exec.CommandContext(ctx, "/bin/sh", path).Run()
			})
			if err := m.Click(t.Context(), "owned"); !errors.Is(err, cause) || !errors.Is(err, ErrLaunchNotSubmitted) || launches != 0 {
				t.Fatal("missing pre-launch evidence/cause", err, launches)
			}
			configured = true
			if err := m.Click(t.Context(), "owned"); err != nil || launches != 1 {
				t.Fatal("repaired terminal remained blocked", err, launches)
			}
		})
	}
}

func TestManagerRetriesAfterLaunchDirectoryRepaired(t *testing.T) {
	launches := 0
	m := testManager(t, func(ctx context.Context, path string) (Window, error) {
		launches++
		_, w := controllerFixture(WindowForeground)
		immediateWindow(w)
		return w, exec.CommandContext(ctx, "/bin/sh", path).Run()
	})
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	m.directory = filepath.Join(blocked, "launches")
	if err := m.Click(t.Context(), "owned"); err == nil || launches != 0 {
		t.Fatal("expected failure before terminal launch", err, launches)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := m.Click(t.Context(), "owned"); err != nil || launches != 1 {
		t.Fatal("repaired directory remained blocked", err, launches)
	}
}
func TestManagerBindingGenerationsFencePreviousCompletions(t *testing.T) {
	m := testManager(t, nil)
	var revisions []uint64
	m.changed = func(_ string, e WindowEvent) { revisions = append(revisions, e.Revision) }
	a, _ := m.entry("a")
	a.controller.notify(WindowIdle, nil)
	if err := m.Dismiss(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	b, _ := m.entry("a")
	b.controller.notify(WindowReconciling, nil)
	newRevision := revisions[len(revisions)-1]
	a.controller.notify(WindowIdle, nil)
	if !(revisions[0] < newRevision && revisions[len(revisions)-1] < newRevision) {
		t.Fatal(revisions)
	}
}
func TestControllerConcurrentRequestsRace(t *testing.T) {
	c, w := controllerFixture(WindowBackground)
	immediateWindow(w)
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if err := c.request(t.Context(), nil, nil); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	c.mu.Lock()
	running := c.running
	completed, requested := c.completedRevision, c.revision
	c.mu.Unlock()
	if running || completed != requested {
		t.Fatal("lost last request", running, completed, requested)
	}
}
func TestManagerClosedWindowReleasesOldBindingExactlyOnce(t *testing.T) {
	var launches atomic.Int32
	m := testManager(t, func(ctx context.Context, path string) (Window, error) {
		launches.Add(1)
		_, w := controllerFixture(WindowForeground)
		immediateWindow(w)
		return w, exec.CommandContext(ctx, "/bin/sh", path).Run()
	})
	if err := m.Click(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	entry, _ := m.entry("a")
	entry.launcher.mu.Lock()
	old := entry.launcher.windows["a"].(*observedWindow)
	entry.launcher.mu.Unlock()
	old.set(func(o *WindowObservation) { o.State = WindowClosed })
	if err := m.Click(t.Context(), "a"); err != nil {
		t.Fatal(err)
	}
	if launches.Load() != 2 {
		t.Fatal(launches.Load())
	}
	// An old binding wrapper cannot mutate a freshly attached replacement.
	stale := &controlledBinding{launcher: entry.launcher, id: "a", window: old}
	if err := stale.Apply(t.Context(), CollapseWindow); !errors.Is(err, ErrWindowIdentity) {
		t.Fatal(err)
	}
}
func TestControllerTimeoutDoesNotRetryMutation(t *testing.T) {
	c, w := controllerFixture(WindowBackground)
	c.timeout = 10 * time.Millisecond
	if err := c.request(t.Context(), nil, nil); !errors.Is(err, ErrWindowUnsettled) {
		t.Fatal(err)
	}
	if len(w.calls()) != 1 {
		t.Fatal(w.calls())
	}
}

func TestManagerRetainsConfirmedBindingAcrossLaunchAndControlFailures(t *testing.T) {
	for _, launchErr := range []error{ErrWindowActivation, ErrWindowIdentity} {
		t.Run(launchErr.Error(), func(t *testing.T) {
			_, w := controllerFixture(WindowBackground)
			immediateWindow(w)
			launches := 0
			m := testManager(t, func(ctx context.Context, path string) (Window, error) {
				launches++
				if err := exec.CommandContext(ctx, "/bin/sh", path).Run(); err != nil {
					return nil, err
				}
				return w, launchErr
			})
			err := m.Click(t.Context(), "owned")
			if errors.Is(launchErr, ErrWindowActivation) {
				if err != nil || len(w.calls()) != 1 || w.calls()[0] != FocusWindow {
					t.Fatal("confirmed creation must use controller focus", err, w.calls())
				}
			} else if !errors.Is(err, ErrWindowIdentity) {
				t.Fatal(err)
			}
			w.mu.Lock()
			w.read = func() error { return ErrWindowPermission }
			w.mu.Unlock()
			if err := m.Click(t.Context(), "owned"); !errors.Is(err, ErrWindowPermission) {
				t.Fatal(err)
			}
			w.mu.Lock()
			w.read = nil
			w.observation.State = WindowBackground
			w.mu.Unlock()
			if err := m.Click(t.Context(), "owned"); err != nil || launches != 1 {
				t.Fatal("failure reconnected existing client", err, launches)
			}
		})
	}
}

type unconfirmedObservedWindow struct{ *observedWindow }

func (*unconfirmedObservedWindow) Confirm(context.Context, int) error { return ErrWindowIdentity }

func TestManagerReopensAfterProvenClosureOfUnconfirmedClient(t *testing.T) {
	_, first := controllerFixture(WindowUnknown)
	_, next := controllerFixture(WindowForeground)
	immediateWindow(next)
	launches := 0
	m := testManager(t, func(ctx context.Context, path string) (Window, error) {
		launches++
		if err := exec.CommandContext(ctx, "/bin/sh", path).Run(); err != nil {
			return nil, err
		}
		if launches == 1 {
			return &unconfirmedObservedWindow{first}, nil
		}
		return next, nil
	})
	if err := m.Click(t.Context(), "owned"); !errors.Is(err, ErrWindowIdentity) {
		t.Fatal(err)
	}
	first.read = func() error { return ErrWindowIdentity }
	if err := m.Click(t.Context(), "owned"); !errors.Is(err, ErrWindowIdentity) || launches != 1 {
		t.Fatal("replayed unknown launch", err, launches)
	}
	first.read = nil
	first.observation = WindowObservation{State: WindowClosed, Settled: true}
	if err := m.Click(t.Context(), "owned"); err != nil || launches != 2 {
		t.Fatal("closed client blocked reconnect", err, launches)
	}
}

type reconnectWindow struct {
	*observedWindow
	releases, dismissals int
}

func (w *reconnectWindow) Release() { w.releases++ }
func (w *reconnectWindow) Dismiss(context.Context) error {
	w.dismissals++
	w.set(func(o *WindowObservation) { o.State = WindowClosed })
	return nil
}

func TestManagerClientExitRebindsWithoutClosingOldApp(t *testing.T) {
	for _, state := range []WindowState{WindowForeground, WindowBackground, WindowCollapsed} {
		t.Run(string(state), func(t *testing.T) {
			_, oldObserved := controllerFixture(WindowForeground)
			old := &reconnectWindow{observedWindow: oldObserved}
			_, next := controllerFixture(WindowForeground)
			immediateWindow(next)
			launches := 0
			m := testManager(t, func(ctx context.Context, path string) (Window, error) {
				launches++
				if err := exec.CommandContext(ctx, "/bin/sh", path).Run(); err != nil {
					return nil, err
				}
				if launches == 1 {
					return old, nil
				}
				return next, nil
			})
			if err := m.Click(t.Context(), "owned"); err != nil {
				t.Fatal(err)
			}
			old.set(func(o *WindowObservation) { o.State, o.ClientEnded = state, true })
			if err := m.Click(t.Context(), "owned"); err != nil {
				t.Fatal(err)
			}
			if launches != 2 || old.releases != 1 || old.dismissals != 0 || len(old.calls()) != 0 {
				t.Fatal("reconnect mutated old app or failed to replace binding", launches, old.releases, old.dismissals, old.calls())
			}
			o, _ := old.Observe(t.Context())
			if o.State != state {
				t.Fatal("old app changed", o)
			}
			entry, _ := m.entry("owned")
			stale := &controlledBinding{launcher: entry.launcher, id: "owned", window: old}
			if err := stale.Apply(t.Context(), CollapseWindow); !errors.Is(err, ErrWindowIdentity) {
				t.Fatal(err)
			}
			if err := m.Click(t.Context(), "owned"); err != nil {
				t.Fatal(err)
			}
			if launches != 2 || len(next.calls()) != 1 || next.calls()[0] != CollapseWindow {
				t.Fatal("next click did not control new app", launches, next.calls())
			}
			if err := m.Dismiss(t.Context(), "owned"); !errors.Is(err, ErrWindowUnsupported) {
				t.Fatal(err)
			}
			if old.dismissals != 0 {
				t.Fatal("dismiss reached detached app")
			}
		})
	}
}

func TestManagerUnknownReplacementDoesNotRetryOrReclaimOldApp(t *testing.T) {
	_, w := controllerFixture(WindowForeground)
	old := &reconnectWindow{observedWindow: w}
	launches := 0
	m := testManager(t, func(ctx context.Context, path string) (Window, error) {
		launches++
		if launches == 1 {
			return old, exec.CommandContext(ctx, "/bin/sh", path).Run()
		}
		return nil, ErrWindowIdentity // Creation may have occurred without an owner.
	})
	if err := m.Click(t.Context(), "owned"); err != nil {
		t.Fatal(err)
	}
	old.set(func(o *WindowObservation) { o.ClientEnded = true })
	for range 2 {
		if err := m.Click(t.Context(), "owned"); !errors.Is(err, ErrWindowIdentity) {
			t.Fatal(err)
		}
	}
	if launches != 2 || old.releases != 1 || old.dismissals != 0 || len(old.calls()) != 0 {
		t.Fatal("unknown replacement replayed or changed old app", launches, old)
	}
}

type consentWindow struct {
	*observedWindow
	entered  chan struct{}
	answer   chan error
	requests atomic.Int32
}

func (w *consentWindow) Dismiss(ctx context.Context) error {
	w.requests.Add(1)
	w.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-w.answer:
		if err == nil {
			w.set(func(o *WindowObservation) { o.State = WindowClosed })
		}
		return err
	}
}
func TestManagerCloseConsentLifecycle(t *testing.T) {
	for _, action := range []string{"confirm", "cancel", "click", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			_, observed := controllerFixture(WindowForeground)
			immediateWindow(observed)
			w := &consentWindow{observedWindow: observed, entered: make(chan struct{}, 2), answer: make(chan error, 1)}
			m := testManager(t, func(ctx context.Context, path string) (Window, error) {
				return w, exec.CommandContext(ctx, "/bin/sh", path).Run()
			})
			if err := m.Click(t.Context(), "owned"); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- m.Dismiss(t.Context(), "owned") }()
			select {
			case <-w.entered:
			case <-time.After(time.Second):
				t.Fatal("close did not start")
			}
			if err := m.Dismiss(t.Context(), "owned"); !errors.Is(err, ErrWindowClosePending) || w.requests.Load() != 1 {
				t.Fatal("duplicate quit", err, w.requests.Load())
			}
			switch action {
			case "confirm":
				w.answer <- nil
				if err := result(t, done); err != nil {
					t.Fatal(err)
				}
				m.mu.Lock()
				retained := m.entries["owned"] != nil
				m.mu.Unlock()
				if retained {
					t.Fatal("confirmed exit retained binding")
				}
			case "cancel":
				w.answer <- ErrWindowCloseCancelled
				if err := result(t, done); !errors.Is(err, ErrWindowCloseCancelled) {
					t.Fatal(err)
				}
				if err := m.Click(t.Context(), "owned"); err != nil {
					t.Fatal("cancel left card blocked", err)
				}
				if len(w.calls()) != 1 || w.calls()[0] != CollapseWindow {
					t.Fatal(w.calls())
				}
			case "click":
				if err := m.Click(t.Context(), "owned"); err != nil {
					t.Fatal(err)
				}
				if err := result(t, done); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if len(w.calls()) != 0 {
					t.Fatal("click hid pending confirmation", w.calls())
				}
			case "shutdown":
				m.Close()
				if err := result(t, done); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			}
		})
	}
}
