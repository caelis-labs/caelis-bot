package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/bot"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/memorytransfer"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/roaming"
)

// NewManagedNode assembles an isolated imported generation. It does not start
// execution or restore old native threads, task ledgers, wakes or receipts.
// Callers install the broker grant on Guard before Application.Start/Connect.
// Default New remains the original local assembly with no broker dependency.
type ManagedNodeOptions struct {
	// BrokerNodeID is the paired broker's inspected identity, supplied by native
	// composition. It is never a tool or renderer parameter.
	BrokerNodeID       string
	WatchdogHelperPath string
	CodexBinary        string
	Backend            api.NodeBackend
	CaelisHost         *caelis.OwnedHostOptions
	Model              string
	buildContext       context.Context
}

func NewManagedNode(root string, host Host, nodeID string, options ...ManagedNodeOptions) (*Application, *roaming.Guard, error) {
	if len(options) > 1 {
		return nil, nil, errors.New("managed node accepts one native configuration")
	}
	var configured ManagedNodeOptions
	if len(options) == 1 {
		configured = options[0]
	}
	if configured.Backend == "" {
		configured.Backend = api.NodeCodex
	}
	if configured.Backend != api.NodeCodex && configured.Backend != api.NodeCaelis {
		return nil, nil, errors.New("managed backend is unavailable")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil, nil, errors.New("this platform cannot confirm owned runtime shutdown")
	}
	if nodeID == "" || !filepath.IsAbs(root) {
		return nil, nil, errors.New("managed node requires an isolated absolute generation and node identity")
	}
	// Only a fresh Notebook import may populate the generation's portable data.
	// Never adopt an old profile's native binding or pending operation ledger.
	for _, name := range []string{"conversation.json", "tasks.json", "dream.json", "care.json", "worker-nodes.json", "providers", "Tasks"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			return nil, nil, errors.New("managed generation contains previous runtime state")
		}
	}
	snapshot, err := memorytransfer.ReadInstalledNotebookRef(context.Background(), root)
	if err != nil {
		return nil, nil, err
	}
	stateBytes, err := os.ReadFile(filepath.Join(root, "bot.json"))
	if err != nil {
		return nil, nil, err
	}
	var state bot.State
	if json.Unmarshal(stateBytes, &state) != nil || state.ID == "" || state.Wake != nil || len(state.Schedules) != 0 {
		return nil, nil, errors.New("managed generation must have fresh portable identity without previous wake work")
	}
	if configured.Backend == api.NodeCaelis {
		if configured.CaelisHost == nil || configured.CaelisHost.NodeID != nodeID {
			return nil, nil, errors.New("managed Caelis needs the exact designated target-side owned host")
		}
		if err := localstate.Write(filepath.Join(root, "runtime.json"), api.RuntimeSettings{Runtime: "caelis", CLIPath: configured.CaelisHost.Binary, CaelisStore: configured.CaelisHost.Store}); err != nil {
			return nil, nil, err
		}
		if err := os.MkdirAll(filepath.Join(root, "providers", "caelis"), 0700); err != nil {
			return nil, nil, err
		}
		if err := localstate.Write(filepath.Join(root, "providers", "caelis", "execution.json"), api.ExecutionSettings{Model: configured.Model, ApprovalMode: "workspace-write"}); err != nil {
			return nil, nil, err
		}
	}

	resolve := func(id string) (providerFactory, error) {
		if id != string(configured.Backend) {
			return providerFactory{}, errors.New("Caelis shared Host is not eligible for automatic Bot takeover")
		}
		f, err := resolveProvider(id)
		if err != nil {
			return f, err
		}
		f.Open = func(c providerConfig) (api.Engine, error) {
			if id == "caelis" {
				ctx := configured.buildContext
				if ctx == nil {
					ctx = context.Background()
				}
				hostOwner := *configured.CaelisHost
				hostOwner.WatchdogHelper = configured.WatchdogHelperPath
				return caelis.NewOwned(ctx, caelis.Options{Diagnostics: c.Diagnostics, Directory: filepath.Dir(c.ConversationFile), Settings: c.Settings, Execution: c.Execution, WorkExecution: c.WorkExecution}, hostOwner)
			}
			if err := codex.ValidateSettings(c.Settings, c.Execution); err != nil {
				return nil, err
			}
			binary := configured.CodexBinary
			if binary == "" {
				binary = c.Settings.CLIPath
			}
			if binary == "" {
				binary = os.Getenv("CODEX_BIN")
			}
			session := codex.NewSession(codex.SessionOptions{Diagnostics: c.Diagnostics, Binary: binary, Execution: c.Execution, WorkExecution: c.WorkExecution, Directory: c.WorkDirectory, WorkRoot: c.WorkRoot, StateFile: c.ConversationFile, ForceOwned: true, WatchdogHelper: configured.WatchdogHelperPath})
			ctx := configured.buildContext
			if ctx == nil {
				ctx = context.Background()
			}
			if err := session.VerifyOwnedSupervisor(ctx); err != nil {
				return nil, err
			}
			return session, nil
		}
		return f, nil
	}
	a, err := newApplication(root, host, resolve)
	if err != nil {
		return nil, nil, err
	}
	owner := &managedNodeOwner{app: a, nodeID: nodeID, generation: rand.Text(), snapshot: snapshot}
	guard := roaming.NewGuard(nodeID, configured.Backend, owner, true)
	if err := guard.ConfigureBroker(configured.BrokerNodeID); err != nil {
		_ = a.Close()
		return nil, nil, err
	}
	owner.guard = guard
	owner.nativeFenced = make(chan struct{})
	owner.nativeFenceDone = make(chan struct{})
	a.managed = owner
	a.executionAdmission = guard
	a.Backend.ConfigureExecutionAdmission(guard)
	a.engine.(interface{ ConfigureExecutionAdmission(api.ExecutionAdmission) }).ConfigureExecutionAdmission(guard)
	a.engine.(interface {
		ConfigureDispatchSource(func(context.Context, api.WorkDispatchSource) (api.WorkDispatchSource, error))
	}).ConfigureDispatchSource(guard.AnnotateWorkSource)
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if host.BindLeasePower == nil {
			_ = a.Close()
			return nil, nil, errors.New("native sleep/wake lease fencing must be bound before managed activation")
		}
		release, err := host.BindLeasePower(func() {
			guard.Suspend()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			if err := owner.hardFence(ctx); err != nil && host.ReportError != nil {
				host.ReportError(err)
			}
		}, func() { _ = guard.Wake() })
		if err != nil {
			_ = a.Close()
			return nil, nil, err
		}
		owner.powerMu.Lock()
		owner.releasePower = release
		owner.powerMu.Unlock()
		if guard.IsStopped() {
			go release()
			return nil, nil, roaming.ErrFenced
		}
	}
	return a, guard, nil
}

