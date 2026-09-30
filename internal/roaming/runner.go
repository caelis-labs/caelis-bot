package roaming

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"path/filepath"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/memorytransfer"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// Broker is the authenticated single-user authority. CommitInstall must hold
// latest CAS through the offline generation's final install; it is not UI state.
type Broker interface {
	nodeplane.Coordinator
	nodeplane.SnapshotReader
	nodeplane.SnapshotPublisher
	SnapshotState(context.Context) (nodeplane.Lease, nodeplane.SnapshotRef, error)
	CommitInstall(context.Context, nodeplane.SnapshotRef, func() error) error
}

type ManagedRuntime interface {
	nodeplane.RuntimeProofPort
	Start() error
	Close() error
	PauseNotebook(context.Context) (func(), error)
	SetNotebookSnapshot(context.Context, nodeplane.Lease, nodeplane.SnapshotRef) error
}

type RuntimeFactory func(context.Context, string, api.WorkTarget) (ManagedRuntime, *Guard, error)

type RunnerOptions struct {
	BotID          string
	Target         api.WorkTarget
	GenerationRoot string
	Broker         Broker
	Factory        RuntimeFactory
	// RegisterOwner installs the exact newly assembled native proof port in the
	// paired agent. It must not accept self-reported catalog controllable flags.
	RegisterOwner func(api.WorkTarget, nodeplane.RuntimeProofPort) error
}

type Runner struct {
	opts     RunnerOptions
	runtime  ManagedRuntime
	guard    *Guard
	profile  string
	snapshot nodeplane.SnapshotRef
}

func NewRunner(o RunnerOptions) (*Runner, error) {
	if o.BotID == "" || o.Target.NodeID == "" || o.Target.Backend != "codex" || o.Target.Role != api.RoleBot || !filepath.IsAbs(o.GenerationRoot) || o.Broker == nil || o.Factory == nil || o.RegisterOwner == nil {
		return nil, errors.New("managed roaming requires an exact paired native owner and coordinator")
	}
	return &Runner{opts: o}, nil
}

// Activate imports only the latest complete Notebook into an absent generation,
// then obtains a lease before starting a fresh native session. No old operation,
// task, receipt, Wake, history or local Memory is adopted or replayed.
func (r *Runner) Activate(ctx context.Context) error {
	if r.runtime == nil {
		if err := r.Prepare(ctx); err != nil {
			return err
		}
	}
	return r.TryClaim(ctx)
}

// Prepare retains one safely idle candidate across ordinary claim conflicts.
// Its generation proof remains stable while an existing owner stays active.
func (r *Runner) Prepare(ctx context.Context) error {
	if r.runtime != nil {
		return errors.New("runner generation already assembled")
	}
	_, latest, err := r.opts.Broker.SnapshotState(ctx)
	if err != nil {
		return err
	}
	if latest.BotID != r.opts.BotID {
		return errors.New("latest Notebook belongs to another Bot")
	}
	payload, err := r.opts.Broker.ReadSnapshot(ctx, latest)
	if err != nil {
		return err
	}
	profile := filepath.Join(r.opts.GenerationRoot, "generation-"+rand.Text())
	result, err := memorytransfer.ApplyNotebook(ctx, memorytransfer.NotebookApplyOptions{Payload: payload, Destination: profile, DestinationStopped: true, Expected: latest, Commit: r.opts.Broker.CommitInstall})
	if err != nil {
		return err
	}
	if !result.Activated {
		return errors.New("Notebook generation was not installed")
	}
	native, guard, err := r.opts.Factory(ctx, profile, r.opts.Target)
	if err != nil {
		return err
	}
	r.runtime, r.guard, r.profile, r.snapshot = native, guard, profile, latest
	if err = r.opts.RegisterOwner(r.opts.Target, native); err != nil {
		guard.Revoke()
		return errors.Join(err, native.Close())
	}
	return nil
}
func (r *Runner) TryClaim(ctx context.Context) error {
	if r.runtime == nil {
		return errors.New("prepare a native generation before claiming")
	}
	if _, active := r.guard.Lease(); active {
		return errors.New("runner generation is already leased")
	}
	previous, latest, err := r.opts.Broker.SnapshotState(ctx)
	if err != nil {
		return err
	}
	if latest != r.snapshot {
		r.guard.Revoke()
		_ = r.runtime.Close()
		r.runtime, r.guard = nil, nil
		return errors.New("latest Notebook changed; prepare a fresh generation")
	}
	proof, err := r.runtime.ReadRuntimeProof(ctx, r.opts.Target)
	if err != nil {
		return err
	}
	if !proof.SafeIdle || proof.Pending || proof.Unknown || proof.Snapshot != latest || proof.LeaseEpoch != "" {
		return errors.New("new node is not safely idle at the current Notebook")
	}
	started := time.Now()
	lease, err := r.opts.Broker.Claim(ctx, nodeplane.ClaimRequest{BotID: r.opts.BotID, Target: r.opts.Target, ExpectedEpoch: previous.Epoch, Snapshot: latest, Proof: proof.Proof})
	if err != nil {
		return err
	} // retain the exact closed-admission candidate across conflicts
	if err = r.guard.Install(lease, started); err != nil {
		r.guard.Revoke()
		return err
	}
	if err = r.runtime.Start(); err != nil {
		r.guard.Revoke()
		return err
	}
	return nil
}

