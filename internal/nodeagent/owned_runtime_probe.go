package nodeagent

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
)

// OwnedRuntimeProbe is native-only preflight evidence. It does not establish
// authentication, healthy Runtime state, or a leased owner generation.
type OwnedRuntimeProbe struct {
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason"`
}

type ownedRuntimeProbePort interface {
	ProbeOwnedRuntime(context.Context, string, api.NodeBackend) (OwnedRuntimeProbe, error)
}

func validOwnedRuntimeProbe(probe OwnedRuntimeProbe) bool {
	if probe.Eligible {
		return probe.Reason == ""
	}
	switch probe.Reason {
	case "unsupported-platform", "runtime-metadata-unavailable", "shared-default-store", "owned-store-path-invalid", "owned-store-setup-required", "owned-store-not-private", "owned-store-marker-unavailable", "owned-store-node-mismatch", "owned-store-controller-busy", "owned-store-discovery-present", "owned-store-state-unavailable", "owned-store-native-home-invalid":
		return true
	}
	return false
}

func (s *Service) ProbeOwnedRuntime(ctx context.Context, nodeID string, b api.NodeBackend) (OwnedRuntimeProbe, error) {
	if err := ctx.Err(); err != nil {
		return OwnedRuntimeProbe{}, err
	}
	if nodeID != s.options.NodeID || !backend(b) {
		return OwnedRuntimeProbe{}, errors.New("native owned runtime scope changed")
	}
	if !codex.OwnedRuntimeSupported() {
		return OwnedRuntimeProbe{Reason: "unsupported-platform"}, nil
	}
	metadata, err := s.ReadOwnedRuntimeSettings(ctx, nodeID, b)
	if err != nil {
		if err := ctx.Err(); err != nil {
			return OwnedRuntimeProbe{}, err
		}
		return OwnedRuntimeProbe{Reason: "runtime-metadata-unavailable"}, nil
	}
	if b == api.NodeCodex {
		return OwnedRuntimeProbe{Eligible: true}, nil
	}
	eligible, reason := caelis.ProbeOwnedStore(nodeID, metadata.Store)
	return OwnedRuntimeProbe{Eligible: eligible, Reason: reason}, nil
}

func (c *Client) ProbeOwnedRuntime(ctx context.Context, nodeID string, b api.NodeBackend) (OwnedRuntimeProbe, error) {
	if nodeID != c.expected || !backend(b) {
		return OwnedRuntimeProbe{}, errors.New("native owned runtime scope changed")
	}
	var out OwnedRuntimeProbe
	if err := c.request(ctx, "POST", "/v1/node/probe-owned-runtime", ownedRuntimeSettingsRequest{NodeID: nodeID, Backend: b}, &out); err != nil {
		return OwnedRuntimeProbe{}, err
	}
	if !validOwnedRuntimeProbe(out) {
		return OwnedRuntimeProbe{}, errors.New("invalid native ownability evidence")
	}
	return out, nil
}
