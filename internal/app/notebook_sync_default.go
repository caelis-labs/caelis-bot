package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/notebooksync"
)

// The default controller is dormant until the user chooses backup nodes. Its
// settings contain only enrolled identities and cadence, never SSH or paths.
type defaultNotebookSync struct {
	mu        sync.Mutex
	app       *Application
	settings  backend.NotebookSyncSettings
	state     notebooksync.State
	returning *notebookLocalReturn
	ownerCall func(context.Context, NodeRegistration, nodeagent.NotebookOwnerRequest) (nodeagent.NotebookOwnerState, error)
}

func attachDefaultNotebookSync(a *Application) error {
	c := &defaultNotebookSync{app: a, settings: backend.NotebookSyncSettings{IntervalMinutes: 5, Targets: []backend.NotebookBackupTarget{}}}
	c.ownerCall = func(ctx context.Context, r NodeRegistration, in nodeagent.NotebookOwnerRequest) (nodeagent.NotebookOwnerState, error) {
		helper := r.HostHelperPath
		if helper == "" {
			helper = filepath.Join(r.Directory, "caelis-node")
		}
		return nodeagent.NotebookOwner(ctx, nodeagent.SSHConfig{Target: r.SSHDestination}, helper, r.Directory, r.ID, in)
	}
	file := filepath.Join(a.root, "nodeplane", "notebook-settings.json")
	if info, e := os.Lstat(file); e == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 64<<10 {
			return errors.New("Notebook settings unavailable; original file preserved")
		}
		body, e := os.ReadFile(file)
		if e != nil || json.Unmarshal(body, &c.settings) != nil {
			return errors.New("Notebook settings invalid; original file preserved")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	source := api.LocalNodeID
	if a.product != nil {
		source = a.product.pairing.NodeID
	}
	c.state = notebooksync.State{SourceNodeID: source, Targets: []notebooksync.Status{}}
	if info, e := os.Lstat(filepath.Join(a.root, "nodeplane", "notebook-sync.json")); e == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 64<<10 {
			return errors.New("Notebook switch intent unavailable; original file preserved")
		}
		body, e := os.ReadFile(filepath.Join(a.root, "nodeplane", "notebook-sync.json"))
		if e != nil || json.Unmarshal(body, &c.state) != nil {
			return errors.New("Notebook switch intent invalid")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if c.settings.SourceNodeID == api.LocalNodeID {
		if runtime, err := ReadNotebookRuntimeSettings(a.root); err == nil {
			c.settings.SourceBackend = api.NodeBackend(runtime.Runtime)
		}
	}
	for _, s := range c.state.Targets {
		if source == api.LocalNodeID && s.NodeID == source && (s.Phase == "starting" || s.Phase == "restart-required") {
			var receipt notebookLocalReturn
			if err := readNotebookPrivate(filepath.Join(a.root, "nodeplane", "notebook-local-return.json"), &receipt); err != nil {
				return err
			}
			if receipt.OperationID != s.OperationID || receipt.SourceNodeID != c.state.SourceNodeID {
				return errors.New("local return does not match original switch")
			}
			c.returning = &receipt
			a.mu.Lock()
			a.notebookReturn = c
			a.mu.Unlock()
		}
		if s.Phase == "switched" && s.NodeID == source && a.product != nil {
			if err := c.activateSource(source); err != nil {
				return err
			}
			break
		}
		if s.Phase != "ready" {
			a.notebookSyncRecovery = true
		}
	}
	if c.settings.Enabled && c.settings.SourceNodeID == source && a.notebookSync == nil && c.returning == nil {
		if err := c.assemble(context.Background(), false); err != nil {
			return err
		}
	}
	a.Backend.ConfigureNotebookSync(c)
	return nil
}
func (c *defaultNotebookSync) State() notebooksync.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.app.mu.Lock()
	controller := c.app.notebookSync
	c.app.mu.Unlock()
	if controller != nil {
		return controller.State()
	}
	out := c.state
	out.Targets = append([]notebooksync.Status{}, out.Targets...)
	return out
}
func (c *defaultNotebookSync) Settings() backend.NotebookSyncSettings {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.settings
	out.Targets = append([]backend.NotebookBackupTarget{}, out.Targets...)
	return out
}
func (c *defaultNotebookSync) Sync(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.app.notebookSync == nil {
		return errors.New("choose Notebook backup nodes and save first")
	}
	return c.app.notebookSync.Sync(ctx, id)
}
func (c *defaultNotebookSync) Switch(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.app.notebookSync == nil {
		return errors.New("choose Notebook backup nodes and save first")
	}
	if err := c.app.notebookSync.Switch(ctx, id); err != nil {
		return err
	}
	c.state = c.app.notebookSync.State()
	if id != api.LocalNodeID {
		if err := c.rotate(id); err != nil {
			return err
		}
	}
	c.app.mu.Lock()
	c.app.notebookRestartPrepared = true
	c.app.mu.Unlock()
	return nil
}
func (c *defaultNotebookSync) SaveSettings(ctx context.Context, in backend.NotebookSyncSettings) (backend.NotebookSyncSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.app
	source := api.LocalNodeID
	if a.product != nil {
		source = a.product.pairing.NodeID
	}

	if in.IntervalMinutes < 1 || in.IntervalMinutes > 1440 || len(in.Targets) > 16 || in.Enabled && len(in.Targets) == 0 {
		return c.settings, errors.New("choose backup nodes and an interval from 1 to 1440 minutes")
	}
	in.SourceNodeID = source
	in.SourceBackend = c.settings.SourceBackend
	if source == api.LocalNodeID {
		runtime, err := ReadNotebookRuntimeSettings(a.root)
		if err != nil && in.Enabled {
			return c.settings, err
		}
		if err == nil {
			in.SourceBackend = api.NodeBackend(runtime.Runtime)
		}
	}
	if a.notebookSync != nil {
		state := a.notebookSync.State()
		for _, s := range state.Targets {
			if s.Phase != "ready" {
				return c.settings, errors.New("finish the original Notebook switch before changing backup settings")
			}
		}
	} else {
		// A changed source is admitted only after connecting the exact successful
		// target saved by this switch. Failed/unknown switches stay fenced.
		for _, s := range c.state.Targets {
			if s.Phase != "ready" && !(s.Phase == "switched" && source == s.NodeID) {
				return c.settings, errors.New("original Notebook switch requires native owner review")
			}
		}
	}
	old := c.settings
	c.settings = in
	// Prepare and validate all targets before changing the active controller.
	options, err := c.options(ctx, true)
	if err != nil {
		c.settings = old
		return old, err
	}
	prior := c.state
	if a.notebookSync != nil {
		prior = a.notebookSync.State()
	}
	a.closeNotebookSync()
	a.mu.Lock()
	a.notebookSync = nil
	a.notebookSyncCancel = nil
	a.mu.Unlock()
	if err = localstate.Write(filepath.Join(a.root, "nodeplane", "notebook-settings.json"), in); err != nil {
		c.settings = old
		return old, err
	}
	c.state = notebooksync.State{SourceNodeID: source, Targets: []notebooksync.Status{}}
	for _, t := range in.Targets {
		status := notebooksync.Status{NodeID: t.NodeID, Phase: "ready"}
		if prior.SourceNodeID == source {
			for _, p := range prior.Targets {
				if p.NodeID == t.NodeID && p.Phase == "ready" {
					status = p
				}
			}
		}
		c.state.Targets = append(c.state.Targets, status)
	}
	sort.Slice(c.state.Targets, func(i, j int) bool { return c.state.Targets[i].NodeID < c.state.Targets[j].NodeID })
	if err = localstate.Write(filepath.Join(a.root, "nodeplane", "notebook-sync.json"), c.state); err != nil {
		return in, err
	}
	if in.Enabled {
		if err = AttachNotebookSync(a, options); err != nil {
			a.Backend.ConfigureNotebookSync(c)
			return in, err
		}
		a.mu.Lock()
		running := a.started
		a.mu.Unlock()
		if running {
			a.startNotebookSync()
		}
	}
	a.Backend.ConfigureNotebookSync(c)
	return in, nil
}
func (c *defaultNotebookSync) assemble(ctx context.Context, prepare bool) error {
	options, err := c.options(ctx, prepare)
	if err != nil {
		return err
	}
	return AttachNotebookSync(c.app, options)
}
func (c *defaultNotebookSync) options(ctx context.Context, prepare bool) (NotebookSyncOptions, error) {
	a := c.app
	o := NotebookSyncOptions{SourceNodeID: c.settings.SourceNodeID, Interval: time.Duration(c.settings.IntervalMinutes) * time.Minute, Profiles: map[string]string{}}
	if o.SourceNodeID == api.LocalNodeID {
		o.Profiles[api.LocalNodeID] = a.root
	}
	if !c.settings.Enabled {
		return o, nil
	}
	if o.Interval < time.Minute || o.Interval > 24*time.Hour {
		return o, errors.New("invalid saved Notebook interval")
	}
	doc, err := loadNodeManagementDocument(filepath.Join(a.root, "nodeplane", "config.json"))
	if err != nil {
		return o, err
	}
	regs := map[string]NodeRegistration{}
	for _, r := range doc.Nodes {
		regs[r.ID] = r
	}
	botID := ""
	if a.product != nil {
		var state bot.State
		// The portable identity was prepared at the target, and retained locally;
		// it is never reconstructed by decoding an opaque product Bot handle.
		body, e := os.ReadFile(filepath.Join(a.root, "bot.json"))
		if e != nil || json.Unmarshal(body, &state) != nil || state.ID == "" {
			return o, errors.New("portable Bot identity unavailable")
		}
		botID = state.ID
	} else {
		if prepare {
			if err = a.PreparePersonal(); err != nil {
				return o, err
			}
		}
		if a.companion != nil {
			botID = a.companion.State().ID
		} else {
			var state bot.State
			body, e := os.ReadFile(filepath.Join(a.root, "bot.json"))
			if e != nil || json.Unmarshal(body, &state) != nil || state.ID == "" {
				return o, errors.New("prepare Bot identity before enabling Notebook backup")
			}
			botID = state.ID
		}
	}
	operation := func() string {
		if a.notebookSync != nil {
			for _, s := range a.notebookSync.State().Targets {
				if s.OperationID != "" {
					return s.OperationID
				}
			}
		}
		return ""
	}
	request := func(ctx context.Context, id, action, operation string) (nodeagent.NotebookOwnerState, error) {
		r, ok := regs[id]
		if !ok || r.Join != api.NodeSSH {
			return nodeagent.NotebookOwnerState{}, errors.New("Notebook requires an existing direct SSH node")
		}
		return c.ownerCall(ctx, r, nodeagent.NotebookOwnerRequest{Action: action, BotID: botID, OperationID: operation})
	}
	seen := map[string]bool{o.SourceNodeID: true}
	for _, t := range c.settings.Targets {
		if t.NodeID == api.LocalNodeID && !seen[t.NodeID] {
			runtime, err := ReadNotebookRuntimeSettings(a.root)
			if err != nil {
				return o, err
			}
			if api.NodeBackend(runtime.Runtime) != t.Backend {
				return o, errors.New("local standby must use its configured Runtime")
			}
			if err := c.localStandby(botID); err != nil {
				return o, err
			}
			seen[t.NodeID] = true
			o.Profiles[t.NodeID] = a.root
			continue
		}
		r, ok := regs[t.NodeID]
		if !ok || r.Join != api.NodeSSH || seen[t.NodeID] || (t.Backend != api.NodeCodex && t.Backend != api.NodeCaelis) {
			return o, errors.New("choose distinct existing SSH backup nodes and their Runtime")
		}
		seen[t.NodeID] = true
		o.Profiles[t.NodeID] = nodeagent.NotebookOwnerProfile(r.Directory)
		if prepare {
			n := &nodeRuntimeMetadata{app: a}
			settings, e := n.runtimeSettings(ctx, r, t.Backend)
			if e != nil {
				return o, e
			}
			// Existing native ownership probe refuses shared/unmarked Caelis Stores.
			if t.Backend == api.NodeCaelis {
				eligible, _, e := n.ownedRuntimeProbe(ctx, r, t.Backend)
				if e != nil || !eligible {
					return o, errors.New("prepare this node's own Caelis Runtime before selecting it as a standby")
				}
			}
			state, e := c.ownerCall(ctx, r, nodeagent.NotebookOwnerRequest{Action: "prepare", BotID: botID, Runtime: &nodeagent.OwnedRuntimeSettings{Backend: t.Backend, Binary: settings.CLIPath, Store: settings.CaelisStore}})
			if e != nil || !state.Stopped {
				return o, errors.Join(errors.New("Notebook standby stop not confirmed"), e)
			}
		}
	}
	if len(c.settings.Targets) == 0 {
		return o, errors.New("at least one Notebook backup node required")
	}

	o.Hooks.TargetReady = func(ctx context.Context, id string) error {
		if id == api.LocalNodeID {
			return c.localStandby(botID)
		}
		var target backend.NotebookBackupTarget
		for _, t := range c.settings.Targets {
			if t.NodeID == id {
				target = t
			}
		}
		r := regs[id]
		n := &nodeRuntimeMetadata{app: a}
		settings, e := n.runtimeSettings(ctx, r, target.Backend)
		if e != nil {
			return e
		}
		if target.Backend == api.NodeCaelis {
			preferences, e := n.targetPreferences(ctx, r, target.Backend)
			if e != nil {
				return e
			}
			readiness, e := n.ownedRuntimeReadiness(ctx, r, nodeagent.OwnedRuntimeReadinessRequest{NodeID: id, Backend: target.Backend, OperationID: "notebook-ready-" + operation(), ExpectedBinary: settings.CLIPath, ExpectedStore: settings.CaelisStore, Model: preferences.Conversation.Model})
			if e != nil || !readiness.Ready || !readiness.StopConfirmed || readiness.Outcome != "ready" {
				if !readiness.StopConfirmed {
					return errors.Join(notebooksync.ErrPreparationUnconfirmed, errors.New("target Caelis stopped proof unconfirmed"), e)
				}
				return errors.Join(errors.New("target Caelis Runtime readiness failed"), e)
			}
		} else {
			node, e := n.catalogNode(ctx, id)
			if e != nil {
				return e
			}
			ready := false
			for _, runtime := range node.Runtimes {
				ready = ready || runtime.Backend == target.Backend && runtime.Health == api.NodeHealthy && runtime.Authentication == api.NodeAuthenticated
			}
			if !ready {
				return errors.New("target Runtime needs detection or sign-in before switching")
			}
		}
		state, e := c.ownerCall(ctx, r, nodeagent.NotebookOwnerRequest{Action: "prepare", BotID: botID, Runtime: &nodeagent.OwnedRuntimeSettings{Backend: target.Backend, Binary: settings.CLIPath, Store: settings.CaelisStore}})
		if e != nil || !state.Stopped {
			return errors.Join(errors.New("target standby stop unconfirmed"), e)
		}
		return nil
	}
	o.Hooks.StandbyStopped = func(ctx context.Context, id string) error {
		if id == api.LocalNodeID {
			return c.localStandby(botID)
		}
		state, e := request(ctx, id, "status", "")
		if e != nil {
			return e
		}
		if !state.Stopped {
			return errors.New("backup Bot must be stopped")
		}
		return nil
	}
	if o.SourceNodeID == api.LocalNodeID {
		local, completed := a.NotebookLocalSourceHooks()
		o.Hooks.SourceActive, o.Hooks.SourceStopped = local.SourceActive, local.SourceStopped
		o.CompletedHandoff = completed
		o.Hooks.StopSource = func(ctx context.Context) error {
			if err := local.StopSource(ctx); err != nil {
				return err
			}
			return localstate.Write(filepath.Join(a.root, "nodeplane", "notebook-local-owner.json"), notebookLocalStop{BotID: botID, Stopped: true})
		}
	}
	if o.SourceNodeID != api.LocalNodeID {
		r, ok := regs[o.SourceNodeID]
		if !ok || r.Join != api.NodeSSH || a.product == nil || a.product.pairing.NodeID != r.ID {
			return o, errors.New("connect the actual remote Notebook source first")
		}
		o.Profiles[r.ID] = nodeagent.NotebookOwnerProfile(r.Directory)
		o.Hooks.SourceActive = func(ctx context.Context) error {
			state, e := request(ctx, r.ID, "status", "")
			if e != nil {
				return e
			}
			if state.Stopped {
				return errors.New("remote source is stopped")
			}
			if state.Identity.BotID != a.product.pairing.BotID || state.Endpoint != a.product.pairing.Endpoint {
				return errors.New("remote source pairing changed")
			}
			return nil
		}
		o.CompletedHandoff = func(ctx context.Context) ([]byte, error) {
			state, e := request(ctx, r.ID, "handoff", "")
			if e != nil {
				return nil, e
			}
			if !state.Stopped {
				return nil, errors.New("remote source stop unconfirmed")
			}
			return state.Handoff, nil
		}
		o.Hooks.StopSource = func(ctx context.Context) error {
			if e := a.PrepareUpdate(); e != nil {
				return errors.Join(notebooksync.ErrStopNotDispatched, e)
			}
			state, e := request(ctx, r.ID, "stop", "notebook-stop-"+operation())
			if e != nil {
				return e
			}
			if !state.Stopped {
				return errors.New("remote source stop unconfirmed")
			}
			return nil
		}
		o.Hooks.SourceStopped = func(ctx context.Context) error {
			state, e := request(ctx, r.ID, "status", "")
			if e != nil {
				return e
			}
			if !state.Stopped {
				return errors.New("remote source still active")
			}
			return nil
		}
	}
	o.BeforeFinalTransfer = func(ctx context.Context, id string) error {
		if id == api.LocalNodeID {
			return c.preserveLocalNotebook(botID, operation())
		}
		return nil
	}
	o.Hooks.StartFresh = func(ctx context.Context, id string) error {
		if id == api.LocalNodeID {
			return c.prepareLocalReturn(botID, operation())
		}
		state, e := request(ctx, id, "start", "notebook-start-"+operation())
		if e != nil {
			return e
		}
		if state.Stopped || state.Endpoint == "" {
			return errors.New("target fresh owner not confirmed")
		}
		r := regs[id]
		helper := r.HostHelperPath
		if helper == "" {
			helper = filepath.Join(r.Directory, "caelis-node")
		}
		pairing := backend.ProductPairing{Mode: "remote", Label: r.Label, SSH: r.SSHDestination, Helper: helper, Endpoint: state.Endpoint, AuthFile: filepath.Join(o.Profiles[id], "Product", "product.auth"), NodeID: state.Identity.NodeID, BotID: state.Identity.BotID}
		if e = validateProductPairing(pairing); e != nil {
			return e
		}
		// Test the existing product proxy before claiming the target is usable.
		client, transport, e := newSSHProductClient(pairing)
		if e != nil {
			return e
		}
		defer client.Close()
		if transport != nil {
			defer transport.Close()
		}
		if _, e = client.Connect(ctx); e != nil {
			return e
		}
		_, e = a.Backend.SaveProductPairing(pairing, a.Backend.ProductConnection().Revision)
		return e
	}
	return o, nil
}
