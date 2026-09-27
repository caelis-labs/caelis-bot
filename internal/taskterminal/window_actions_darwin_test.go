//go:build darwin && cgo

package taskterminal

import (
	"context"
	"errors"
	"testing"
	"time"
)

type actionWindow struct {
	observedWindow
	releases       int
	closes, places int
	closeErr       error
	point          Point
}

type waitingPreviewWindow struct {
	observedWindow
	entered chan struct{}
	release chan struct{}
}

func (w *waitingPreviewWindow) Preview(ctx context.Context) (PreviewSource, error) {
	close(w.entered)
	select {
	case <-ctx.Done():
		return PreviewSource{}, ctx.Err()
	case <-w.release:
		return PreviewSource{}, nil
	}
}

func TestWindowInputPreemptsOptionalPreview(t *testing.T) {
	for _, action := range []string{"apply", "move", "dismiss", "release"} {
		t.Run(action, func(t *testing.T) {
			w := &waitingPreviewWindow{entered: make(chan struct{}), release: make(chan struct{})}
			defer close(w.release)
			l := NewManaged(t.TempDir(), nil)
			l.windows["owned"] = w
			previewDone := make(chan error, 1)
			go func() { _, err := l.Preview(t.Context(), "owned"); previewDone <- err }()
			<-w.entered
			done := make(chan struct{})
			go func() {
				switch action {
				case "apply":
					_ = (&controlledBinding{launcher: l, id: "owned", window: w}).Apply(t.Context(), FocusWindow)
				case "move":
					_ = (&controlledBinding{launcher: l, id: "owned", window: w}).Move(t.Context(), Point{})
				case "dismiss":
					_ = l.Dismiss(t.Context(), "owned")
				case "release":
					l.Close()
				}
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("explicit window action waited for optional preview")
			}
			if err := <-previewDone; !errors.Is(err, context.Canceled) {
				t.Fatal("preview not canceled", err)
			}
		})
	}
}

func TestPreviewSkipsBusyWindowInsteadOfQueuing(t *testing.T) {
	l := NewManaged(t.TempDir(), nil)
	l.lockOperation()
	defer l.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := l.Preview(t.Context(), "owned"); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("preview queued behind explicit operation")
	}
}

func (w *actionWindow) Dismiss(context.Context) error { w.closes++; return w.closeErr }
func (w *actionWindow) Move(_ context.Context, p Point) error {
	w.places++
	w.point = p
	return nil
}
func (w *actionWindow) Preview(context.Context) (PreviewSource, error) { return PreviewSource{}, nil }
func TestDismissRetainsUnconfirmedBindingAndPlacementNeverToggles(t *testing.T) {
	w := &actionWindow{closeErr: ErrWindowPermission}
	l := NewManaged(t.TempDir(), nil)
	l.windows["owned"] = w
	if err := l.Dismiss(t.Context(), "owned"); !errors.Is(err, ErrWindowPermission) || len(l.windows) != 1 || w.releases != 0 {
		t.Fatal("lost unclosed window", err)
	}
	p := Point{X: -480, Y: 720}
	if err := (&controlledBinding{launcher: l, id: "owned", window: w}).Move(t.Context(), p); err != nil || len(w.calls()) != 0 || w.point != p {
		t.Fatal("drop toggled or lost target", err, w)
	}
	w.closeErr = nil
	if err := l.Dismiss(t.Context(), "owned"); err != nil || len(l.windows) != 0 || w.releases != 1 {
		t.Fatal("confirmed close not released", err)
	}
	if err := l.Dismiss(t.Context(), "never-opened"); err != nil {
		t.Fatal(err)
	}
	l.unmanaged = map[string]bool{"custom": true}
	if err := l.Dismiss(t.Context(), "custom"); !errors.Is(err, ErrWindowIdentity) {
		t.Fatal("claimed an unmanaged terminal closed", err)
	}

}

func (w *actionWindow) Release() { w.releases++ }

func TestManagerPlacementMovesBoundWindowWithoutTogglingForeground(t *testing.T) {
	m := testManager(t, nil)
	e, _ := m.entry("owned")
	w := &actionWindow{}
	w.observation = WindowObservation{State: WindowForeground, AppActive: true, Settled: true, CanCollapse: true}
	e.launcher.windows["owned"] = w
	e.controller.window = &controlledBinding{launcher: e.launcher, id: "owned", window: w}
	p := Point{X: -300, Y: 400}
	if err := m.Place(t.Context(), "owned", p); err != nil || w.point != p || len(w.calls()) != 0 {
		t.Fatal(err, w.point, w.calls())
	}
}

func TestUnsupportedPlacementStillBringsInstanceForward(t *testing.T) {
	m := testManager(t, nil)
	e, _ := m.entry("owned")
	_, w := controllerFixture(WindowBackground)
	immediateWindow(w)
	e.launcher.windows["owned"] = w
	e.controller.window = &controlledBinding{launcher: e.launcher, id: "owned", window: w}
	if err := m.Place(t.Context(), "owned", Point{X: 10, Y: 10}); err != nil {
		t.Fatal(err)
	}
	if got := w.calls(); len(got) != 1 || got[0] != FocusWindow {
		t.Fatal(got)
	}
}
