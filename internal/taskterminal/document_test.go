//go:build !windows

package taskterminal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type documentFixture struct {
	*observedWindow
	docMu            sync.Mutex
	complete         bool
	result           error
	open             func(context.Context, string) error
	reuses, releases int
}

type dismissibleDocumentFixture struct {
	*documentFixture
	dismissErr error
}

func (w *dismissibleDocumentFixture) Dismiss(context.Context) error {
	if w.dismissErr != nil {
		return w.dismissErr
	}
	w.set(func(o *WindowObservation) { o.State = WindowClosed })
	return nil
}

func (w *documentFixture) keepUnconfirmedLaunch() bool { return true }
func (w *documentFixture) Release()                    { w.releases++ }
func (w *documentFixture) Confirm(context.Context, int) error {
	w.set(func(o *WindowObservation) { o.ClientEnded = false })
	return nil
}
func (w *documentFixture) OpenDocument(ctx context.Context, path string) error {
	w.reuses++
	return w.open(ctx, path)
}
func (w *documentFixture) DocumentResult(context.Context) (bool, error) {
	w.docMu.Lock()
	defer w.docMu.Unlock()
	return w.complete, w.result
}
func (w *documentFixture) reply(complete bool, err error) {
	w.docMu.Lock()
	defer w.docMu.Unlock()
	w.complete, w.result = complete, err
}
func newDocumentFixture() *documentFixture {
	_, o := controllerFixture(WindowForeground)
	immediateWindow(o)
	return &documentFixture{observedWindow: o, complete: true}
}

func TestRevokedDocumentRemainsReadableUntilOwnedInstanceExits(t *testing.T) {
	root := t.TempDir()
	w := &dismissibleDocumentFixture{documentFixture: newDocumentFixture()}
	w.reply(false, nil)
	first := make(chan string, 1)
	launches := 0
	l := NewManaged(filepath.Join(root, "Terminal"), func(_ context.Context, path string) (Window, error) {
		launches++
		first <- path
		return w, nil // Native creation replied before the shell opened the script.
	})
	target := api.TerminalTarget{Runtime: "setup", Binary: "/usr/bin/true", Directory: root}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.Open(ctx, "owned", target) }()
	path := <-first
	cancel()
	if err := <-done; !errors.Is(err, ErrUnconfirmed) {
		t.Fatal(err)
	}
	if l.windows["owned"] != w || w.releases != 0 {
		t.Fatal("uncertain native owner was discarded")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("late shell lost its entry point", err)
	}
	if out, err := exec.Command("/bin/sh", path).CombinedOutput(); err == nil || !strings.Contains(string(out), "expired") {
		t.Fatal("revoked command attached", string(out), err)
	}
	w.open = func(ctx context.Context, next string) error {
		w.reply(true, nil)
		return exec.CommandContext(ctx, "/bin/sh", next).Run()
	}
	if err := l.Open(t.Context(), "owned", target); err != nil {
		t.Fatal("explicit retry failed", err)
	}
	if launches != 1 || l.windows["owned"] != w {
		t.Fatal("explicit retry replaced the original native owner", launches)
	}
	w.dismissErr = ErrWindowCloseCancelled
	if err := l.Dismiss(t.Context(), "owned"); !errors.Is(err, ErrWindowCloseCancelled) {
		t.Fatal("cancelled close unexpectedly cleaned the owner", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("cancelled close removed the late shell path", err)
	}
	w.dismissErr = nil
	if err := l.Dismiss(t.Context(), "owned"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("proven app exit did not clean retained attempt", err)
	}
}

func TestUnknownSubmittedOpenRetainsOwnerAndReadableRevokedScript(t *testing.T) {
	root := t.TempDir()
	w := &dismissibleDocumentFixture{documentFixture: newDocumentFixture()}
	var path string
	unknown := errors.New("synthetic native reply unknown")
	l := NewManaged(filepath.Join(root, "Terminal"), func(_ context.Context, command string) (Window, error) {
		path = command
		return w, unknown
	})
	target := api.TerminalTarget{Runtime: "setup", Binary: "/usr/bin/true", Directory: root}
	if err := l.Open(t.Context(), "owned", target); !errors.Is(err, unknown) {
		t.Fatal(err)
	}
	if l.windows["owned"] != w || w.releases != 0 {
		t.Fatal("unknown submitted result lost its native owner")
	}
	if out, err := exec.Command("/bin/sh", path).CombinedOutput(); err == nil || !strings.Contains(string(out), "expired") {
		t.Fatal("delayed unknown request could not safely read", string(out), err)
	}
	if err := l.Dismiss(t.Context(), "owned"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("closed native owner left attempt directory", err)
	}
}

