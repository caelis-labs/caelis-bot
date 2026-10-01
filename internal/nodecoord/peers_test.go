package nodecoord

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"testing"
)

type ownedPeer struct {
	state nodeplane.RuntimeEligibility
	err   error
	reads int
}

func (p *ownedPeer) ReadRuntimeProof(ctx context.Context, _ api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	p.reads++
	return p.state, p.err
}
func TestPairedVerifierReadsOwnedSnapshotAndNativeEligibility(t *testing.T) {
	c, _, s := fixture(t)
	r := claim("n1", s, "")
	p := &ownedPeer{state: nodeplane.RuntimeEligibility{Proof: r.Proof, Snapshot: s, SafeIdle: true}}
	registry := NewPeerRegistry()
	if e := registry.Register(r.Target, p); e != nil {
		t.Fatal(e)
	}
	c.opts.Verify = registry.VerifyClaim
	c.opts.VerifyRenew = registry.VerifyRenew
	ctx := context.Background()
	l, e := c.Claim(ctx, r)
	if e != nil || p.reads != 1 {
		t.Fatalf("claim %+v %v reads %d", l, e, p.reads)
	}
	// The native owner must install this exact grant before it can be renewed.
	_, e = c.Heartbeat(ctx, l)
	requireError(t, e, ErrIneligible)
	p.state.LeaseEpoch = l.Epoch
	p.state.SafeIdle = false
	p.state.Pending = true
	if _, e = c.Heartbeat(ctx, l); e != nil {
		t.Fatal(e)
	}
	p.state.Unknown = true
	_, e = c.Heartbeat(ctx, l)
	if e != nil {
		t.Fatal("controlled unknown owner lost authority", e)
	}
	p.state.Unknown = false
	p.err = errors.New("partition")
	_, e = c.Heartbeat(ctx, l)
	requireError(t, e, ErrIneligible)
	p.err = nil
	p.state.Snapshot.Version = "old"
	_, e = c.Heartbeat(ctx, l)
	requireError(t, e, ErrIneligible)
}
func TestPairingRejectsSelfReportedProofForeignAndUnknown(t *testing.T) {
	_, _, s := fixture(t)
	ctx := context.Background()
	r := claim("n1", s, "")
	registry := NewPeerRegistry()
	requireError(t, registry.VerifyClaim(ctx, r), ErrIneligible)
	p := &ownedPeer{state: nodeplane.RuntimeEligibility{Proof: r.Proof, Snapshot: s, SafeIdle: true}}
	if e := registry.Register(r.Target, p); e != nil {
		t.Fatal(e)
	}
	for _, state := range []nodeplane.RuntimeEligibility{{Proof: r.Proof, Snapshot: s, SafeIdle: false}, {Proof: r.Proof, Snapshot: s, SafeIdle: true, Unknown: true}, {Proof: r.Proof, Snapshot: s, SafeIdle: true, Pending: true}, {Proof: nodeplane.RuntimeProof{NodeID: "foreign", Backend: api.NodeCodex, Epoch: "owned", Controllable: true}, Snapshot: s, SafeIdle: true}} {
		p.state = state
		requireError(t, registry.VerifyClaim(ctx, r), ErrIneligible)
	}
}

func TestBootstrapRequiresPairedQuiescedExactSnapshotSource(t *testing.T) {
	_, _, s := fixture(t)
	r := claim("source", s, "")
	registry := NewPeerRegistry()
	p := &ownedPeer{state: nodeplane.RuntimeEligibility{Proof: r.Proof, Snapshot: s, SafeIdle: true}}
	if e := registry.Register(r.Target, p); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e := registry.VerifyBootstrap(ctx, r.Target, s); e != nil {
		t.Fatal(e)
	}
	p.state.Unknown = true
	requireError(t, registry.VerifyBootstrap(ctx, r.Target, s), ErrIneligible)
	p.state.Unknown = false
	p.state.Snapshot.Version = "2"
	requireError(t, registry.VerifyBootstrap(ctx, r.Target, s), ErrIneligible)
	p.state.Snapshot = s
	p.state.LeaseEpoch = "1"
	requireError(t, registry.VerifyBootstrap(ctx, r.Target, s), ErrIneligible)
}