type managedNodeOwner struct {
	app                *Application
	guard              *roaming.Guard
	nodeID, generation string
	snapshot           nodeplane.SnapshotRef
	powerMu            sync.Mutex
	releasePower       func()
	nativeFenced       chan struct{}
	nativeFenceDone    chan struct{}
	nativeFenceOnce    sync.Once
	nativeFenceErr     error
}

func (o *managedNodeOwner) WithdrawWorkerGrants(ctx context.Context) error {
	a := o.app
	a.mu.Lock()
	resident := a.companion
	a.mu.Unlock()
	if resident != nil {
		resident.Stop()
	}
	return nil
}

func (o *managedNodeOwner) ConfigureLeaseDeadline(ctx context.Context, lease nodeplane.Lease, deadline time.Time) error {
	native, ok := o.app.engine.(interface {
		ConfigureOwnedDeadline(context.Context, string, time.Time) error
	})
	if !ok {
		return errors.New("native runtime has no independent lease watchdog")
	}
	return native.ConfigureOwnedDeadline(ctx, lease.Epoch, deadline)
}
func (o *managedNodeOwner) hardFence(ctx context.Context) error {
	o.nativeFenceOnce.Do(func() {
		native, ok := o.app.engine.(interface{ FenceStop(context.Context) error })
		if !ok {
			o.nativeFenceErr = errors.New("runtime does not expose owned native fencing")
		} else {
			o.nativeFenceErr = native.FenceStop(ctx)
		}
		if o.nativeFenceErr == nil {
			close(o.nativeFenced)
		}
		close(o.nativeFenceDone)
	})
	select {
	case <-o.nativeFenceDone:
		return o.nativeFenceErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (o *managedNodeOwner) Stop(ctx context.Context) error {
	err := o.hardFence(ctx)
	err = errors.Join(err, o.app.Close())
	// Never call a power binder's release from its own suspend callback. Native
	// termination is already complete; observer disposal is independent cleanup.
	o.powerMu.Lock()
	release := o.releasePower
	o.powerMu.Unlock()
	if release != nil {
		go release()
	}
	return err
}

func (o *managedNodeOwner) SafeIdle(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return o.app.guardRuntimeChange()
}

// ReadRuntimeProof is served only through an agent's paired owner port. Native
// idle checks and the installed Notebook descriptor accompany every proof.
func (a *Application) ReadRuntimeProof(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	a.mu.Lock()
	o := a.managed
	var snapshot nodeplane.SnapshotRef
	if o != nil {
		snapshot = o.snapshot
	}
	a.mu.Unlock()
	backend := a.engine.(api.Provider).ProviderInfo().ID
	if o == nil || target.NodeID != o.nodeID || target.Backend != backend || target.Role != api.RoleBot {
		return nodeplane.RuntimeEligibility{}, errors.New("target has no managed native Bot owner")
	}
	if readiness, ok := a.engine.(interface{ OwnedRuntimeReady(context.Context) error }); ok {
		if err := readiness.OwnedRuntimeReady(ctx); err != nil {
			return nodeplane.RuntimeEligibility{}, err
		}
	}
	lease, active := o.guard.Lease()
	leaseEpoch := lease.Epoch
	if !active {
		lease = nodeplane.Lease{BotID: snapshot.BotID, NodeID: o.nodeID, Backend: api.NodeBackend(backend), Epoch: o.generation}
	}
	proof, err := o.guard.Proof(ctx, lease)
	if err != nil {
		return nodeplane.RuntimeEligibility{}, err
	}
	proof.Epoch = o.generation
	idleErr := o.SafeIdle(ctx)
	v := a.engine.Snapshot()
	pending := v.CanInterrupt || v.Phase == "sending" || v.Phase == "working" || len(v.Approvals) > 0
	unknown := v.Phase == "unknown" || v.LastReceipt.Outcome == "unknown"
	a.mu.Lock()
	manager, resident := a.tasks, a.companion
	a.mu.Unlock()
	if manager != nil {
		for _, task := range manager.ListTasks() {
			if task.Status == "unknown" || task.Outcome == "unknown" {
				unknown = true
			}
			switch task.Status {
			case "completed", "failed", "cancelled", "interrupted":
			default:
				pending = true
			}
		}
	}
	if resident != nil {
		if wake := resident.State().Wake; wake != nil && wake.Status != "accepted" {
			pending = true
			unknown = unknown || wake.Status == "unknown" || wake.Status == "dispatching"
		}
	}
	return nodeplane.RuntimeEligibility{Proof: proof, Snapshot: snapshot, SafeIdle: idleErr == nil && !pending && !unknown, Pending: pending, Unknown: unknown, LeaseEpoch: leaseEpoch}, nil
}

// ManagedNodeFactory bridges the runtime orchestrator to the real application
// assembly. The caller owns the paired agent and supplies its native host.
func ManagedNodeFactory(host Host, options ...ManagedNodeOptions) roaming.RuntimeFactory {
	return func(ctx context.Context, profile string, target api.WorkTarget) (roaming.ManagedRuntime, *roaming.Guard, error) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		var configured ManagedNodeOptions
		if len(options) > 1 {
			return nil, nil, errors.New("managed node accepts one native configuration")
		}
		if len(options) == 1 {
			configured = options[0]
		}
		configured.Backend = api.NodeBackend(target.Backend)
		configured.buildContext = ctx
		return NewManagedNode(profile, host, target.NodeID, configured)
	}
}
func (a *Application) PauseNotebook(ctx context.Context) (func(), error) {
	if a.managed == nil {
		return nil, errors.New("managed native owner unavailable")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := a.PrepareUpdate(); err != nil {
		return nil, err
	}
	if err := a.managed.SafeIdle(ctx); err != nil {
		a.CancelUpdate()
		proof, proofErr := a.ReadRuntimeProof(ctx, api.WorkTarget{NodeID: a.managed.nodeID, Backend: a.engine.(api.Provider).ProviderInfo().ID, Role: api.RoleBot})
		if proofErr == nil && proof.Pending && !proof.Unknown {
			return nil, errors.Join(roaming.ErrNotebookBusy, err)
		}
		return nil, err
	}
	return a.CancelUpdate, nil
}
func (a *Application) SetNotebookSnapshot(ctx context.Context, lease nodeplane.Lease, ref nodeplane.SnapshotRef) error {
	if a.managed == nil {
		return errors.New("managed native owner unavailable")
	}
	if err := a.managed.guard.Check(ctx, lease); err != nil {
		return err
	}
	if ref.BotID != lease.BotID || ref.Epoch != lease.Epoch {
		return errors.New("published Notebook does not match active epoch")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := localstate.Write(filepath.Join(a.root, "notebook-snapshot.json"), map[string]any{"format": memorytransfer.NotebookFormat, "version": 1, "snapshot": ref}); err != nil {
		return err
	}
	a.managed.snapshot = ref
	return nil
}

// ExportNotebookForRoaming derives stopped authority from this concrete owner.
// It cannot export an arbitrary filesystem profile or a shared Host based on a
// caller-supplied stopped flag. Unknown effects prevent export/reclaim.
func (a *Application) ExportNotebookForRoaming(ctx context.Context, epoch, version string) ([]byte, nodeplane.SnapshotRef, error) {
	if a.managed == nil {
		return nil, nodeplane.SnapshotRef{}, errors.New("Notebook export requires a managed native runtime owner")
	}
	lease, _ := a.managed.guard.Lease()
	if err := a.managed.guard.Quiesce(ctx, lease); err != nil {
		return nil, nodeplane.SnapshotRef{}, err
	}
	return memorytransfer.ExportNotebook(ctx, memorytransfer.NotebookExportOptions{Source: a.root, SourceStopped: true, Epoch: epoch, Version: version})
}

// PrepareRoamingBootstrap is called on the original live native APP by an
// explicit coordinator-enable action. It proves and stops that APP's exact
// owned Codex lifetime before exporting genesis; it cannot adopt another APP's
// profile or stop a discovered shared App Server/Caelis Host.
func (a *Application) PrepareRoamingBootstrap(ctx context.Context, nodeID string) ([]byte, nodeplane.SnapshotRef, nodeplane.RuntimeProofPort, error) {
	fail := func(err error) ([]byte, nodeplane.SnapshotRef, nodeplane.RuntimeProofPort, error) {
		return nil, nodeplane.SnapshotRef{}, nil, err
	}
	if nodeID == "" {
		return fail(errors.Join(ErrNodeRoamingPreflight, errors.New("bootstrap requires the actual source node identity")))
	}
	native, ok := a.engine.(*codex.Session)
	if !ok || !native.OwnsLiveRuntime() {
		return fail(errors.Join(ErrNodeRoamingPreflight, errors.New("bootstrap requires this APP's live owned Codex runtime")))
	}
	if err := a.PrepareUpdate(); err != nil {
		return fail(errors.Join(ErrNodeRoamingPreflight, err))
	}
	if err := a.guardRuntimeChange(); err != nil {
		a.CancelUpdate()
		return fail(errors.Join(ErrNodeRoamingPreflight, err))
	}
	if err := native.FenceOwnedForBootstrap(ctx); err != nil {
		_ = a.Close()
		return fail(err)
	}
	if err := a.retireRoamingSource(ctx); err != nil {
		return fail(err)
	}
	if err := a.guardRuntimeChange(); err != nil {
		return fail(err)
	}
	payload, ref, err := memorytransfer.ExportNotebook(ctx, memorytransfer.NotebookExportOptions{Source: a.root, SourceStopped: true, Epoch: "0", Version: "1"})
	if err != nil {
		return fail(err)
	}
	target := api.WorkTarget{NodeID: nodeID, Backend: "codex", Role: api.RoleBot}
	port := &preparedNotebookSource{target: target, ref: ref, generation: rand.Text()}
	return payload, ref, port, nil
}

// retireRoamingSource closes every original native/local writer while retaining
// the stable native facade and its management observers for the thin client.
// It is used only after successful hard fencing of the actual live source.
func (a *Application) retireRoamingSource(ctx context.Context) error {
	a.mu.Lock()
	a.sourceRetired = true
	a.started = false
	if a.cancel != nil {
		a.cancel()
	}
	resident, bridge := a.companion, a.bridge
	if resident != nil {
		resident.Stop()
	}
	a.mu.Unlock()
	var err error
	if a.workerNodes != nil {
		err = errors.Join(err, a.workerNodes.Close())
	}
	err = errors.Join(err, a.engine.Close(ctx))
	if resident != nil {
		resident.Close()
	}
	if bridge != nil {
		bridge.Close()
	}
	a.workers.Wait()
	if a.notebook != nil {
		err = errors.Join(err, a.notebook.Close())
	}
	if a.personal != nil {
		err = errors.Join(err, a.personal.Close())
	}
	a.mu.Lock()
	a.companion, a.bridge, a.tasks, a.notebook, a.personal = nil, nil, nil, nil, nil
	a.cancel = nil
	a.mu.Unlock()
	return err
}

type preparedNotebookSource struct {
	target     api.WorkTarget
	ref        nodeplane.SnapshotRef
	generation string
}

func (p *preparedNotebookSource) ReadRuntimeProof(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	if err := ctx.Err(); err != nil {
		return nodeplane.RuntimeEligibility{}, err
	}
	if target != p.target {
		return nodeplane.RuntimeEligibility{}, errors.New("prepared Notebook source does not own this target")
	}
	return nodeplane.RuntimeEligibility{Proof: nodeplane.RuntimeProof{NodeID: target.NodeID, Backend: api.NodeBackend(target.Backend), Epoch: p.generation, Controllable: true}, Snapshot: p.ref, SafeIdle: true}, nil
}
