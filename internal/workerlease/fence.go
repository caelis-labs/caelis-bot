// Package workerlease supplies native target admission for independently owned
// Worker runtimes. Broker endpoints and ownership callbacks are native assembly
// configuration; dispatch frames provide only an exact source grant.
package workerlease

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

var ErrFenced = errors.New("owned Worker lease is unavailable")

type Options struct {
	BrokerNodeID, BotID, SourceNode, SourceBackend string
	Reader                                         nodeplane.WorkLeaseReader
	Source                                         api.WorkSourceProvider
	Ready                                          func(context.Context) error
	Renew                                          func(context.Context, string, time.Time) error
	Stop                                           func(context.Context) error
}
type ticketKey struct{}
type Fence struct {
	opts     Options
	mu       sync.Mutex
	check    sync.Mutex
	life     context.Context
	cancel   context.CancelFunc
	grant    api.WorkerLeaseGrant
	deadline time.Time
	revoked  bool
	cause    error
	stopOnce sync.Once
	stopErr  error
	stopped  chan struct{}
}

func New(o Options) (*Fence, error) {
	if o.BrokerNodeID == "" || o.BotID == "" || o.SourceNode == "" || (o.SourceBackend != "codex" && o.SourceBackend != "caelis") || o.Reader == nil || o.Source == nil || o.Ready == nil || o.Renew == nil || o.Stop == nil {
		return nil, errors.New("owned Worker needs complete trusted broker and native watchdog ports")
	}
	life, cancel := context.WithCancel(context.Background())
	f := &Fence{opts: o, life: life, cancel: cancel, stopped: make(chan struct{})}
	if !f.Capability() {
		cancel()
		return nil, errors.Join(errors.New("owned Worker native readiness is unconfirmed"), f.Err(), f.stopErr)
	}
	go f.maintain()
	return f, nil
}
func (f *Fence) Capability() bool {
	f.mu.Lock()
	revoked, deadline := f.revoked, f.deadline
	f.mu.Unlock()
	if revoked || (!deadline.IsZero() && !live(deadline)) {
		f.Revoke()
		return false
	}
	ctx, cancel := context.WithTimeout(f.life, 2*time.Second)
	defer cancel()
	if err := f.opts.Ready(ctx); err != nil {
		_ = f.fail(err)
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.revoked && (f.deadline.IsZero() || live(f.deadline))
}
func live(deadline time.Time) bool {
	now := time.Now()
	return now.Before(deadline) && now.Round(0).Before(deadline.Round(0))
}

// Admit runs after original receipt reconciliation, immediately before a new
// native intent. It never changes/replays an original operation or its source.
func (f *Fence) Admit(ctx context.Context, source api.WorkDispatchSource) error {
	if err := source.Validate(); err != nil {
		return err
	}
	return f.confirm(ctx, source.Lease)
}
func (f *Fence) confirm(ctx context.Context, g api.WorkerLeaseGrant) error {
	if g.Validate() != nil || g == (api.WorkerLeaseGrant{}) || g.BrokerNodeID != f.opts.BrokerNodeID || g.BotID != f.opts.BotID || g.SourceNodeID != f.opts.SourceNode || g.Backend != f.opts.SourceBackend {
		return ErrFenced
	}
	f.check.Lock()
	defer f.check.Unlock()
	f.mu.Lock()
	invalid := f.revoked || (f.grant != (api.WorkerLeaseGrant{}) && f.grant != g) || (!f.deadline.IsZero() && !live(f.deadline))
	f.mu.Unlock()
	if invalid {
		f.Revoke()
		return ErrFenced
	}
	started := time.Now()
	request, cancel := context.WithTimeout(ctx, 3*time.Second)
	stop := context.AfterFunc(f.life, cancel)
	lease, err := f.opts.Reader.ReadWorkerLease(request, nodeplane.WorkLeaseRef{BotID: g.BotID, BrokerNodeID: g.BrokerNodeID, SourceNode: g.SourceNodeID, SourceBackend: api.NodeBackend(g.Backend), Epoch: g.Epoch})
	stop()
	cancel()
	if err != nil {
		return f.fail(err)
	}
	if lease.BotID != g.BotID || lease.NodeID != g.SourceNodeID || string(lease.Backend) != g.Backend || lease.Epoch != g.Epoch || lease.TTLMs <= 15000 || lease.TTLMs > 60000 {
		f.Revoke()
		return ErrFenced
	}
	deadline, err := lease.Deadline(started, 15*time.Second)
	if err != nil {
		return f.fail(err)
	}
	if !live(deadline) {
		f.Revoke()
		return ErrFenced
	}
	request, cancel = context.WithTimeout(ctx, 2*time.Second)
	stop = context.AfterFunc(f.life, cancel)
	err = f.opts.Renew(request, g.Epoch, deadline)
	stop()
	cancel()
	if err != nil {
		return f.fail(err)
	}
	f.mu.Lock()
	invalid = f.revoked || !live(deadline) || (f.grant != (api.WorkerLeaseGrant{}) && f.grant != g) || (!f.deadline.IsZero() && !live(f.deadline))
	if !invalid {
		f.grant, f.deadline = g, deadline
	}
	f.mu.Unlock()
	if invalid {
		f.Revoke()
		return ErrFenced
	}
	return ctx.Err()
}
func (f *Fence) Begin(ctx context.Context) (context.Context, func(), error) {
	if g, ok := ctx.Value(ticketKey{}).(api.WorkerLeaseGrant); ok {
		if err := f.confirm(ctx, g); err != nil {
			return ctx, func() {}, err
		}
		return f.ticket(ctx, g)
	}
	source, err := f.opts.Source.WorkDispatchSource(ctx)
	if err != nil {
		return ctx, func() {}, err
	}
	return f.BeginSource(ctx, source)
}

// BeginSource is for a persisted native intent, after its exact source and
// request have been reconciled. It cannot replace an established generation.
func (f *Fence) BeginSource(ctx context.Context, source api.WorkDispatchSource) (context.Context, func(), error) {
	if err := f.Admit(ctx, source); err != nil {
		return ctx, func() {}, err
	}
	return f.ticket(ctx, source.Lease)
}
func (f *Fence) ticket(ctx context.Context, g api.WorkerLeaseGrant) (context.Context, func(), error) {
	child, cancel := context.WithCancel(context.WithValue(ctx, ticketKey{}, g))
	stop := context.AfterFunc(f.life, cancel)
	return child, func() { stop(); cancel() }, nil
}
func (f *Fence) CheckContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g, ok := ctx.Value(ticketKey{}).(api.WorkerLeaseGrant)
	if !ok {
		return ErrFenced
	}
	return f.confirm(ctx, g)
}
func (f *Fence) maintain() {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	refresh := time.Now().Add(10 * time.Second)
	for {
		select {
		case <-f.life.Done():
			return
		case now := <-tick.C:
			f.mu.Lock()
			g, deadline := f.grant, f.deadline
			f.mu.Unlock()
			if g == (api.WorkerLeaseGrant{}) {
				continue
			}
			if !live(deadline) {
				f.Revoke()
				return
			}
			if !now.Before(refresh) {
				refresh = now.Add(10 * time.Second)
				go func() { _ = f.confirm(f.life, g) }()
			}
		}
	}
}

// Revoke closes tickets before the independent native owner hard fence. It is
// synchronous for native suspend callbacks and bypasses queued protocol locks.
func (f *Fence) Revoke() { f.revoke(ErrFenced) }

// Err retains the first reason admission was revoked, including asynchronous
// broker failures. Close separately reports native termination confirmation.
func (f *Fence) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cause
}
func (f *Fence) fail(err error) error {
	f.revoke(err)
	return errors.Join(err, f.stopErr)
}
func (f *Fence) revoke(err error) {
	f.mu.Lock()
	f.revoked = true
	if f.cause == nil {
		f.cause = err
	}
	f.mu.Unlock()
	f.cancel()
	f.stopOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		f.stopErr = f.opts.Stop(ctx)
		close(f.stopped)
	})
}
func (f *Fence) Close(ctx context.Context) error {
	f.Revoke()
	select {
	case <-f.stopped:
		return f.stopErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
