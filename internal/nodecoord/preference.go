package nodecoord

import (
	"context"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// These are broker policy, not request parameters or user-facing lease knobs.
const preferredStableWindow = 30 * time.Second
const preferredObservationGap = 2 * nodeplane.DefaultHeartbeatInterval

// One in-memory intent is enough: only one configured node may ask to reclaim.
// Restart deliberately forgets stability and retains the full lease quarantine.
type preferredIntent struct {
	claim nodeplane.ClaimRequest
	since time.Time
	seen  time.Time
}

func (c *Coordinator) isPreferred(r nodeplane.ClaimRequest) bool {
	return c.opts.PreferredNodeID != "" && r.Target.NodeID == c.opts.PreferredNodeID
}

func (c *Coordinator) invalidatePreferredClaim(r nodeplane.ClaimRequest) {
	if c.isPreferred(r) {
		c.preference = preferredIntent{}
	}
}

func (c *Coordinator) observePreferredClaim(now time.Time, r nodeplane.ClaimRequest) {
	if !c.isPreferred(r) || c.state.Lease.NodeID == c.opts.PreferredNodeID || r.ExpectedEpoch != c.state.Lease.Epoch {
		return
	}
	if r != c.preference.claim || now.Sub(c.preference.seen) > preferredObservationGap {
		c.preference = preferredIntent{claim: r, since: now}
	}
	c.preference.seen = now
}

// reclaimPreferred is called only after ordinary renewal proof has passed.
// Candidate failure must not revoke the current owner's authority. All reads
// here are trusted native callbacks; a claim's wire booleans prove nothing.
func (c *Coordinator) reclaimPreferred(ctx context.Context) error {
	p := &c.preference
	if !c.isPreferred(p.claim) || c.opts.ReadOwnerEligibility == nil || c.state.Lease.NodeID == c.opts.PreferredNodeID {
		return nil
	}
	if p.claim.Snapshot != c.state.Latest || p.claim.ExpectedEpoch != c.state.Lease.Epoch || c.verify(ctx, p.claim) != nil {
		c.preference = preferredIntent{}
		return nil
	}
	observed, err := c.tick(ctx)
	if err != nil {
		return err
	}
	if !c.active(observed) {
		return ErrConflict
	}
	if observed.Sub(p.seen) > preferredObservationGap {
		p.since = observed
	}
	p.seen = observed
	if observed.Sub(p.since) < preferredStableWindow {
		return nil
	}
	owner, err := c.opts.ReadOwnerEligibility(ctx, c.state.Claim.Target)
	if err != nil || owner.Proof != c.state.Claim.Proof || owner.Snapshot != c.state.Latest || owner.LeaseEpoch != c.state.Lease.Epoch || !owner.SafeIdle || owner.Pending || owner.Unknown {
		return nil
	}
	// A slow owner read must not count as continuous candidate presence. Check
	// the exact prepared proof again immediately before refusing renewal.
	if c.verify(ctx, p.claim) != nil {
		c.preference = preferredIntent{}
		return nil
	}
	checked, err := c.tick(ctx)
	if err != nil {
		return err
	}
	if !c.active(checked) {
		return ErrConflict
	}
	if checked.Sub(observed) > preferredObservationGap {
		p.since, p.seen = checked, checked
		return nil
	}
	// Fence all later renewal attempts even if the candidate subsequently goes
	// away. Preserve the complete existing deadline: no release or wire stop is
	// evidence that the old native owner has already stopped executing.
	s := c.state
	s.Claim.Proof.Controllable = false
	if err := c.persist(s); err != nil {
		return err
	}
	c.state = s
	c.preference = preferredIntent{}
	return ErrConflict
}
