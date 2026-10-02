package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
)

// NotebookSyncOptions is native preparation, never renderer input. Profiles
// must be the existing APP profiles on those nodes, not node-agent directories.
// The existing lifecycle owner supplies idle/stopped proofs and a fresh-session
// start action. There is no automatic failover or session receipt migration.
type NotebookSyncOptions struct {
	SourceNodeID string
	Profiles     map[string]string
	Interval     time.Duration
	Hooks        notebooksync.Hooks
	// BeforeFinalTransfer preserves a stopped local target before the final copy.
	BeforeFinalTransfer func(context.Context, string) error
	// CompletedHandoff is optional. It must verify the exact completed Dream and
	// current digest after stopping. Missing/unfinished handoff returns no bytes.
	CompletedHandoff func(context.Context) ([]byte, error)
}

// AttachNotebookSync is opt-in native assembly, before APP Start or through the
// serialized default settings controller after closing the old timer. It reuses
// enrolled SSH pairings and starts only an APP-scoped timer.
func AttachNotebookSync(a *Application, o NotebookSyncOptions) error {
	if a == nil || a.Backend == nil || o.Interval < time.Minute {
		return errors.New("invalid Notebook sync assembly")
	}
	a.mu.Lock()
	busy := a.closed || a.notebookSync != nil
	a.mu.Unlock()
	if busy {
		return errors.New("Notebook sync already attached or APP closed")
	}

	if o.SourceNodeID == api.LocalNodeID && o.Hooks.SourceActive == nil && o.Hooks.StopSource == nil && o.Hooks.SourceStopped == nil {
		local, completed := a.NotebookLocalSourceHooks()
		o.Hooks.SourceActive, o.Hooks.StopSource, o.Hooks.SourceStopped = local.SourceActive, local.StopSource, local.SourceStopped
		if o.CompletedHandoff == nil {
			o.CompletedHandoff = completed
		}
	}
	if o.Hooks.SourceActive == nil || o.Hooks.StandbyStopped == nil {
		return errors.New("native active and standby ownership checks required")
	}
	doc, err := loadNodeManagementDocument(filepath.Join(a.root, "nodeplane", "config.json"))
	if err != nil {
		return err
	}
	registrations := map[string]NodeRegistration{}
	for _, r := range doc.Nodes {
		registrations[r.ID] = r
	}
	profiles := map[string]string{}
	for id, p := range o.Profiles {
		if id == api.LocalNodeID {
			if p != a.root {
				return errors.New("local Notebook profile must be this APP profile")
			}
		} else {
			r, ok := registrations[id]
			if !ok || r.Join != api.NodeSSH {
				return errors.New("Notebook sync requires the node's existing direct SSH pairing")
			}
		}
		profiles[id] = p
	}
	if profiles[o.SourceNodeID] == "" {
		return errors.New("actual source APP profile required")
	}
	state := notebooksync.State{SourceNodeID: o.SourceNodeID}
	for id := range profiles {
		if id != o.SourceNodeID {
			state.Targets = append(state.Targets, notebooksync.Status{NodeID: id, Phase: "ready"})
		}
	}
	sort.Slice(state.Targets, func(i, j int) bool { return state.Targets[i].NodeID < state.Targets[j].NodeID })
	file := filepath.Join(a.root, "nodeplane", "notebook-sync.json")
	if body, e := os.ReadFile(file); e == nil {
		var saved notebooksync.State
		if len(body) > 64<<10 || json.Unmarshal(body, &saved) != nil || saved.SourceNodeID != state.SourceNodeID || len(saved.Targets) != len(state.Targets) {
			return errors.New("Notebook sync state differs; reconcile native ownership before reconfiguration")
		}
		for i := range saved.Targets {
			if saved.Targets[i].NodeID != state.Targets[i].NodeID {
				return errors.New("Notebook backup targets differ from saved state")
			}
		}
		state = saved
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	resolve := func(ctx context.Context, id string) (notebooksync.Endpoint, func(), error) {
		p, ok := profiles[id]
		if !ok {
			return notebooksync.Endpoint{}, nil, errors.New("Notebook node is not configured")
		}
		e := notebooksync.Endpoint{Profile: p}
		cleanup := func() {}
		if id != api.LocalNodeID {
			r := registrations[id]
			args, close, err := nodeagent.NotebookSSH(ctx, nodeagent.SSHConfig{Target: r.SSHDestination})
			if err != nil {
				return e, nil, err
			}
			e.Target, e.Shell, cleanup = r.SSHDestination, args, close
		}
		return e, cleanup, nil
	}
	hooks := o.Hooks
	hooks.Transfer = func(ctx context.Context, id string, final bool) error {

		if final && o.BeforeFinalTransfer != nil {
			if err := o.BeforeFinalTransfer(ctx, id); err != nil {
				return err
			}
		}
		source, close, err := resolve(ctx, o.SourceNodeID)
		if err != nil {
			return err
		}
		defer close()
		target, close, err := resolve(ctx, id)
		if err != nil {
			return err
		}
		defer close()
		var handoff []byte
		if final && o.CompletedHandoff != nil {
			handoff, err = o.CompletedHandoff(ctx)
			if err != nil {
				return err
			}
		}
		return (notebooksync.Rsync{}).Sync(ctx, source, target, notebooksync.AttemptID(), final, handoff)
	}
	hooks.Save = func(s notebooksync.State) error { return localstate.Write(file, s) }
	c, err := notebooksync.New(state, hooks)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.notebookSync = c
	a.notebookSyncInterval = o.Interval
	a.mu.Unlock()
	a.Backend.ConfigureNotebookSync(c)
	return nil
}

func (a *Application) startNotebookSync() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.notebookSync == nil || a.notebookSyncCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.notebookSyncCancel = cancel
	c, interval := a.notebookSync, a.notebookSyncInterval
	a.workers.Add(1)
	go func() { defer a.workers.Done(); _ = c.Run(ctx, interval) }()
}
func (a *Application) closeNotebookSync() {
	a.mu.Lock()
	c, cancel := a.notebookSync, a.notebookSyncCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if c != nil {
		c.Close()
	}
}

func (a *Application) notebookSyncStartupGuard() error {
	a.mu.Lock()
	returning := a.notebookReturn
	a.mu.Unlock()
	if returning != nil {
		return returning.validateReturn(context.Background())
	}
	a.mu.Lock()
	c := a.notebookSync
	remote := a.product != nil
	recovery := a.notebookSyncRecovery
	a.mu.Unlock()
	if recovery && !remote {
		return errors.New("Notebook switch needs native owner review before restarting this source")
	}
	if c == nil || remote {
		return nil
	}
	for _, s := range c.State().Targets {
		if s.Phase != "ready" {
			return errors.New("Notebook switch has retired or may have stopped this source; reconcile the native owner before starting this APP")
		}
	}
	return nil
}
