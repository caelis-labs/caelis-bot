package caelis

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/workerlease"
)

// LeasedWorker owns a private target-side foreground Host and its descendants.
// The embedded WorkerClient exposes no resident Bot or Host control surface.
// Trusted broker/source pins come from native assembly, never dispatch arguments.
type LeasedWorker struct {
	*WorkerClient
	fence *workerlease.Fence
}

// NewLeasedWorker launches an independent bounded application Worker Host.
// Endpoint, Source and lifetime callbacks are installed from the actual native
// owner; supplied callback values cannot manufacture lease-aware capability.
func NewLeasedWorker(ctx context.Context, opts WorkerOptions, owner OwnedHostOptions, authority workerlease.Options) (*LeasedWorker, error) {
	if !filepath.IsAbs(opts.Directory) || opts.Target.Validate() != nil || opts.Target.Backend != "caelis" || opts.Target.Role != api.RoleWorker || owner.NodeID != opts.Target.NodeID || opts.Source == nil || authority.Reader == nil || authority.BotID == "" || authority.BrokerNodeID == "" || authority.SourceNode == "" || (authority.SourceBackend != "codex" && authority.SourceBackend != "caelis") {
		return nil, errors.New("owned Caelis Worker requires exact native target and trusted source/broker pins")
	}
	if opts.Protocol != "" && opts.Protocol != WorkerProtocolBoundedApplication {
		return nil, errors.New("owned leased Caelis Worker requires bounded application protocol")
	}
	h, err := startOwnedHost(ctx, owner)
	if err != nil {
		return nil, err
	}
	opts.Protocol = WorkerProtocolBoundedApplication
	var fence *workerlease.Fence
	opts.Endpoint = func(ctx context.Context) (WorkerEndpoint, error) {
		if err := h.check(ctx); err != nil {
			return WorkerEndpoint{}, err
		}
		probe, err := workerBootstrap(ctx, WorkerBootstrapRequest{Action: "probe", Protocol: opts.Protocol, Store: h.settings.CaelisStore})
		if err != nil {
			return WorkerEndpoint{}, err
		}
		return WorkerEndpoint{Capabilities: probe.Capabilities, Execution: probe.Execution, ModelConfigured: probe.ModelConfigured, ModelAuth: probe.ModelAuth, Origin: probe.Endpoint, StoreID: probe.StoreID, InstanceID: probe.InstanceID, PrincipalID: probe.PrincipalID, Enroll: func(ctx context.Context, op, credential string) (wire.ApplicationConnection, error) {
			if err := fence.CheckContext(ctx); err != nil {
				return wire.ApplicationConnection{}, err
			}
			enrolled, err := workerBootstrapAdmitted(ctx, WorkerBootstrapRequest{Action: "enroll", Protocol: opts.Protocol, Store: h.settings.CaelisStore, StoreID: probe.StoreID, InstanceID: probe.InstanceID, PrincipalID: probe.PrincipalID, OperationID: op, AppCredential: credential}, fence)
			if err != nil {
				return wire.ApplicationConnection{}, err
			}
			if enrolled.Connection == nil {
				return wire.ApplicationConnection{}, errors.New("owned Worker enrollment unconfirmed")
			}
			return *enrolled.Connection, nil
		}}, nil
	}
	w := NewWorker(opts)
	w.engine.owned = h
	authority.Source = opts.Source
	authority.Ready = func(ctx context.Context) error { return h.ready(ctx, opts.Execution.Model) }
	authority.Renew = h.process.ConfigureDeadline
	authority.Stop = func(ctx context.Context) error {
		w.engine.mu.Lock()
		w.engine.issue = "owned Worker lease is unavailable"
		w.engine.bumpLocked()
		w.engine.mu.Unlock()
		return w.engine.FenceStop(ctx)
	}
	fence, err = workerlease.New(authority)
	if err != nil {
		return nil, errors.Join(err, h.stop(context.Background()))
	}
	w.engine.workerLease = fence
	return &LeasedWorker{WorkerClient: w, fence: fence}, nil
}

func (w *LeasedWorker) LeaseAwareAdmission() bool           { return w.fence.Capability() }
func (w *LeasedWorker) Revoke()                             { w.fence.Revoke() }
func (w *LeasedWorker) FenceStop(ctx context.Context) error { return w.fence.Close(ctx) }
func (w *LeasedWorker) Close(ctx context.Context) error {
	return errors.Join(w.fence.Close(ctx), w.WorkerClient.Close(ctx))
}

var _ api.LeaseAwareWorkRuntime = (*LeasedWorker)(nil)

func (w *LeasedWorker) WorkAdmission(ctx context.Context) error {
	ctx, release, err := w.fence.Begin(ctx)
	if err != nil {
		return err
	}
	defer release()
	return w.WorkerClient.WorkAdmission(ctx)
}
func (w *LeasedWorker) PrepareWorkWorkspace(ctx context.Context, id, workspace string, selected bool) error {
	ctx, release, err := w.fence.Begin(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := w.fence.CheckContext(ctx); err != nil {
		return err
	}
	return w.WorkerClient.PrepareWorkWorkspace(ctx, id, workspace, selected)
}

// LeaseError preserves the native fence reason for host-side diagnostics.
func (w *LeasedWorker) LeaseError() error { return w.fence.Err() }
