package app

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func validNodeConnectionRef(ref api.NodeRuntimeConnectionRef) bool {
	return ref.Backend == api.NodeCaelis && productIdentifier.MatchString(ref.NodeID) && productIdentifier.MatchString(ref.OperationID)
}

func (m *nodeManagement) nodeConnectionController() (api.NodeRuntimeConnectionController, error) {
	c, ok := m.agent.(api.NodeRuntimeConnectionController)
	if !ok {
		return nil, errors.New("native node connection setup is unavailable")
	}
	return c, nil
}

func (n *nativeNodeManagement) nodeConnectionTarget(nodeID string) (api.NodeRuntimeConnectionController, error) {
	peer, e := n.agent(nodeID)
	if e != nil {
		return nil, e
	}
	c, ok := peer.(api.NodeRuntimeConnectionController)
	if !ok {
		return nil, errors.New("paired native node connection setup is unavailable")
	}
	return c, nil
}

func (m *nodeManagement) BeginNodeRuntimeConnection(ctx context.Context, guard api.NodeEditGuard, operationID string) (api.NodeRuntimeConnectionRef, error) {
	ref := api.NodeRuntimeConnectionRef{NodeID: guard.NodeID, Backend: guard.Backend, OperationID: operationID}
	if !validNodeConnectionRef(ref) || guard.Revision == "" {
		return api.NodeRuntimeConnectionRef{}, errors.New("exact original Caelis connection scope required")
	}
	// Only the exact target admits a new guard. Original retries must reach its
	// journal before comparing a revision changed by the first setup Begin.
	c, e := m.nodeConnectionController()
	if e != nil {
		return api.NodeRuntimeConnectionRef{}, e
	}
	got, e := c.BeginNodeRuntimeConnection(ctx, guard, operationID)
	if e != nil {
		return api.NodeRuntimeConnectionRef{}, e
	}
	if got != ref {
		return api.NodeRuntimeConnectionRef{}, errors.New("native connection changed original target")
	}
	return got, nil
}

func (m *nodeManagement) NodeRuntimeConnectionCatalog(ctx context.Context, ref api.NodeRuntimeConnectionRef, kind string) (api.RuntimeConnectionCatalog, error) {
	if !validNodeConnectionRef(ref) {
		return api.RuntimeConnectionCatalog{}, errors.New("exact original Caelis connection scope required")
	}
	c, e := m.nodeConnectionController()
	if e != nil {
		return api.RuntimeConnectionCatalog{}, e
	}
	return c.NodeRuntimeConnectionCatalog(ctx, ref, kind)
}

func (m *nodeManagement) NodeRuntimeSetupCatalog(ctx context.Context, ref api.NodeRuntimeConnectionRef, action, provider, baseURL string) ([]api.SetupChoice, error) {
	if !validNodeConnectionRef(ref) {
		return nil, errors.New("exact original Caelis connection scope required")
	}
	c, e := m.nodeConnectionController()
	if e != nil {
		return nil, e
	}
	return c.NodeRuntimeSetupCatalog(ctx, ref, action, provider, baseURL)
}

func (m *nodeManagement) StartNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, input api.RuntimeConnectionInput) (api.RuntimeFlow, error) {
	if !validNodeConnectionRef(ref) {
		return api.RuntimeFlow{}, errors.New("exact original Caelis connection scope required")
	}
	if input.Settings != nil {
		return api.RuntimeFlow{}, errors.New("node setup accepts no Runtime paths or settings override")
	}
	c, e := m.nodeConnectionController()
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.StartNodeRuntimeConnection(ctx, ref, input)
}

func (m *nodeManagement) AdvanceNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, action api.RuntimeFlowAction) (api.RuntimeFlow, error) {
	if !validNodeConnectionRef(ref) {
		return api.RuntimeFlow{}, errors.New("exact original Caelis connection scope required")
	}
	c, e := m.nodeConnectionController()
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.AdvanceNodeRuntimeConnection(ctx, ref, action)
}

func (m *nodeManagement) WaitNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, flowID string, after int) (api.RuntimeFlow, error) {
	if !validNodeConnectionRef(ref) {
		return api.RuntimeFlow{}, errors.New("exact original Caelis connection scope required")
	}
	c, e := m.nodeConnectionController()
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.WaitNodeRuntimeConnection(ctx, ref, flowID, after)
}

func (m *nodeManagement) CancelNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, flowID string) error {
	if !validNodeConnectionRef(ref) {
		return errors.New("exact original Caelis connection scope required")
	}
	c, e := m.nodeConnectionController()
	if e != nil {
		return e
	}
	return c.CancelNodeRuntimeConnection(ctx, ref, flowID)
}