func TestDismissKeepsUnreadCommandForPreviouslySpawnedShell(t *testing.T) {
	root := t.TempDir()
	w := &dismissibleDocumentFixture{documentFixture: newDocumentFixture()}
	var path string
	l := NewManaged(filepath.Join(root, "Terminal"), func(_ context.Context, command string) (Window, error) {
		path = command
		return w, errors.New("synthetic native outcome unknown")
	})
	target := api.TerminalTarget{Runtime: "setup", Binary: "/usr/bin/true", Directory: root}
	if err := l.Open(t.Context(), "owned", target); err == nil {
		t.Fatal("unknown native result became success")
	}
	if err := l.Dismiss(t.Context(), "owned"); err != nil {
		t.Fatal(err)
	}
	// GUI exit alone does not prove a child shell has opened its pathname.
	if out, err := exec.Command("/bin/sh", path).CombinedOutput(); err == nil || !strings.Contains(string(out), "expired") {
		t.Fatal("late child could not safely read after GUI exit", string(out), err)
	}
	if err := l.cleanReadAttempts("owned"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read rejected attempt was not pruned", err)
	}
}
func TestReconnectReusesOwnedApplication(t *testing.T) {
	w := newDocumentFixture()
	launches := 0
	w.open = func(ctx context.Context, path string) error { return exec.CommandContext(ctx, "/bin/sh", path).Run() }
	m := testManager(t, func(ctx context.Context, path string) (Window, error) { launches++; return w, w.open(ctx, path) })
	if err := m.Click(t.Context(), "owned"); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		w.set(func(o *WindowObservation) { o.ClientEnded = true })
		if err := m.Click(t.Context(), "owned"); err != nil {
			t.Fatal(err)
		}
	}
	if launches != 1 || w.reuses != 10 || w.releases != 0 {
		t.Fatal(launches, w.reuses, w.releases)
	}
}
func TestDocumentCancellationAndSuccessWithoutExecutionRemainRetryable(t *testing.T) {
	for _, replyErr := range []error{ErrWindowOpenCancelled, nil} {
		t.Run(map[bool]string{true: "cancel reply", false: "success without execution"}[replyErr != nil], func(t *testing.T) {
			w := newDocumentFixture()
			w.result = replyErr
			var buffered []byte
			launches := 0
			m := testManager(t, func(ctx context.Context, path string) (Window, error) {
				launches++
				var err error
				buffered, err = os.ReadFile(path)
				return w, err
			})
			e, _ := m.entry("owned")
			e.launcher.receiptGrace = time.Millisecond
			want := ErrWindowNotConnected
			if replyErr != nil {
				want = replyErr
			}
			if err := m.Click(t.Context(), "owned"); !errors.Is(err, want) {
				t.Fatal(err)
			}
			if !e.launcher.reconnect["owned"] {
				t.Fatal("unclaimed command was not fenced for retry")
			}
			w.open = func(ctx context.Context, path string) error {
				w.reply(true, nil)
				return exec.CommandContext(ctx, "/bin/sh", path).Run()
			}
			if err := m.Click(t.Context(), "owned"); err != nil {
				t.Fatal(err)
			}
			if launches != 1 || w.reuses != 1 || w.releases != 0 {
				t.Fatal("retry created/dropped application", launches, w.reuses, w.releases)
			}
			cmd := exec.Command("/bin/sh")
			cmd.Stdin = strings.NewReader(string(buffered))
			if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "expired") {
				t.Fatal("cancelled script executed", string(out), err)
			}
		})
	}
}
func TestPendingDocumentClicksDoNotStackOrCollapse(t *testing.T) {
	w := newDocumentFixture()
	w.complete = false
	pathCh := make(chan string, 1)
	m := testManager(t, func(_ context.Context, path string) (Window, error) { pathCh <- path; return w, nil })
	e, _ := m.entry("owned")
	e.controller.interval = time.Millisecond
	done := make(chan error, 1)
	go func() { done <- m.Click(t.Context(), "owned") }()
	path := <-pathCh
	for {
		e.launcher.waitMu.Lock()
		ready := e.launcher.waitCancel != nil
		e.launcher.waitMu.Unlock()
		if ready {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := m.Click(t.Context(), "owned"); err != nil {
		t.Fatal(err)
	}
	if w.reuses != 0 {
		t.Fatal("second document sent during dialog")
	}
	if err := exec.Command("/bin/sh", path).Run(); err != nil {
		t.Fatal(err)
	}
	w.reply(true, nil)
	if err := result(t, done); err != nil {
		t.Fatal(err)
	}
	if len(w.calls()) != 0 {
		t.Fatal("confirmation click collapsed new terminal", w.calls())
	}
}
func TestClosedApplicationEndsReceiptWait(t *testing.T) {
	w := newDocumentFixture()
	w.complete = false
	m := testManager(t, func(_ context.Context, path string) (Window, error) {
		w.set(func(o *WindowObservation) { o.State = WindowClosed })
		return w, nil
	})
	if err := m.Click(t.Context(), "owned"); !errors.Is(err, ErrWindowOpenCancelled) {
		t.Fatal(err)
	}
	e, _ := m.entry("owned")
	e.controller.mu.Lock()
	running := e.controller.running
	e.controller.mu.Unlock()
	if running {
		t.Fatal("closed app left controller waiting")
	}
}

func TestClickAfterDocumentReplyRetiresUnexecutedRequestBeforeReuse(t *testing.T) {
	w := newDocumentFixture()
	w.complete = false
	pathCh := make(chan string, 1)
	launches := 0
	m := testManager(t, func(_ context.Context, path string) (Window, error) {
		launches++
		pathCh <- path
		return w, nil
	})
	e, _ := m.entry("owned")
	e.launcher.receiptGrace = time.Minute
	done := make(chan error, 1)
	go func() { done <- m.Click(t.Context(), "owned") }()
	path := <-pathCh
	buffered, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for {
		e.launcher.waitMu.Lock()
		ready := e.launcher.waitCancel != nil
		e.launcher.waitMu.Unlock()
		if ready {
			break
		}
		time.Sleep(time.Millisecond)
	}
	w.open = func(ctx context.Context, next string) error {
		// Retry must wait until the old token is revoked, even if the terminal
		// buffered the complete script before replying without execution.
		stale := exec.CommandContext(ctx, "/bin/sh")
		stale.Stdin = strings.NewReader(string(buffered))
		if out, err := stale.CombinedOutput(); err == nil || !strings.Contains(string(out), "expired") {
			return errors.New("old request was still executable at retry")
		}
		return exec.CommandContext(ctx, "/bin/sh", next).Run()
	}
	w.reply(true, nil)
	if err := m.Click(t.Context(), "owned"); err != nil {
		t.Fatal(err)
	}
	if err := result(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if launches != 1 || w.reuses != 1 || w.releases != 0 {
		t.Fatal(launches, w.reuses, w.releases)
	}
}

func TestReconnectReplacesOnlyProvenExitedOriginalApplication(t *testing.T) {
	for _, exits := range []bool{true, false} {
		t.Run(map[bool]string{true: "app exits during reuse", false: "user cancels in live app"}[exits], func(t *testing.T) {
			w := newDocumentFixture()
			next := newDocumentFixture()
			launches := 0
			m := testManager(t, func(ctx context.Context, path string) (Window, error) {
				launches++
				owner := w
				if launches == 2 {
					owner = next
				}
				return owner, exec.CommandContext(ctx, "/bin/sh", path).Run()
			})
			if err := m.Click(t.Context(), "owned"); err != nil {
				t.Fatal(err)
			}
			w.set(func(o *WindowObservation) { o.ClientEnded = true })
			w.open = func(context.Context, string) error {
				if exits {
					w.set(func(o *WindowObservation) { o.State = WindowClosed })
				}
				w.reply(true, ErrWindowOpenCancelled)
				return nil
			}
			err := m.Click(t.Context(), "owned")
			if exits {
				if err != nil || launches != 2 || w.releases != 1 {
					t.Fatal(err, launches, w.releases)
				}
			} else if !errors.Is(err, ErrWindowOpenCancelled) || launches != 1 || w.releases != 0 {
				t.Fatal("cancel caused automatic replacement", err, launches, w.releases)
			}
		})
	}
}
