//go:build darwin && cgo

package desktop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// RunTerminalSmoke is an explicit local acceptance entrypoint. It never loads
// the Bot store, starts a Runtime, attaches a real session or sends model input.
func RunTerminalSmoke(terminals []string) error {
	confirmClose := len(terminals) > 0 && terminals[0] == "--confirm-close"
	confirmOpen := len(terminals) > 0 && terminals[0] == "--confirm-open"
	if confirmClose || confirmOpen {
		terminals = terminals[1:]
	}
	if len(terminals) == 0 {
		return fmt.Errorf("specify terminal, iterm2 or ghostty")
	}
	for _, terminal := range terminals {
		if taskterminal.BundleID(terminal) == "" {
			return fmt.Errorf("unknown test terminal")
		}
	}
	results := make(chan error, 1)
	app := application.New(application.Options{Name: "Caelis Bot terminal acceptance", Mac: application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory}})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			result := smokeTerminalInstances(terminals, confirmClose, confirmOpen)
			if result != nil {
				log.Printf("TERMINAL E2E FAIL: %v", result)
			} else {
				log.Print("TERMINAL E2E PASS")
			}
			results <- result
			app.Quit()
		}()
	})
	if err := app.Run(); err != nil {
		return err
	}
	return <-results
}
func smokeTerminalInstances(terminals []string, confirmClose, confirmOpen bool) error {
	dir, err := os.MkdirTemp("", "caelis-terminal-e2e-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var failures []error
	for _, terminal := range terminals {
		if !terminalInstalled(taskterminal.BundleID(terminal)) {
			return fmt.Errorf("%s is not installed", terminal)
		}
		if err := smokeTerminalInstance(dir, terminal, confirmClose, confirmOpen); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", terminal, err))
		}
	}
	return errors.Join(failures...)
}
func smokeTerminalInstance(dir, terminal string, confirmClose, confirmOpen bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	stop := filepath.Join(dir, terminal+"-finish")
	binary := filepath.Join(dir, terminal+"-client")
	script := "#!/bin/sh\nprintf '\\033[36mCaelis Bot disposable terminal acceptance\\033[0m\\nNo real task, account or model request.\\n'\nwhile [ ! -f '" + stop + "' ]; do /bin/sleep 0.1; done\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		return err
	}
	var last taskterminal.WindowState
	opened := 0
	var windows []*smokeOwnedWindow
	manager := taskterminal.NewWindowManager(filepath.Join(dir, terminal), func(ctx context.Context, path string) (taskterminal.Window, error) {
		opened++
		w, err := taskterminal.OpenWindow(ctx, terminal, path)
		if cw, ok := w.(taskterminal.ControlledWindow); ok {
			owned := &smokeOwnedWindow{ControlledWindow: cw}
			windows = append(windows, owned)
			return owned, err
		}
		return w, err
	}, func(context.Context, string) (api.TerminalTarget, error) {
		return terminalSmokeTarget(dir, binary), nil
	}, func(_ string, event taskterminal.WindowEvent) {
		last = event.State
		log.Printf("TERMINAL E2E %s phase=%s state=%s error=%v", terminal, event.Phase, event.State, event.Err)
	}, nil)
	// End only this synthetic client and normally quit its owned GUI on failure.
	defer func() {
		_ = os.WriteFile(stop, nil, 0600)
		time.Sleep(350 * time.Millisecond)
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := manager.Dismiss(cleanup, "fixture"); err != nil {
			log.Printf("TERMINAL E2E %s cleanup: %v", terminal, err)
		}
		manager.Close()
		// Only our synthetic instances are retained for this explicit cleanup.
		// Product reconnect never closes an instance it has detached from.
		for _, window := range windows {
			if err := window.Dismiss(cleanup); err != nil {
				log.Printf("TERMINAL E2E %s retained cleanup: %v", terminal, err)
			}
			window.ControlledWindow.Release()
		}
	}()
	if confirmOpen {
		log.Printf("TERMINAL E2E %s MANUAL: cancel the first open confirmation", terminal)
		err := manager.Click(ctx, "fixture")
		if !errors.Is(err, taskterminal.ErrWindowOpenCancelled) && !errors.Is(err, taskterminal.ErrWindowNotConnected) {
			return fmt.Errorf("cancelled open: %w", err)
		}
		if opened != 1 || len(windows) != 1 || windows[0].released != 0 {
			return fmt.Errorf("cancel lost owned application")
		}
		log.Printf("TERMINAL E2E %s MANUAL: accept the retry open confirmation", terminal)
		if err := manager.Click(ctx, "fixture"); err != nil {
			return fmt.Errorf("retry after cancel: %w", err)
		}
		if opened != 1 || windows[0].reused != 1 || last != taskterminal.WindowForeground {
			return fmt.Errorf("retry failed to reuse original instance")
		}
		time.Sleep(3 * time.Second)
		_ = os.WriteFile(stop, nil, 0600)
		time.Sleep(350 * time.Millisecond)
		if err := manager.Dismiss(ctx, "fixture"); err != nil {
			return err
		}
		log.Printf("TERMINAL E2E %s cancelled-open/retry-same-instance/confirmed-client/close passed", terminal)
		return nil
	}
	for cycle := 0; cycle < 2; cycle++ {
		_ = os.Remove(stop)
		before := opened
		if err := manager.Click(ctx, "fixture"); err != nil {
			return fmt.Errorf("open: %w", err)
		}
		if last != taskterminal.WindowForeground {
			return fmt.Errorf("open did not reach foreground")
		}
		if opened != before+1 {
			return fmt.Errorf("duplicate launch")
		}
		// Model a long-lived TUI. Immediate synthetic exit triggers some
		// terminals' "session ended too soon" warning instead of normal quit.
		time.Sleep(3 * time.Second)
		// Other apps/user interaction may take focus during that intentional
		// script-lifetime wait. Establish the test precondition from OS state,
		// rather than treating a click on a background app as a collapse.
		current, err := windows[len(windows)-1].Observe(ctx)
		if err != nil {
			return err
		}
		if current.State != taskterminal.WindowForeground {
			log.Printf("TERMINAL E2E %s foreground precondition changed to %s; explicitly restoring", terminal, current.State)
			// This fixture deliberately has no Move capability. Place therefore
			// exercises the manager's idempotent show path without moving windows.
			if err := manager.Place(ctx, "fixture", taskterminal.Point{}); err != nil {
				return err
			}
		}
		for toggle := 0; toggle < 2; toggle++ {
			if err := manager.Click(ctx, "fixture"); err != nil {
				return fmt.Errorf("toggle: %w", err)
			}
			want := taskterminal.WindowCollapsed
			if toggle == 1 {
				want = taskterminal.WindowForeground
			}
			if last != want {
				return fmt.Errorf("toggle reached %s, expected %s", last, want)
			}
		}
		if last != taskterminal.WindowForeground {
			return fmt.Errorf("restore did not reach foreground")
		}
		if opened != before+1 {
			return fmt.Errorf("toggle created another client")
		}
		if cycle == 0 {
			if confirmClose {
				log.Printf("TERMINAL E2E %s MANUAL: cancel the first close confirmation", terminal)
				if err := manager.Dismiss(ctx, "fixture"); !errors.Is(err, taskterminal.ErrWindowCloseCancelled) {
					return fmt.Errorf("expected user cancel, got: %v", err)
				}
				for range 2 {
					if err := manager.Click(ctx, "fixture"); err != nil {
						return fmt.Errorf("click after cancelled close: %w", err)
					}
				}
				log.Printf("TERMINAL E2E %s MANUAL: accept the second close confirmation", terminal)
				if err := manager.Dismiss(ctx, "fixture"); err != nil {
					return fmt.Errorf("confirmed close: %w", err)
				}
				log.Printf("TERMINAL E2E %s manual cancel/continued-control/confirmed-exit passed", terminal)
				continue
			}
			// Simulate closing the task client while its application may remain
			// resident. Native process evidence must authorize a fresh connection,
			// not toggling an empty shell or quitting the user's leftover windows.
			previous := windows[len(windows)-1]
			if err := os.WriteFile(stop, nil, 0600); err != nil {
				return err
			}
			var oldState taskterminal.WindowObservation
			deadline := time.Now().Add(4 * time.Second)
			for {
				var err error
				oldState, err = previous.Observe(ctx)
				if err != nil {
					return fmt.Errorf("observe ended client: %w", err)
				}
				if oldState.ClientEnded || oldState.State == taskterminal.WindowClosed {
					break
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("client exit not observed")
				}
				time.Sleep(40 * time.Millisecond)
			}
			if err := os.Remove(stop); err != nil {
				return err
			}
			if err := manager.Click(ctx, "fixture"); err != nil {
				return fmt.Errorf("reconnect ended client: %w", err)
			}
			if last != taskterminal.WindowForeground {
				return fmt.Errorf("reconnect did not reach foreground")
			}
			now, err := previous.Observe(ctx)
			if err != nil {
				return fmt.Errorf("observe detached app: %w", err)
			}
			// A terminal may quit itself after its last session exits (including
			// Ghostty's launch setting). Assert no Bot quit, not perpetual liveness.
			if previous.dismissed != 0 {
				return fmt.Errorf("reconnect quit old application")
			}
			if now.State == taskterminal.WindowClosed {
				// Exit can happen just after client-exit observation. The same
				// gesture may replace it once after fencing the unclaimed script.
				if opened != before+2 || previous.released != 1 {
					return fmt.Errorf("exited application was not replaced")
				}
			} else if opened != before+1 || previous.released != 0 || previous.reused != 1 {
				return fmt.Errorf("live application was not reused")
			}
			log.Printf("TERMINAL E2E %s client-exit reconnect passed; previous-gui=%s->%s reused=%d; no quit sent", terminal, oldState.State, now.State, previous.reused)
			time.Sleep(3 * time.Second)
		}
		// Let the synthetic command finish so normal quit cannot need a
		// terminate-running-command confirmation from the terminal itself.
		if err := os.WriteFile(stop, nil, 0600); err != nil {
			return err
		}
		time.Sleep(350 * time.Millisecond)
		if err := manager.Dismiss(ctx, "fixture"); err != nil {
			return fmt.Errorf("close: %w", err)
		}
		log.Printf("TERMINAL E2E %s cycle=%d open/focus/optional-collapse/close passed", terminal, cycle+1)
	}
	return nil
}

// Instrument lease release so acceptance can verify the detached GUI survives
// and normally quit only these disposable instances afterward. All creation,
// receipt, process observation and window operations use the production adapter.
type smokeOwnedWindow struct {
	taskterminal.ControlledWindow
	released, dismissed, reused int
}

func (w *smokeOwnedWindow) Release() { w.released++ }
func (w *smokeOwnedWindow) Dismiss(ctx context.Context) error {
	w.dismissed++
	return w.ControlledWindow.(interface{ Dismiss(context.Context) error }).Dismiss(ctx)
}
func (w *smokeOwnedWindow) OpenDocument(ctx context.Context, path string) error {
	w.reused++
	return w.ControlledWindow.(taskterminal.DocumentWindow).OpenDocument(ctx, path)
}
func (w *smokeOwnedWindow) DocumentResult(ctx context.Context) (bool, error) {
	return w.ControlledWindow.(taskterminal.DocumentWindow).DocumentResult(ctx)
}
