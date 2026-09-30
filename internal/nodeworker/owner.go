// Package nodeworker keeps a node's native Worker lifetime separate from remote
// observers. The node service owns Stop; an SSH/HTTP observer owns only Detach.
package nodeworker

import (
	"context"
	"errors"
	"sync"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type Client interface {
	api.WorkRuntime
	api.WorkWorkspaceProvider
	api.WorkApprovalProvider
	Connect(context.Context) error
	Close(context.Context) error
	Snapshot() api.Snapshot
	WaitSnapshot(context.Context, uint64) (api.Snapshot, error)
}

type Owner struct {
	client  Client
	op      sync.Mutex
	mu      sync.Mutex
	stopped bool
	life    context.Context
	cancel  context.CancelFunc
	stopErr error
}

func New(client Client) *Owner {
	life, cancel := context.WithCancel(context.Background())
	return &Owner{client: client, life: life, cancel: cancel}
}

// Start (also an explicit reconnect) bounds connection setup with ctx, without
// deriving the native process lifetime from this request or observer context.
func (o *Owner) Start(ctx context.Context) error {
	o.op.Lock()
	defer o.op.Unlock()
	o.mu.Lock()
	stopped := o.stopped
	o.mu.Unlock()
	if stopped || o.client == nil {
		return errors.New("Worker owner unavailable")
	}
	return o.client.Connect(ctx)
}

func (o *Owner) Runtime() api.WorkRuntime             { return o.client }
func (o *Owner) Workspace() api.WorkWorkspaceProvider { return o.client }
func (o *Owner) Approvals() api.WorkApprovalProvider  { return o.client }

func (o *Owner) Stop(ctx context.Context) error {
	o.op.Lock()
	defer o.op.Unlock()
	o.mu.Lock()
	if o.stopped {
		err := o.stopErr
		o.mu.Unlock()
		return err
	}
	o.stopped = true
	o.cancel()
	o.mu.Unlock()
	if o.client != nil {
		o.stopErr = o.client.Close(ctx)
	}
	return o.stopErr
}

type Observer struct {
	owner     *Owner
	ctx       context.Context
	cancel    context.CancelFunc
	stopOwner func() bool
}

func (o *Owner) Observe(ctx context.Context) *Observer {
	ctx, cancel := context.WithCancel(ctx)
	if o.life.Err() != nil {
		cancel()
	}
	return &Observer{owner: o, ctx: ctx, cancel: cancel, stopOwner: context.AfterFunc(o.life, cancel)}
}

// Detach releases only this observer, preserving native work and other clients.
func (v *Observer) Detach() { v.stopOwner(); v.cancel() }
func (v *Observer) Snapshot() (api.Snapshot, error) {
	if err := v.ctx.Err(); err != nil {
		return api.Snapshot{}, err
	}
	if v.owner.client == nil {
		return api.Snapshot{}, errors.New("Worker owner unavailable")
	}
	return v.owner.client.Snapshot(), nil
}
func (v *Observer) WaitSnapshot(revision uint64) (api.Snapshot, error) {
	if err := v.ctx.Err(); err != nil {
		return api.Snapshot{}, err
	}
	if v.owner.client == nil {
		return api.Snapshot{}, errors.New("Worker owner unavailable")
	}
	return v.owner.client.WaitSnapshot(v.ctx, revision)
}
