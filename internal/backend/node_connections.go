package backend

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (s *Service) nodeRuntimeConnectionController() (api.NodeRuntimeConnectionController, error) {
	management, e := NativeNodeManagementController(s)
	if e != nil {
		return nil, e
	}
	c, ok := management.(api.NodeRuntimeConnectionController)
	if !ok {
		return nil, errors.New("paired node connection setup is unavailable")
	}
	return c, nil
}

func (s *Service) BeginNodeRuntimeConnection(ctx context.Context, guard api.NodeEditGuard, operationID string) (api.NodeRuntimeConnectionRef, error) {
	c, e := s.nodeRuntimeConnectionController()
	if e != nil {
		return api.NodeRuntimeConnectionRef{}, e
	}
	return c.BeginNodeRuntimeConnection(ctx, guard, operationID)
}

func (s *Service) NodeRuntimeConnectionCatalog(ctx context.Context, ref api.NodeRuntimeConnectionRef, kind string) (api.RuntimeConnectionCatalog, error) {
	c, e := s.nodeRuntimeConnectionController()
	if e != nil {
		return api.RuntimeConnectionCatalog{}, e
	}
	return c.NodeRuntimeConnectionCatalog(ctx, ref, kind)
}

func (s *Service) NodeRuntimeSetupCatalog(ctx context.Context, ref api.NodeRuntimeConnectionRef, action, provider, baseURL string) ([]api.SetupChoice, error) {
	c, e := s.nodeRuntimeConnectionController()
	if e != nil {
		return nil, e
	}
	return c.NodeRuntimeSetupCatalog(ctx, ref, action, provider, baseURL)
}

func (s *Service) StartNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, input api.RuntimeConnectionInput) (api.RuntimeFlow, error) {
	c, e := s.nodeRuntimeConnectionController()
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.StartNodeRuntimeConnection(ctx, ref, input)
}

func (s *Service) AdvanceNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, action api.RuntimeFlowAction) (api.RuntimeFlow, error) {
	c, e := s.nodeRuntimeConnectionController()
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.AdvanceNodeRuntimeConnection(ctx, ref, action)
}

func (s *Service) WaitNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, flowID string, after int) (api.RuntimeFlow, error) {
	c, e := s.nodeRuntimeConnectionController()
	if e != nil {
		return api.RuntimeFlow{}, e
	}
	return c.WaitNodeRuntimeConnection(ctx, ref, flowID, after)
}

func (s *Service) CancelNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef, flowID string) error {
	c, e := s.nodeRuntimeConnectionController()
	if e != nil {
		return e
	}
	return c.CancelNodeRuntimeConnection(ctx, ref, flowID)
}

func (s *Service) CloseNodeRuntimeConnection(ctx context.Context, ref api.NodeRuntimeConnectionRef) error {
	c, e := s.nodeRuntimeConnectionController()
	if e != nil {
		return e
	}
	return c.CloseNodeRuntimeConnection(ctx, ref)
}
