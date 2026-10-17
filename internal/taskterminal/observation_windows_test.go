package taskterminal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"golang.org/x/sys/windows"
)

// Exercise the real receipt confirmation and Observe lifecycle, with only
// native HWND visibility/association facts controlled by the fixture.
type delayedHostWindow struct {
	hostedConsole
	reads, hiddenReads int
	lost               bool
	confirms           int
	receipt            []byte
}

func (w *delayedHostWindow) Confirm(ctx context.Context, pid int) error {
	if err := w.hostedConsole.Confirm(ctx, pid); err != nil {
		return err
	}
	w.confirms++
	var err error
	w.receipt, err = os.ReadFile(filepath.Join(filepath.Dir(w.descriptor), "accepted-"+strconv.Itoa(pid)))
	return err
}

func (w *delayedHostWindow) Observe(ctx context.Context) (WindowObservation, error) {
	return w.consoleWindow.observe(ctx, func() (WindowObservation, bool) {
		w.reads++
		if w.reads <= w.hiddenReads || w.lost {
			return WindowObservation{}, false
		}
		w.hwnd = 1 // A mapping was established; later loss must not become pending.
		return WindowObservation{State: WindowForeground, AppActive: true, Settled: true, CanCollapse: true}, true
	})
}

func TestWindowsManagerFirstClickWaitsForOriginalHostMapping(t *testing.T) {
	for _, neverVisible := range []bool{false, true} {
		t.Run(strconv.FormatBool(neverVisible), func(t *testing.T) {
			root := t.TempDir()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			w := &delayedHostWindow{hiddenReads: 3}
			if neverVisible {
				w.hiddenReads = 1 << 20
			}
			launches := 0
			var receipt []byte
			var accepted string
			var events []WindowEvent
			m := NewWindowManager(filepath.Join(root, "Terminal"), func(ctx context.Context, path string) (Window, error) {
				launches++
				data, err := os.ReadFile(path)
				if err != nil {
					return nil, err
				}
				var launch windowsLaunch
				if err := json.Unmarshal(data, &launch); err != nil {
					return nil, err
				}
				receipt, err = os.ReadFile(launch.Pending)
				if err != nil {
					return nil, err
				}
				process, err := windows.GetCurrentProcess()
				if err != nil {
					return nil, err
				}
				if err = writeConsoleIdentity(path, process, os.Getpid()); err != nil {
					return nil, err
				}
				accepted = launch.AcceptedPrefix + strconv.Itoa(os.Getpid())
				if err = os.Rename(launch.Pending, accepted); err != nil {
					return nil, err
				}
				w.descriptor, w.binary = path, exe
				return w, nil
			}, func(context.Context, string) (api.TerminalTarget, error) {
				return api.TerminalTarget{Runtime: "setup", Binary: exe, Directory: root}, nil
			}, func(_ string, event WindowEvent) { events = append(events, event) }, nil)
			t.Cleanup(m.Close)
			e, err := m.entry("owned")
			if err != nil {
				t.Fatal(err)
			}
			e.controller.timeout, e.controller.interval, e.controller.stableFor = 200*time.Millisecond, time.Millisecond, 2*time.Millisecond
			err = m.Click(t.Context(), "owned")
			if neverVisible {
				if !errors.Is(err, ErrWindowUnsettled) && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("first mapping wait was not bounded", err)
				}
			} else if err != nil || events[len(events)-1].Phase != WindowIdle || w.reads < 4 {
				t.Fatal("one click did not wait for host readiness", err, w.reads, events)
			}
			for _, event := range events {
				if errors.Is(event.Err, ErrWindowUnsupported) || !neverVisible && event.Phase == WindowUncertain {
					t.Fatal("initial host readiness became unsupported/uncertain", event)
				}
			}
			if !bytes.Equal(w.receipt, receipt) || launches != 1 || w.confirms != 1 || w.pid != os.Getpid() || w.process == 0 || e.launcher.windows["owned"] != w {
				t.Fatal("pending observation changed the client or receipt", launches, w.pid, w.confirms)
			}
			if !neverVisible {
				w.lost = true
				if err := m.Click(t.Context(), "owned"); !errors.Is(err, ErrWindowUnsupported) || launches != 1 {
					t.Fatal("lost established association must fail without waiting or relaunch", err, launches)
				}
			}
		})
	}
}
