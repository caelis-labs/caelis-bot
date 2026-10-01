package codex

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

// WorkerLeaseOptions is trusted target-native configuration. A dispatch frame
// can supply an epoch, never the broker endpoint, pairing or power binder.
type WorkerLeaseOptions struct {
	BrokerNodeID, RawBotID, SourceNode, SourceBackend string
	HelperPath                                        string
	Reader                                            nodeplane.WorkLeaseReader
	BindPower                                         func(context.Context, func(), func()) (func(), error)
}
type workerLeaseFence struct {
	w                *WorkerClient
	opts             WorkerLeaseOptions
	mu               sync.Mutex
	enabled, revoked bool
	grant            api.WorkerLeaseGrant
	deadline         time.Time
	life             context.Context
	cancel           context.CancelFunc
	release          func()
	stopOnce         sync.Once
	stopErr          error
	renew            func(context.Context, string, time.Time) error
}
type workerLeaseTicket struct{}

func newWorkerLeaseFence(w *WorkerClient, opts WorkerLeaseOptions) *workerLeaseFence {
	life, cancel := context.WithCancel(context.Background())
	return &workerLeaseFence{w: w, opts: opts, life: life, cancel: cancel}
}
func (f *workerLeaseFence) valid() bool {
	return f.opts.HelperPath != "" && f.opts.Reader != nil && f.opts.BindPower != nil && f.opts.BrokerNodeID != "" && f.opts.RawBotID != "" && f.opts.SourceNode != "" && (f.opts.SourceBackend == "codex" || f.opts.SourceBackend == "caelis")
}
func (f *workerLeaseFence) activate(ctx context.Context) error {
	f.mu.Lock()
	already := f.enabled
	revoked := f.revoked
	f.mu.Unlock()
	if revoked {
		return errors.New("Worker lease generation is revoked")
	}
	if already {
		return nil
	}
	f.w.engine.mu.Lock()
	owned := f.w.owned
	f.w.engine.mu.Unlock()
	if owned == nil {
		return errors.New("leased Worker has no isolated native process owner")
	}
	if _, ok := owned.rpc.conn.(interface {
		freezeOwned() error
		forceKillOwned() error
	}); !ok {
		f.revoke()
		return errors.New("leased Worker immediate owned process fence unavailable")
	}
	port, ok := owned.rpc.conn.(interface {
		renewOwnedLease(context.Context, string, time.Time) error
		ownedSupervisorLive() bool
	})
	if !ok || !port.ownedSupervisorLive() {
		f.revoke()
		return errors.New("leased Worker independent watchdog unavailable")
	}
	f.renew = port.renewOwnedLease
	if err := owned.toolCleanupError(); err != nil {
		f.revoke()
		return err
	}
	release, err := f.opts.BindPower(f.life, func() { f.revoke() }, func() { f.revoke() })
	if err != nil {
		f.revoke()
		return err
	}
	f.mu.Lock()
	if f.revoked {
		f.mu.Unlock()
		release()
		return errors.New("Worker power fence revoked during startup")
	}
	f.release = release
	f.enabled = true
	f.mu.Unlock()
	go f.maintain()
	if err := ctx.Err(); err != nil {
		f.revoke()
		return err
	}
	return nil
}
func (w *WorkerClient) LeaseAwareAdmission() bool {
	if w.lease == nil {
		return false
	}
	w.engine.mu.Lock()
	owned := w.owned
	ready := !w.engine.closing && w.engine.client != nil && w.engine.client.Err() == nil
	w.engine.mu.Unlock()
	if !ready || owned == nil {
		return false
	}
	if supervisor, ok := owned.rpc.conn.(interface{ ownedSupervisorLive() bool }); !ok || !supervisor.ownedSupervisorLive() {
		return false
	}
	w.lease.mu.Lock()
	defer w.lease.mu.Unlock()
	return w.lease.enabled && !w.lease.revoked
}
func (w *WorkerClient) checkWorkerLease(ctx context.Context, s api.WorkDispatchSource) error {
	if w.lease == nil {
		if s.Lease != (api.WorkerLeaseGrant{}) {
			return errors.New("Worker does not provide leased native admission")
		}
		return nil
	}
	return w.lease.check(ctx, s.Lease)
}
func (f *workerLeaseFence) check(ctx context.Context, g api.WorkerLeaseGrant) error {
	if g.Validate() != nil || g == (api.WorkerLeaseGrant{}) || g.BotID != f.opts.RawBotID || g.BrokerNodeID != f.opts.BrokerNodeID || g.SourceNodeID != f.opts.SourceNode || g.Backend != f.opts.SourceBackend {
		return errors.New("Worker source lease differs from trusted native pairing")
	}
	f.mu.Lock()
	invalid := !f.enabled || f.revoked || (f.grant != (api.WorkerLeaseGrant{}) && f.grant != g) || (!f.deadline.IsZero() && !time.Now().Before(f.deadline))
	f.mu.Unlock()
	if invalid {
		return errors.New("Worker lease is unavailable or expired")
	}
	start := time.Now()
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	stop := context.AfterFunc(f.life, cancel)
	defer func() { stop(); cancel() }()
	l, err := f.opts.Reader.ReadWorkerLease(bounded, nodeplane.WorkLeaseRef{BotID: g.BotID, BrokerNodeID: g.BrokerNodeID, SourceNode: g.SourceNodeID, SourceBackend: api.NodeBackend(g.Backend), Epoch: g.Epoch})
	if err != nil {
		f.revoke()
		return err
	}
	if l.BotID != g.BotID || l.NodeID != g.SourceNodeID || string(l.Backend) != g.Backend || l.Epoch != g.Epoch || l.TTLMs <= 15000 || l.TTLMs > 60000 {
		f.revoke()
		return errors.New("Worker broker returned no exact live source lease")
	}
	deadline := start.Add(time.Duration(l.TTLMs)*time.Millisecond - 15*time.Second)
	if f.renew == nil {
		return errors.New("Worker watchdog deadline port unavailable")
	}
	if err = f.renew(ctx, g.Epoch, deadline); err != nil {
		f.revoke()
		return err
	}
	f.mu.Lock()
	invalid = f.revoked || !time.Now().Before(deadline) || (f.grant != (api.WorkerLeaseGrant{}) && f.grant != g)
	if !invalid {
		f.grant = g
		f.deadline = deadline
	}
	f.mu.Unlock()
	if invalid {
		f.revoke()
		return errors.New("Worker lease expired before native admission")
	}
	return ctx.Err()
}
func (f *workerLeaseFence) Begin(ctx context.Context) (context.Context, func(), error) {
	if g, ok := ctx.Value(workerLeaseTicket{}).(api.WorkerLeaseGrant); ok {
		return f.beginGrant(ctx, g)
	}
	s, err := f.w.source.WorkDispatchSource(ctx)
	if err != nil {
		return ctx, func() {}, err
	}
	if err = s.Validate(); err != nil {
		return ctx, func() {}, err
	}
	return f.beginGrant(ctx, s.Lease)
}
func (f *workerLeaseFence) beginGrant(ctx context.Context, g api.WorkerLeaseGrant) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return ctx, func() {}, err
	}
	if err := f.check(ctx, g); err != nil {
		return ctx, func() {}, err
	}
	child, cancel := context.WithCancel(context.WithValue(ctx, workerLeaseTicket{}, g))
	stop := context.AfterFunc(f.life, cancel)
	return child, func() { stop(); cancel() }, nil
}
func (f *workerLeaseFence) CheckContext(ctx context.Context) error {
	g, ok := ctx.Value(workerLeaseTicket{}).(api.WorkerLeaseGrant)
	if !ok {
		return errors.New("Worker native lease admission ticket missing")
	}
	return f.check(ctx, g)
}
func (f *workerLeaseFence) maintain() {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	nextRefresh := time.Now().Add(10 * time.Second)
	for {
		if !f.w.LeaseAwareAdmission() {
			f.revoke()
			return
		}
		select {
		case <-f.life.Done():
			return
		case now := <-tick.C:
			f.mu.Lock()
			g, deadline := f.grant, f.deadline
			f.mu.Unlock()
			if g == (api.WorkerLeaseGrant{}) {
				if !now.Before(nextRefresh) {
					nextRefresh = now.Add(10 * time.Second)
					if f.renew == nil || f.renew(f.life, "", now.Add(45*time.Second)) != nil {
						f.revoke()
						return
					}
				}
				continue
			}
			if !now.Before(deadline) {
				f.revoke()
				return
			}
			if !now.Before(nextRefresh) {
				nextRefresh = now.Add(10 * time.Second)
				go func() { _ = f.check(f.life, g) }()
			}
		}
	}
}