// Run combines live renewal with periodic whole-Notebook publication. A busy
// owner retains the previous complete cold bundle and tries the next period.
func (r *Runner) Run(ctx context.Context) error {
	if r.runtime == nil || r.guard == nil {
		return errors.New("activate the managed generation before running")
	}
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	renewDone := make(chan error, 1)
	go func() { renewDone <- r.guard.Maintain(life, r.opts.Broker) }()
	ticker := time.NewTicker(nodeplane.DefaultSnapshotInterval)
	defer ticker.Stop()
	for {
		select {
		case err := <-renewDone:
			return err
		case <-ctx.Done():
			cancel()
			return <-renewDone
		case <-ticker.C:
			if err := r.Publish(ctx); err != nil {
				if errors.Is(err, ErrFenced) {
					cancel()
					return <-renewDone
				}
			}
		}
	}
}
func (r *Runner) Publish(ctx context.Context) error {
	lease, active := r.guard.Lease()
	if !active {
		return ErrFenced
	}
	resume, err := r.runtime.PauseNotebook(ctx)
	if err != nil {
		return err
	}
	defer resume()
	if err = r.guard.Check(ctx, lease); err != nil {
		return err
	}
	latest, err := r.opts.Broker.LatestSnapshot(ctx, r.opts.BotID)
	if err != nil {
		return err
	}
	version, ok := new(big.Int).SetString(latest.Version, 10)
	if !ok {
		return errors.New("invalid current Notebook version")
	}
	version.Add(version, big.NewInt(1))
	payload, ref, err := memorytransfer.ExportNotebook(ctx, memorytransfer.NotebookExportOptions{Source: r.profile, SourceStopped: true, Epoch: lease.Epoch, Version: version.String()})
	if err != nil {
		return err
	}
	if err = r.guard.Check(ctx, lease); err != nil {
		return err
	}
	if err = r.opts.Broker.PublishSnapshot(ctx, lease, ref, payload); err != nil {
		return err
	}
	return r.runtime.SetNotebookSnapshot(ctx, lease, ref)
}

// Stop withdraws the lease only after native termination and zero unresolved
// operation proof. Failure preserves broker expiry and all unknown outcomes.
func (r *Runner) Stop(ctx context.Context) error {
	if r.guard == nil {
		return nil
	}
	lease, _ := r.guard.Lease()
	if err := r.guard.Quiesce(ctx, lease); err != nil {
		return err
	}
	return r.opts.Broker.Release(ctx, lease)
}
