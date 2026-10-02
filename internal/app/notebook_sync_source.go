package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
)

// NotebookLocalSourceHooks adapts the retained APP's existing idle and process
// boundaries. It cannot stop a discovered/shared Host or infer stop from SSH
// loss. Target standby/start ports still belong to their existing native owner.
func (a *Application) NotebookLocalSourceHooks() (notebooksync.Hooks, func(context.Context) ([]byte, error)) {
	var mu sync.Mutex
	var stopped bool
	var handoff []byte
	hooks := notebooksync.Hooks{}
	hooks.SourceActive = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		a.mu.Lock()
		active := a.started && !a.closed && !a.sourceRetired && a.product == nil
		a.mu.Unlock()
		if !active {
			return errors.New("retained native source is not active")
		}
		return nil
	}
	hooks.StopSource = func(ctx context.Context) error {
		if err := hooks.SourceActive(ctx); err != nil {
			return errors.Join(notebooksync.ErrStopNotDispatched, err)
		}
		if err := a.PrepareUpdate(); err != nil {
			return errors.Join(notebooksync.ErrStopNotDispatched, err)
		}
		if err := a.guardRuntimeChange(); err != nil {
			a.CancelUpdate()
			return errors.Join(notebooksync.ErrStopNotDispatched, err)
		}
		a.mu.Lock()
		resident := a.companion
		a.mu.Unlock()
		if resident != nil {
			body, err := resident.CompletedNotebookHandoff()
			if err != nil {
				a.CancelUpdate()
				return errors.Join(notebooksync.ErrStopNotDispatched, err)
			}
			mu.Lock()
			handoff = body
			mu.Unlock()
		}
		// Preserve the existing exact owned-process fencing implementation.
		var err error
		if native, ok := a.engine.(*codex.Session); ok {
			err = native.FenceOwnedForBootstrap(ctx)
		} else if native, ok := a.engine.(interface{ FenceStop(context.Context) error }); ok {
			err = native.FenceStop(ctx)
		} else {
			a.CancelUpdate()
			return errors.Join(notebooksync.ErrStopNotDispatched, errors.New("this Runtime cannot provide stopped ownership proof"))
		}
		if err != nil {
			return err
		}
		if err = a.retireNotebookSource(ctx); err != nil {
			return err
		}
		if err = a.guardRuntimeChange(); err != nil {
			return err
		}
		mu.Lock()
		stopped = true
		mu.Unlock()
		return nil
	}
	hooks.SourceStopped = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			return errors.New("source stop is not confirmed by the retained native owner")
		}
		return nil
	}
	completed := func(ctx context.Context) ([]byte, error) {
		if err := hooks.SourceStopped(ctx); err != nil {
			return nil, err
		}
		mu.Lock()
		body := append([]byte(nil), handoff...)
		mu.Unlock()
		if len(body) == 0 {
			return nil, nil
		}
		path := filepath.Join(a.root, "Notebook", "HANDOFF.md")
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 16<<10 {
			return nil, nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		after, err := f.Stat()
		if err != nil || !os.SameFile(info, after) {
			return nil, errors.New("stopped handoff changed during read")
		}
		current, err := io.ReadAll(io.LimitReader(f, (16<<10)+1))
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(current, body) {
			return nil, nil
		} // edited temporary state is not the proven Dream
		return body, nil
	}
	return hooks, completed
}
