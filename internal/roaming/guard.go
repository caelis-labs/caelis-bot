// Package roaming owns the optional leased execution lifetime. It is never
// constructed by the default local application path.
package roaming

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

var ErrFenced = errors.New("node execution lease is unavailable; a confirmed new activation is required")

// OwnedRuntime must stop the exact runtime and descendants, withdraw all new
// worker grants, and report failure when termination cannot be confirmed.
// Unknown previously dispatched external effects are never retried by Guard.
type OwnedRuntime interface {
	WithdrawWorkerGrants(context.Context) error
	Stop(context.Context) error
	SafeIdle(context.Context) error
}

const StopMargin = 15 * time.Second

type ticketKey struct{}
type ticket struct {
	guard *Guard
	epoch string
}

type Guard struct {
	mu           sync.Mutex
	runtime      OwnedRuntime
	node         string
	backend      api.NodeBackend
	eligible     bool
	lease        nodeplane.Lease
	deadline     time.Time
	requestStart time.Time
	now          func() time.Time
	clockBase    time.Time
	timer        *time.Timer
	life         context.Context
	cancel       context.CancelFunc
	active       bool
	stopped      bool
	pending      int
	drained      chan struct{}
	stoppedDone  chan struct{}
	stopErr      error
}

// NewGuard does not grant authority. Eligibility must come from the concrete
// native owner, never from renderer facts or a remote catalog controllable bit.
func NewGuard(node string, backend api.NodeBackend, runtime OwnedRuntime, eligible bool) *Guard {
	return &Guard{node: node, backend: backend, runtime: runtime, eligible: eligible, now: time.Now, clockBase: time.Now(), stoppedDone: make(chan struct{})}
}

// Install uses request start, not response arrival or informational ExpiresAt.
// RequestStart must be captured locally with time.Now; serialized wall times
// cannot supply a monotonic lease. Only the same live epoch may be renewed.
func (g *Guard) Install(lease nodeplane.Lease, requestStart time.Time) error {
	if requestStart == requestStart.Round(0) {
		return errors.New("lease request start has no monotonic clock")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.eligible || g.runtime == nil || g.stopped || lease.BotID == "" || lease.NodeID != g.node || lease.Backend != g.backend || lease.Epoch == "" || lease.TTLMs <= StopMargin.Milliseconds() || lease.TTLMs > 60000 {
		return ErrFenced
	}
	deadline := requestStart.Add(time.Duration(lease.TTLMs)*time.Millisecond - StopMargin)
	if !g.now().Before(deadline) || !g.now().Round(0).Before(deadline.Round(0)) || requestStart.After(g.now()) {
		return ErrFenced
	}
	if g.active && (!g.live(g.now()) || g.lease.BotID != lease.BotID || g.lease.Epoch != lease.Epoch || !requestStart.After(g.requestStart) || !deadline.After(g.deadline)) {
		return ErrFenced
	}
	if !g.active {
		g.life, g.cancel = context.WithCancel(context.Background())
		g.drained = make(chan struct{})
	}
	g.lease, g.deadline, g.requestStart, g.active = lease, deadline, requestStart, true
	if g.timer != nil {
		g.timer.Stop()
	}
	epoch, expected := lease.Epoch, deadline
	g.timer = time.AfterFunc(time.Until(deadline), func() {
		g.revokeIf(epoch, expected)
	})
	return nil
}

func (g *Guard) Begin(parent context.Context) (context.Context, func(), error) {
	g.mu.Lock()
	if parent.Err() != nil {
		g.mu.Unlock()
		return parent, func() {}, parent.Err()
	}
	if !g.active || !g.live(g.now()) {
		expired, epoch, deadline := g.active, g.lease.Epoch, g.deadline
		g.mu.Unlock()
		if expired {
			g.revokeIf(epoch, deadline)
		}
		return parent, func() {}, ErrFenced
	}
	life, epoch := g.life, g.lease.Epoch
	g.pending++
	g.mu.Unlock()
	ctx, cancel := context.WithCancel(context.WithValue(parent, ticketKey{}, ticket{g, epoch}))
	stop := context.AfterFunc(life, cancel)
	var once sync.Once
	release := func() {
		once.Do(func() {
			stop()
			cancel()
			g.mu.Lock()
			g.pending--
			if g.stopped && g.pending == 0 {
				close(g.drained)
			}
			g.mu.Unlock()
		})
	}
	return ctx, release, nil
}

func (g *Guard) CheckContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t, ok := ctx.Value(ticketKey{}).(ticket)
	g.mu.Lock()
	valid := ok && t.guard == g && g.active && t.epoch == g.lease.Epoch && g.live(g.now())
	g.mu.Unlock()
	if !valid {
		return ErrFenced
	}
	return nil
}

