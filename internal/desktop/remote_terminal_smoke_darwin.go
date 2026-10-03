//go:build darwin && cgo

package desktop

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/machines"
	"github.com/caelis-labs/caelis-bot/internal/taskterminal"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"log"
	"os"
	"path/filepath"
	"time"
)

// RunRemoteTerminalSmoke observes one retained real task using the production
// terminal route. It cannot submit a prompt, start a task, or stop a runtime.
// The explicit profile must be a disposable acceptance profile.
func RunRemoteTerminalSmoke(root, id, terminal string) error {
	if !filepath.IsAbs(root) || id == "" || taskterminal.BundleID(terminal) == "" {
		return errors.New("explicit acceptance profile, owned task and terminal required")
	}
	source, err := machines.Open(filepath.Join(root, "Machines"), observationOnlyRuntime{}, nil)
	if err != nil {
		return err
	}
	before, err := source.WorkTerminal(context.Background(), id)
	if err != nil || len(before.SSH) == 0 {
		return errors.New("original remote terminal target unavailable")
	}
	results := make(chan error, 1)
	native := application.New(application.Options{Name: "Caelis Bot remote terminal acceptance", Mac: application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory}})
	native.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			results <- remoteTerminalCycles(root, id, terminal, source, before)
			native.Quit()
		}()
	})
	if err = native.Run(); err != nil {
		return err
	}
	return <-results
}
func remoteTerminalCycles(root, id, terminal string, source *machines.Service, before api.TerminalTarget) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp(root, ".remote-terminal-acceptance-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	opened := 0
	manager := taskterminal.NewWindowManager(dir, func(ctx context.Context, path string) (taskterminal.Window, error) {
		opened++
		return taskterminal.OpenWindow(ctx, terminal, path)
	}, source.WorkTerminal, func(_ string, event taskterminal.WindowEvent) {
		if event.Err != nil {
			log.Printf("REMOTE TERMINAL phase=%s state=%s error=%v", event.Phase, event.State, event.Err)
		}
	}, nil)
	defer manager.Close()
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = manager.Dismiss(cleanup, id)
	}()
	for cycle := range 2 {
		if err = manager.Click(ctx, id); err != nil {
			return err
		}
		log.Printf("REMOTE TERMINAL cycle=%d native TUI foreground; inspect this window", cycle+1)
		time.Sleep(8 * time.Second)
		if err = manager.Click(ctx, id); err != nil {
			return err
		} // Existing Dock collapse.
		if err = manager.Click(ctx, id); err != nil {
			return err
		} // Existing Dock restore.
		if opened != cycle+1 {
			return errors.New("native observer launched twice during collapse/restore")
		}
		if err = manager.Dismiss(ctx, id); err != nil {
			return err
		}
		after, e := source.WorkTerminal(ctx, id)
		if e != nil || after.Thread != before.Thread || after.Session != before.Session || after.Endpoint != before.Endpoint {
			return errors.New("observer close changed the original binding")
		}
		log.Printf("REMOTE TERMINAL cycle=%d observer closed; original native binding retained", cycle+1)
	}
	log.Print("REMOTE TERMINAL E2E PASS: SSH native TUI, collapse/restore, close/reopen, same task; no task mutation")
	return nil
}

type observationOnlyRuntime struct{}

func (observationOnlyRuntime) WorkAdmission(context.Context) error {
	return errors.New("observation-only acceptance")
}
func (observationOnlyRuntime) WorkStates() []api.WorkState { return nil }
func (observationOnlyRuntime) StartWork(context.Context, api.WorkStart) (api.Task, error) {
	return api.Task{}, errors.New("observation-only acceptance")
}
func (observationOnlyRuntime) ReadWork(context.Context, string) (api.Task, error) {
	return api.Task{}, errors.New("local tasks unavailable")
}
func (observationOnlyRuntime) SendWork(context.Context, api.TaskMessage) (api.Task, error) {
	return api.Task{}, errors.New("observation-only acceptance")
}
func (observationOnlyRuntime) StopWork(context.Context, string) (api.Task, error) {
	return api.Task{}, errors.New("observation-only acceptance")
}
