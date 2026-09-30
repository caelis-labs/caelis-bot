package nodecoord

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// This fixture exposes trusted native observations separately from requests.
type preferencePeers struct {
	mu     sync.Mutex
	states map[api.WorkTarget]nodeplane.RuntimeEligibility
}

func (p *preferencePeers) read(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.states[target]
	if !ok || ctx.Err() != nil {
		return nodeplane.RuntimeEligibility{}, ErrIneligible
	}
	return s, nil
}
func (p *preferencePeers) update(target api.WorkTarget, mutate func(*nodeplane.RuntimeEligibility)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.states[target]
	mutate(&s)
	p.states[target] = s
}
func (p *preferencePeers) drop(target api.WorkTarget) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.states, target)
}
func (p *preferencePeers) verify(ctx context.Context, r nodeplane.ClaimRequest) error {
	s, err := p.read(ctx, r.Target)
	if err != nil || s.Proof != r.Proof || s.Snapshot != r.Snapshot || !s.SafeIdle || s.Pending || s.Unknown || s.LeaseEpoch != "" {
		return ErrIneligible
	}
	return nil
}
func preferenceFixture(t *testing.T) (*Coordinator, *clock, *preferencePeers, nodeplane.Lease, nodeplane.ClaimRequest) {
	t.Helper()
	c, cl, ref := fixture(t)
	r := claim("remote", ref, "")
	native := claim("local", ref, "1")
	p := &preferencePeers{states: map[api.WorkTarget]nodeplane.RuntimeEligibility{
		r.Target:      {Proof: r.Proof, Snapshot: ref, SafeIdle: true},
		native.Target: {Proof: native.Proof, Snapshot: ref, SafeIdle: true},
	}}
	c.opts.Verify = p.verify
	c.opts.VerifyRenew = func(ctx context.Context, l nodeplane.Lease, r nodeplane.ClaimRequest) error {
		s, err := p.read(ctx, r.Target)
		if err != nil || s.Proof != r.Proof || s.Snapshot != r.Snapshot || s.LeaseEpoch != l.Epoch {
			return ErrIneligible
		}
		return nil
	}
	c.opts.PreferredNodeID = "local"
	c.opts.ReadOwnerEligibility = p.read
	l, err := c.Claim(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	p.update(r.Target, func(s *nodeplane.RuntimeEligibility) { s.LeaseEpoch = l.Epoch })
	return c, cl, p, l, native
}
func preferredIntentClaim(t *testing.T, c *Coordinator, r nodeplane.ClaimRequest) {
	t.Helper()
	_, err := c.Claim(context.Background(), r)
	requireError(t, err, ErrConflict)
}
func heartbeatAfter(t *testing.T, c *Coordinator, cl *clock, l nodeplane.Lease, d time.Duration) nodeplane.Lease {
	t.Helper()
	cl.advance(d)
	next, err := c.Heartbeat(context.Background(), l)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func TestPreferredStableSafeIdleReclaimPreservesFullOldTTL(t *testing.T) {
	c, cl, _, l, r := preferenceFixture(t)
	preferredIntentClaim(t, c, r)
	l = heartbeatAfter(t, c, cl, l, 10*time.Second)
	l = heartbeatAfter(t, c, cl, l, 10*time.Second)
	deadline := l.ExpiresAt
	cl.advance(10 * time.Second)
	_, err := c.Heartbeat(context.Background(), l)
	requireError(t, err, ErrConflict)
	if c.deadline != deadline {
		t.Fatal("reclaim changed old deadline")
	}
	preferredIntentClaim(t, c, r)
	_, err = c.Heartbeat(context.Background(), l)
	requireError(t, err, ErrIneligible)
	cl.advance(49 * time.Second)
	preferredIntentClaim(t, c, r)
	cl.advance(time.Second)
	next, err := c.Claim(context.Background(), r)
	if err != nil || next.NodeID != "local" || next.Epoch != "2" {
		t.Fatalf("grant %+v %v", next, err)
	}
}

func TestPreferredNeverEvictsBusyPendingOrUnknownOwner(t *testing.T) {
	for _, mode := range []string{"busy", "pending", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			c, cl, p, l, r := preferenceFixture(t)
			ownerTarget := c.state.Claim.Target
			p.update(ownerTarget, func(s *nodeplane.RuntimeEligibility) {
				s.SafeIdle = mode != "busy"
				s.Pending, s.Unknown = mode == "pending", mode == "unknown"
			})
			preferredIntentClaim(t, c, r)
			for i := 0; i < 9; i++ {
				l = heartbeatAfter(t, c, cl, l, 10*time.Second)
			}
			if c.state.Lease.NodeID != "remote" || !c.state.Claim.Proof.Controllable {
				t.Fatal("busy owner fenced")
			}
			p.update(ownerTarget, func(s *nodeplane.RuntimeEligibility) { s.SafeIdle = true; s.Pending = false; s.Unknown = false })
			cl.advance(10 * time.Second)
			_, err := c.Heartbeat(context.Background(), l)
			requireError(t, err, ErrConflict)
		})
	}
}

