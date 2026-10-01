package caelis

import (
	"context"
	"errors"
	"path/filepath"
	"slices"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/workerlease"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
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

// Connect prepares only authenticated native Host metadata. The node owner
// starts before a paired dispatcher can supply a real execution Source; it must
// not create application credentials, enroll, or activate a task at this stage.
func (w *LeasedWorker) Connect(ctx context.Context) error {
	s := w.engine
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	closed, loadErr := s.closed, s.loadErr
	s.mu.Unlock()
	if closed {
		return errors.New("Worker connection closed")
	}
	if loadErr != nil {
		return loadErr
	}
	if !w.fence.Capability() {
		return errors.Join(workerlease.ErrFenced, w.fence.Err())
	}
	ep, err := s.workerEndpoint(ctx)
	if err != nil {
		return s.fail(err)
	}
	for _, capability := range boundedWorkerRequired {
		if !slices.Contains(ep.Capabilities, capability) {
			return s.fail(errors.New("Worker public capability unavailable"))
		}
	}
	if ep.StoreID == "" || ep.InstanceID != s.owned.instance || ep.PrincipalID == "" {
		return s.fail(errors.New("owned Worker native metadata identity unavailable"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("Worker connection detached during metadata probe")
	}
	if err := w.fence.Err(); err != nil {
		return errors.Join(workerlease.ErrFenced, err)
	}
	if s.workerUseDefault {
		s.workExecution = ep.Execution
	}
	s.workerModelConfigured = ep.ModelConfigured && s.workExecution.Model != "" && s.workExecution.Model == ep.Execution.Model
	s.workerModelAuth = ep.ModelAuth
	s.workerPrepared = true
	s.issue = ""
	s.bumpLocked()
	return ctx.Err()
}
func (w *LeasedWorker) Reconnect(ctx context.Context) error { return w.Connect(ctx) }

// Mutation connection is established only with an authenticated current Source
// ticket. Original task/source IDs survive lazy enrollment and queueing.
func (w *LeasedWorker) connectMutation(ctx context.Context, source api.WorkDispatchSource) (context.Context, func(), error) {
	if err := w.engine.checkWorkerSource(ctx, source); err != nil {
		return ctx, func() {}, err
	}
	ticket, release, err := w.fence.BeginSource(ctx, source)
	if err != nil {
		return ctx, func() {}, err
	}
	if err := w.WorkerClient.Connect(ticket); err != nil {
		release()
		return ctx, func() {}, err
	}
	return ticket, release, nil
}
func (w *LeasedWorker) StartWork(ctx context.Context, in api.WorkStart) (api.Task, error) {
	if !validWorkerDigest(in.RequestDigest) || in.Target == nil || *in.Target != w.engine.workerTarget {
		return api.Task{}, errors.New("Worker durable request or route target unavailable")
	}
	w.engine.mu.Lock()
	_, recorded := w.engine.state.Workers[in.ID]
	w.engine.mu.Unlock()
	if !recorded {
		var release func()
		var err error
		ctx, release, err = w.connectMutation(ctx, in.Source)
		if err != nil {
			return api.Task{}, err
		}
		defer release()
	}
	return w.WorkerClient.StartWork(ctx, in)
}
func (w *LeasedWorker) SendWork(ctx context.Context, in api.TaskMessage) (api.Task, error) {
	if !validWorkerDigest(in.RequestDigest) {
		return api.Task{}, errors.New("Worker durable continuation digest unavailable")
	}
	w.engine.mu.Lock()
	_, recorded := w.engine.state.Workers[in.ID].Messages[in.RequestID]
	w.engine.mu.Unlock()
	if !recorded {
		var release func()
		var err error
		ctx, release, err = w.connectMutation(ctx, in.Source)
		if err != nil {
			return api.Task{}, err
		}
		defer release()
	}
	return w.WorkerClient.SendWork(ctx, in)
}

func (w *LeasedWorker) WorkAdmission(ctx context.Context) error {
	ctx, release, err := w.fence.Begin(ctx)
	if err != nil {
		return err
	}
	defer release()
	if err := w.Connect(ctx); err != nil {
		return err
	}
	w.engine.mu.Lock()
	configured, auth := w.engine.workerModelConfigured, w.engine.workerModelAuth
	w.engine.mu.Unlock()
	if !configured || auth == "reported_missing" {
		return errors.New("owned Worker native model is unavailable")
	}
	return w.fence.CheckContext(ctx)
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
	if err := w.WorkerClient.Connect(ctx); err != nil {
		return err
	}
	return w.WorkerClient.PrepareWorkWorkspace(ctx, id, workspace, selected)
}

// LeaseError preserves the native fence reason for host-side diagnostics.
func (w *LeasedWorker) LeaseError() error { return w.fence.Err() }

// Controls use a current native frame or authenticated paired idle principal,
// retaining the
// original task's lease generation and exact native cancellation/approval IDs.
// They never enroll a replacement application to act on an old native binding.
func (w *LeasedWorker) controlContext(ctx context.Context, id string) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return ctx, func() {}, err
	}
	w.engine.mu.Lock()
	original, exists := w.engine.state.Workers[id]
	connected := w.engine.connected && !w.engine.closed && w.engine.client != nil
	w.engine.mu.Unlock()
	if !exists || original.Task.Target == nil || *original.Task.Target != w.engine.workerTarget || original.Source.Validate() != nil || original.Source.Lease == (api.WorkerLeaseGrant{}) {
		return ctx, func() {}, errors.New("Worker control task does not retain owned lease authority")
	}
	source, err := w.engine.workerSource(ctx)
	if err != nil {
		pair, paired := workerwire.PairedControl(ctx)
		if !errors.Is(err, api.ErrWorkSourceInactive) || !paired || pair.Target != w.engine.workerTarget || pair.BotID != api.ProfileBotID(original.Source.Lease.BotID) || pair.SourceNode != original.Source.NodeID || pair.SourceBackend != original.Source.Backend {
			return ctx, func() {}, err
		}
		// Exact existing-task control uses its original authority. This source is
		// only a private fence ticket, never a fabricated current model activation.
		source = original.Source
	}
	if source.Validate() != nil || original.Source.Lease != source.Lease {
		return ctx, func() {}, workerlease.ErrFenced
	}
	if !connected {
		return ctx, func() {}, errors.New("original Worker native connection is unavailable")
	}
	return w.fence.BeginSource(ctx, source)
}
func (w *LeasedWorker) StopWork(ctx context.Context, id string) (api.Task, error) {
	ticket, release, err := w.controlContext(ctx, id)
	if err != nil {
		task, readErr := w.WorkerClient.ReadWork(ctx, id)
		return task, errors.Join(err, readErr)
	}
	defer release()
	w.engine.mu.Lock()
	original := w.engine.state.Workers[id]
	pending := false
	for _, operation := range w.engine.state.Operations {
		if operation.Path == "/sessions/"+idPath(original.Binding.SessionId)+"/cancel" && operation.Outcome == "unknown" {
			pending = true
			break
		}
	}
	w.engine.mu.Unlock()
	if pending {
		task, readErr := w.WorkerClient.ReadWork(ctx, id)
		return task, errors.Join(errors.New("original Worker cancellation remains unconfirmed"), readErr)
	}
	return w.WorkerClient.StopWork(ticket, id)
}
func (w *LeasedWorker) DecideWork(ctx context.Context, approval api.WorkApproval, decision api.Decision) error {
	if approval.Target != w.engine.workerTarget {
		return errors.New("Worker approval target mismatch")
	}
	ticket, release, err := w.controlContext(ctx, approval.TaskID)
	if err != nil {
		return err
	}
	defer release()
	return w.WorkerClient.DecideWork(ticket, approval, decision)
}