func (m *nodeManagement) CloseNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef) error {
	if !validNodeConnectionRef(ref) {
		return errors.New("exact original Caelis connection scope required")
	}
	c, e := m.nodeConnectionController()
	if e != nil {
		return e
	}
	return c.CloseNodeRuntimeConnection(ctx, ref)
}

func (n *nativeNodeManagement) BeginNodeRuntimeConnection(ctx context.Context, guard api.NodeEditGuard, operationID string) (api.NodeRuntimeConnectionRef, error) {
	ref := api.NodeRuntimeConnectionRef{NodeID: guard.NodeID, Backend: guard.Backend, OperationID: operationID}
	if !validNodeConnectionRef(ref) || guard.Revision == "" {
		return api.NodeRuntimeConnectionRef{}, errors.New("exact original Caelis connection scope required")
	}
	c, e := n.nodeConnectionTarget(guard.NodeID)
	if e != nil {
		return api.NodeRuntimeConnectionRef{}, e
	}
	got, e := c.BeginNodeRuntimeConnection(ctx, guard, operationID)
	if e != nil {
		return api.NodeRuntimeConnectionRef{}, e
	}
	if got != ref {
		return api.NodeRuntimeConnectionRef{}, errors.New("native connection changed original target")
	}
	return got, nil
}

func (n *nativeNodeManagement) NodeRuntimeConnectionCatalog(ctx context.Context, ref api.NodeRuntimeConnectionRef, kind string) (api.RuntimeConnectionCatalog, error) {
	if !validNodeConnectionRef(ref) {
		return api.RuntimeConnectionCatalog{}, errors.New("exact original Caelis connection scope required")
	}
	c, e := n.nodeConnectionTarget(ref.NodeID)
	if e != nil {
		return api.RuntimeConnectionCatalog{}, e
	}
	return c.NodeRuntimeConnectionCatalog(ctx, ref, kind)
}

func (n *nativeNodeManagement) NodeRuntimeSetupCatalog(ctx context.Context, ref api.NodeRuntimeConnectionRef, action, provider, baseURL string) ([]api.SetupChoice, error) {
	if !validNodeConnectionRef(ref) {
		return nil, errors.New("exact original Caelis connection scope required")
	}
	c, e := n.nodeConnectionTarget(ref.NodeID)
	if e != nil {
		return nil, e
	}
	return c.NodeRuntimeSetupCatalog(ctx, ref, action, provider, baseURL)
}

func (n *nativeNodeManagement) StartNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, input api.RuntimeConnectionInput) (api.RuntimeFlow, error) {
	if !validNodeConnectionRef(ref) {
		return api.RuntimeFlow{}, errors.New("exact original Caelis connection scope required")
	}
	if input.Settings != nil {
		return api.RuntimeFlow{}, errors.New("node setup accepts no Runtime paths or settings override")
	}
	c, e := n.nodeConnectionTarget(ref.NodeID)
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.StartNodeRuntimeConnection(ctx, ref, input)
}

func (n *nativeNodeManagement) AdvanceNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, action api.RuntimeFlowAction) (api.RuntimeFlow, error) {
	if !validNodeConnectionRef(ref) {
		return api.RuntimeFlow{}, errors.New("exact original Caelis connection scope required")
	}
	c, e := n.nodeConnectionTarget(ref.NodeID)
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.AdvanceNodeRuntimeConnection(ctx, ref, action)
}

func (n *nativeNodeManagement) WaitNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, flowID string, after int) (api.RuntimeFlow, error) {
	if !validNodeConnectionRef(ref) {
		return api.RuntimeFlow{}, errors.New("exact original Caelis connection scope required")
	}
	c, e := n.nodeConnectionTarget(ref.NodeID)
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.WaitNodeRuntimeConnection(ctx, ref, flowID, after)
}

func (n *nativeNodeManagement) CancelNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, flowID string) error {
	if !validNodeConnectionRef(ref) {
		return errors.New("exact original Caelis connection scope required")
	}
	c, e := n.nodeConnectionTarget(ref.NodeID)
	if e != nil {
		return e
	}
	return c.CancelNodeRuntimeConnection(ctx, ref, flowID)
}

func (n *nativeNodeManagement) CloseNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef) error {
	if !validNodeConnectionRef(ref) {
		return errors.New("exact original Caelis connection scope required")
	}
	c, e := n.nodeConnectionTarget(ref.NodeID)
	if e != nil {
		return e
	}
	return c.CloseNodeRuntimeConnection(ctx, ref)
}