func TestPreferredCandidateLossAndChangedNativeProofResetStability(t *testing.T) {
	for _, mode := range []string{"gone", "proof", "pending", "unknown", "snapshot", "lease"} {
		t.Run(mode, func(t *testing.T) {
			c, cl, p, l, r := preferenceFixture(t)
			preferredIntentClaim(t, c, r)
			l = heartbeatAfter(t, c, cl, l, 10*time.Second)
			if mode == "gone" {
				p.drop(r.Target)
			} else {
				p.update(r.Target, func(s *nodeplane.RuntimeEligibility) {
					switch mode {
					case "proof":
						s.Proof.Epoch = "reprepared"
					case "pending":
						s.Pending = true
					case "unknown":
						s.Unknown = true
					case "snapshot":
						s.Snapshot.Version = "2"
					case "lease":
						s.LeaseEpoch = "foreign"
					}
				})
			}
			for i := 0; i < 4; i++ {
				l = heartbeatAfter(t, c, cl, l, 10*time.Second)
			}
			if c.preference.claim.BotID != "" {
				t.Fatal("invalid intent survived")
			}
			p.update(r.Target, func(s *nodeplane.RuntimeEligibility) {
				*s = nodeplane.RuntimeEligibility{Proof: r.Proof, Snapshot: r.Snapshot, SafeIdle: true}
			})
			preferredIntentClaim(t, c, r)
			l = heartbeatAfter(t, c, cl, l, 10*time.Second)
			l = heartbeatAfter(t, c, cl, l, 10*time.Second)
			cl.advance(10 * time.Second)
			_, err := c.Heartbeat(context.Background(), l)
			requireError(t, err, ErrConflict)
		})
	}
}

func TestPreferredObservationGapAndPublicationRestartWindow(t *testing.T) {
	c, cl, p, l, r := preferenceFixture(t)
	preferredIntentClaim(t, c, r)
	l = heartbeatAfter(t, c, cl, l, 25*time.Second)
	l = heartbeatAfter(t, c, cl, l, 10*time.Second)
	b, ref := payload(l.Epoch, "2", "new complete tree")
	if err := c.PublishSnapshot(context.Background(), l, ref, b); err != nil {
		t.Fatal(err)
	}
	p.update(c.state.Claim.Target, func(s *nodeplane.RuntimeEligibility) { s.Snapshot = ref })
	p.update(r.Target, func(s *nodeplane.RuntimeEligibility) { s.Snapshot = ref })
	r.Snapshot = ref
	preferredIntentClaim(t, c, r)
	l = heartbeatAfter(t, c, cl, l, 10*time.Second)
	l = heartbeatAfter(t, c, cl, l, 10*time.Second)
	cl.advance(10 * time.Second)
	_, err := c.Heartbeat(context.Background(), l)
	requireError(t, err, ErrConflict)
}

func TestPreferredAbsentConfigurationAndMissingOwnerReaderLeaveRenewalUnchanged(t *testing.T) {
	for _, mode := range []string{"disabled", "missing-reader", "no-intent"} {
		t.Run(mode, func(t *testing.T) {
			c, cl, _, l, r := preferenceFixture(t)
			if mode == "disabled" {
				c.opts.PreferredNodeID = ""
			}
			if mode == "missing-reader" {
				c.opts.ReadOwnerEligibility = nil
			}
			if mode != "no-intent" {
				preferredIntentClaim(t, c, r)
			}
			for i := 0; i < 8; i++ {
				l = heartbeatAfter(t, c, cl, l, 10*time.Second)
			}
		})
	}
}

