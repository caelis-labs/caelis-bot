package nodecoord

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"sync"
)

// PeerRegistry contains only the user's explicitly paired exact node/backend
// connections. ReadRuntimeProof is an authenticated same-user private IPC read
// from the native owner; neither request body nor catalog display is authority.
type PeerRegistry struct {
	mu    sync.RWMutex
	peers map[api.WorkTarget]nodeplane.RuntimeProofPort
}

func NewPeerRegistry() *PeerRegistry {
	return &PeerRegistry{peers: make(map[api.WorkTarget]nodeplane.RuntimeProofPort)}
}
func (r *PeerRegistry) Register(target api.WorkTarget, peer nodeplane.RuntimeProofPort) error {
	if target.Validate() != nil || target.Role != api.RoleBot || (target.Backend != "codex" && target.Backend != "caelis") || peer == nil {
		return ErrIneligible
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.peers[target]; exists {
		return errors.New("runtime peer already paired")
	}
	r.peers[target] = peer
	return nil
}
func (r *PeerRegistry) read(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	r.mu.RLock()
	peer := r.peers[target]
	r.mu.RUnlock()
	if peer == nil {
		return nodeplane.RuntimeEligibility{}, ErrIneligible
	}
	proof, err := peer.ReadRuntimeProof(ctx, target)
	if err != nil || !proof.Proof.Controllable || proof.Proof.NodeID != target.NodeID || string(proof.Proof.Backend) != target.Backend || proof.Proof.Epoch == "" {
		return nodeplane.RuntimeEligibility{}, ErrIneligible
	}
	return proof, ctx.Err()
}
func (r *PeerRegistry) VerifyClaim(ctx context.Context, claim nodeplane.ClaimRequest) error {
	p, err := r.read(ctx, claim.Target)
	if err != nil {
		return err
	}
	if p.Proof != claim.Proof || p.Snapshot != claim.Snapshot || !p.SafeIdle || p.Pending || p.Unknown || p.LeaseEpoch != "" {
		return ErrIneligible
	}
	return nil
}
func (r *PeerRegistry) VerifyRenew(ctx context.Context, l nodeplane.Lease, claim nodeplane.ClaimRequest) error {
	p, err := r.read(ctx, claim.Target)
	if err != nil {
		return err
	}
	if p.Snapshot != claim.Snapshot || p.LeaseEpoch != l.Epoch || p.Unknown || p.Proof.NodeID != l.NodeID || p.Proof.Backend != l.Backend {
		return ErrIneligible
	}
	return nil
}

// VerifyBootstrap requires an explicitly selected, paired native source to
// attest the exact exported complete snapshot while admission and all writers
// are quiesced. A private payload file alone is not evidence of quiescence.
func (r *PeerRegistry) VerifyBootstrap(ctx context.Context, target api.WorkTarget, ref nodeplane.SnapshotRef) error {
	if ref.Epoch != "0" {
		return ErrSnapshot
	}
	p, err := r.read(ctx, target)
	if err != nil {
		return err
	}
	if p.Snapshot != ref || !p.SafeIdle || p.Pending || p.Unknown || p.LeaseEpoch != "" {
		return ErrIneligible
	}
	return nil
}