// revoke closes admission before freezing and terminating the exact captured
// process. It bypasses engine.op, including queued native RPCs and approvals.
func (f *workerLeaseFence) revoke() {
	f.mu.Lock()
	f.revoked = true
	f.mu.Unlock()
	f.cancel()
	f.stopOnce.Do(func() {
		s := f.w.engine
		s.mu.Lock()
		s.closing = true
		s.cancelLife()
		s.state.Connection = "stopped"
		s.update()
		owned := f.w.owned
		s.mu.Unlock()
		if owned == nil {
			return
		}
		ports, ok := owned.rpc.conn.(interface {
			freezeOwned() error
			forceKillOwned() error
		})
		if !ok {
			f.stopErr = errors.New("Worker owned process fence unavailable")
			return
		}
		freezeErr := ports.freezeOwned()
		owned.captureTools()
		forceErr := ports.forceKillOwned()
		owned.Close()
		f.stopErr = errors.Join(freezeErr, forceErr, owned.toolCleanupError())
	})
}
func (f *workerLeaseFence) releasePower() {
	f.mu.Lock()
	r := f.release
	f.release = nil
	f.mu.Unlock()
	if r != nil {
		r()
	}
}

// beginControl derives a private native ticket only for an authenticated exact
// existing-task control. It never creates a resident activation or new epoch.
func (w *WorkerClient) beginControl(ctx context.Context, id string) (context.Context, func(), error) {
	if w.lease == nil {
		return ctx, func() {}, nil
	}
	w.engine.mu.Lock()
	task := w.engine.binding.Tasks[id]
	var original api.WorkDispatchSource
	if task != nil && task.WorkerSource != nil && task.View.Target != nil && *task.View.Target == w.target {
		original = *task.WorkerSource
	}
	w.engine.mu.Unlock()
	if original.Validate() != nil || original.Lease == (api.WorkerLeaseGrant{}) {
		return ctx, func() {}, errors.New("Worker control has no original owned lease")
	}
	current, err := w.source.WorkDispatchSource(ctx)
	if err != nil {
		pair, paired := workerwire.PairedControl(ctx)
		if !errors.Is(err, api.ErrWorkSourceInactive) || !paired || pair != w.pair || pair.Target != w.target || pair.BotID != api.ProfileBotID(original.Lease.BotID) || pair.SourceNode != original.NodeID || pair.SourceBackend != original.Backend {
			return ctx, func() {}, err
		}
		current = original
	}
	if current.Validate() != nil || current.Lease != original.Lease || current.NodeID != original.NodeID || current.Backend != original.Backend {
		return ctx, func() {}, errors.New("Worker control differs from original lease generation")
	}
	return w.lease.beginGrant(ctx, original.Lease)
}