func TestPreferredRestartForgetsCandidateAndWaitsFullQuarantine(t *testing.T) {
	c, cl, p, l, r := preferenceFixture(t)
	preferredIntentClaim(t, c, r)
	l = heartbeatAfter(t, c, cl, l, 10*time.Second)
	o := c.opts
	c.Close()
	restarted, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if restarted.preference.claim.BotID != "" {
		t.Fatal("restart retained intent")
	}
	cl.advance(59 * time.Second)
	_, err = restarted.Claim(context.Background(), r)
	requireError(t, err, ErrUnavailable)
	cl.advance(time.Second)
	p.update(c.state.Claim.Target, func(s *nodeplane.RuntimeEligibility) { s.LeaseEpoch = "" })
	_, err = restarted.Claim(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPreferredConcurrentClaimsAndHeartbeatRetainOneOwner(t *testing.T) {
	c, cl, _, l, r := preferenceFixture(t)
	preferredIntentClaim(t, c, r)
	l = heartbeatAfter(t, c, cl, l, 10*time.Second)
	l = heartbeatAfter(t, c, cl, l, 10*time.Second)
	cl.advance(10 * time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			_, err := c.Claim(context.Background(), r)
			if !errors.Is(err, ErrConflict) {
				t.Errorf("candidate %v", err)
			}
		})
		wg.Go(func() {
			_, err := c.Heartbeat(context.Background(), l)
			if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrIneligible) {
				t.Errorf("heartbeat %v", err)
			}
		})
	}
	wg.Wait()
	if c.state.Lease.Epoch != l.Epoch || c.deadline != l.ExpiresAt {
		t.Fatal("concurrent regrant")
	}
}

func TestPreferredOwnerReadRequiresExactTrustedScopeAndGeneration(t *testing.T) {
	for _, mode := range []string{"unavailable", "generation", "node", "backend", "snapshot", "lease", "uncontrollable"} {
		t.Run(mode, func(t *testing.T) {
			c, cl, p, l, r := preferenceFixture(t)
			preferredIntentClaim(t, c, r)
			c.opts.ReadOwnerEligibility = func(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
				s, err := p.read(ctx, target)
				switch mode {
				case "unavailable":
					err = ErrUnavailable
				case "generation":
					s.Proof.Epoch = "other-generation"
				case "node":
					s.Proof.NodeID = "foreign"
				case "backend":
					s.Proof.Backend = api.NodeCaelis
				case "snapshot":
					s.Snapshot.Version = "stale"
				case "lease":
					s.LeaseEpoch = "other-lease"
				case "uncontrollable":
					s.Proof.Controllable = false
				}
				return s, err
			}
			for i := 0; i < 5; i++ {
				l = heartbeatAfter(t, c, cl, l, 10*time.Second)
			}
			if !c.state.Claim.Proof.Controllable {
				t.Fatal("untrusted owner read fenced renewal")
			}
		})
	}
}

func TestPreferredTrustedReadCannotCrossDeadlineOrCancellation(t *testing.T) {
	for _, mode := range []string{"expiry", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			c, cl, p, l, r := preferenceFixture(t)
			preferredIntentClaim(t, c, r)
			l = heartbeatAfter(t, c, cl, l, 10*time.Second)
			l = heartbeatAfter(t, c, cl, l, 10*time.Second)
			deadline := c.deadline
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.opts.ReadOwnerEligibility = func(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
				s, err := p.read(ctx, target)
				if mode == "expiry" {
					cl.advance(nodeplane.DefaultLeaseExpiry)
				} else {
					cancel()
				}
				return s, err
			}
			cl.advance(10 * time.Second)
			_, err := c.Heartbeat(ctx, l)
			if mode == "expiry" {
				requireError(t, err, ErrConflict)
			} else {
				requireError(t, err, context.Canceled)
			}
			if c.deadline != deadline {
				t.Fatal("slow observation renewed lease")
			}
		})
	}
}

func TestPreferredClaimWireProofCannotCreateUnpairedIntent(t *testing.T) {
	c, cl, p, l, r := preferenceFixture(t)
	p.drop(r.Target)
	_, err := c.Claim(context.Background(), r)
	requireError(t, err, ErrIneligible)
	if c.preference.claim.BotID != "" {
		t.Fatal("wire proof created preferred intent")
	}
	for i := 0; i < 5; i++ {
		l = heartbeatAfter(t, c, cl, l, 10*time.Second)
	}
}
