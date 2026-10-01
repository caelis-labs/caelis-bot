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
func NewManagedNode(root string, host Host, nodeID string) (*Application, *roaming.Guard, error) {
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

	resolve := func(id string) (providerFactory, error) {
		if id != "codex" {
			return providerFactory{}, errors.New("Caelis shared Host is not eligible for automatic Bot takeover")
		}
		f, err := resolveProvider(id)
		if err != nil {
			return f, err
		}
		f.Open = func(c providerConfig) (api.Engine, error) {
			if err := codex.ValidateSettings(c.Settings, c.Execution); err != nil {
				return nil, err
			}
			return codex.NewSession(codex.SessionOptions{Diagnostics: c.Diagnostics, Binary: c.Settings.CLIPath, Execution: c.Execution, WorkExecution: c.WorkExecution, Directory: c.WorkDirectory, WorkRoot: c.WorkRoot, StateFile: c.ConversationFile, ForceOwned: true}), nil
		}
		return f, nil
	}
	a, err := newApplication(root, host, resolve)
	if err != nil {
		return nil, nil, err
	}
	owner := &managedNodeOwner{app: a, nodeID: nodeID, generation: rand.Text(), snapshot: snapshot}
	guard := roaming.NewGuard(nodeID, api.NodeCodex, owner, true)
	owner.guard = guard
	owner.nativeFenced = make(chan struct{})
	owner.nativeFenceDone = make(chan struct{})
	a.managed = owner
	a.executionAdmission = guard
	a.Backend.ConfigureExecutionAdmission(guard)
	a.engine.(*codex.Session).ConfigureExecutionAdmission(guard)
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
	if o == nil || target.NodeID != o.nodeID || target.Backend != "codex" || target.Role != api.RoleBot {
		return nodeplane.RuntimeEligibility{}, errors.New("target has no managed native Bot owner")
	}
	lease, active := o.guard.Lease()
	leaseEpoch := lease.Epoch
	if !active {
		lease = nodeplane.Lease{BotID: snapshot.BotID, NodeID: o.nodeID, Backend: api.NodeCodex, Epoch: o.generation}
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
func ManagedNodeFactory(host Host) roaming.RuntimeFactory {
	return func(ctx context.Context, profile string, target api.WorkTarget) (roaming.ManagedRuntime, *roaming.Guard, error) {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		return NewManagedNode(profile, host, target.NodeID)
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
		proof, proofErr := a.ReadRuntimeProof(ctx, api.WorkTarget{NodeID: a.managed.nodeID, Backend: "codex", Role: api.RoleBot})
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
		return fail(errors.New("bootstrap requires the actual source node identity"))
	}
	native, ok := a.engine.(*codex.Session)
	if !ok || !native.OwnsLiveRuntime() {
		return fail(errors.New("bootstrap requires this APP's live owned Codex runtime"))
	}
	if err := a.PrepareUpdate(); err != nil {
		return fail(err)
	}
	if err := a.guardRuntimeChange(); err != nil {
		a.CancelUpdate()
		return fail(err)
	}
	if err := native.FenceOwnedForBootstrap(ctx); err != nil {
		_ = a.Close()
		return fail(err)
	}
	if err := a.Close(); err != nil {
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