func (g *Guard) Check(ctx context.Context, lease nodeplane.Lease) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.active || lease.BotID != g.lease.BotID || lease.NodeID != g.node || lease.Backend != g.backend || lease.Epoch != g.lease.Epoch || !g.live(g.now()) {
		return ErrFenced
	}
	return nil
}
func (g *Guard) Proof(ctx context.Context, lease nodeplane.Lease) (nodeplane.RuntimeProof, error) {
	g.mu.Lock()
	active, stopped := g.active, g.stopped
	eligible := g.eligible && g.runtime != nil && lease.NodeID == g.node && lease.Backend == g.backend && lease.BotID != "" && lease.Epoch != ""
	g.mu.Unlock()
	if !eligible || stopped {
		return nodeplane.RuntimeProof{}, ErrFenced
	}
	if active {
		if err := g.Check(ctx, lease); err != nil {
			return nodeplane.RuntimeProof{}, err
		}
	} else if err := g.runtime.SafeIdle(ctx); err != nil {
		return nodeplane.RuntimeProof{}, err
	}
	return nodeplane.RuntimeProof{NodeID: g.node, Backend: g.backend, Epoch: lease.Epoch, Controllable: true}, nil
}

// Revoke is irreversible for this runtime generation. Wake, expired renewal,
// or a replacement epoch must construct a fresh isolated runtime instead.
func (g *Guard) Revoke() { g.revokeIf("", time.Time{}) }
func (g *Guard) revokeIf(epoch string, deadline time.Time) {
	g.mu.Lock()
	if epoch != "" && (!g.active || g.lease.Epoch != epoch || !g.deadline.Equal(deadline)) {
		g.mu.Unlock()
		return
	}
	if g.stopped {
		g.mu.Unlock()
		return
	}
	g.active, g.stopped = false, true
	if g.timer != nil {
		g.timer.Stop()
	}
	if g.cancel != nil {
		g.cancel()
	}
	if g.drained == nil {
		g.drained = make(chan struct{})
	}
	if g.pending == 0 {
		close(g.drained)
	}
	g.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), StopMargin)
		defer cancel()
		var err error
		if g.runtime != nil {
			err = g.runtime.WithdrawWorkerGrants(ctx)
			err = errors.Join(err, g.runtime.Stop(ctx))
		} else {
			err = ErrFenced
		}
		select {
		case <-g.drained:
		case <-ctx.Done():
			err = errors.Join(err, errors.New("native operations did not quiesce before lease expiry"))
		}
		g.mu.Lock()
		g.stopErr = err
		g.mu.Unlock()
		close(g.stoppedDone)
	}()
}
func (g *Guard) Quiesce(ctx context.Context, lease nodeplane.Lease) error {
	g.mu.Lock()
	matching := g.lease.BotID == lease.BotID && g.lease.NodeID == lease.NodeID && g.lease.Backend == lease.Backend && g.lease.Epoch == lease.Epoch
	g.mu.Unlock()
	if !matching {
		return ErrFenced
	}
	g.Revoke()
	return g.WaitStopped(ctx)
}
func (g *Guard) WaitStopped(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-g.stoppedDone:
	}
	g.mu.Lock()
	err := g.stopErr
	g.mu.Unlock()
	if err != nil {
		return err
	}
	return g.runtime.SafeIdle(ctx)
}

// Suspend revokes before sleep; Wake never restores a lease or replays work.
func (g *Guard) Suspend()    { g.Revoke() }
func (g *Guard) Wake() error { g.Revoke(); return ErrFenced }

// Remote workers need their own native lease-aware grant withdrawal proof.
// Existing observer detachment and controller-epoch DTOs do not provide it.
func (g *Guard) CheckWorkTarget(ctx context.Context, target api.WorkTarget) error {
	if err := g.CheckContext(ctx); err != nil {
		return err
	}
	if target.NodeID != api.LocalNodeID || target.Backend != string(g.backend) {
		return errors.New("selected Worker does not participate in the managed native lease fence")
	}
	return nil
}

// A wall/monotonic discontinuity fails closed as an additional boundary check.
// Native sleep hooks/inhibition still own already-running process effects.
func (g *Guard) live(now time.Time) bool {
	mono := now.Sub(g.clockBase)
	wall := now.Round(0).Sub(g.clockBase.Round(0))
	difference := wall - mono
	if difference > 2*time.Second || difference < -2*time.Second {
		return false
	}
	return now.Before(g.deadline) && now.Round(0).Before(g.deadline.Round(0))
}

func (g *Guard) IsStopped() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.stopped }
