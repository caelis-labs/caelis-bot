package backend

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// SetNodeManagementController injects only the user settings facade. Native
// assembly supplies its trusted agent; this port is never installed as a tool.
func (s *Service) SetNodeManagementController(c api.NodeManagementController) {
	s.mu.Lock()
	s.nodeManagement = c
	s.mu.Unlock()
}

func (s *Service) nodeManagementController() (api.NodeManagementController, error) {
	s.mu.Lock()
	c := s.nodeManagement
	s.mu.Unlock()
	if c == nil {
		return nil, errors.New("node management is unavailable")
	}
	return c, nil
}

func (s *Service) NodeCatalog(ctx context.Context) (api.NodeCatalog, error) {
	c, err := s.nodeManagementController()
	if err != nil {
		return api.NodeCatalog{}, err
	}
	return c.NodeCatalog(ctx)
}

func (s *Service) NodeRuntimeConfiguration(ctx context.Context, nodeID string, backend api.NodeBackend) (api.NodeRuntimeConfiguration, error) {
	c, err := s.nodeManagementController()
	if err != nil {
		return api.NodeRuntimeConfiguration{}, err
	}
	return c.NodeRuntimeConfiguration(ctx, nodeID, backend)
}

func (s *Service) SelectNode(ctx context.Context, nodeID, revision string) (api.NodeCatalog, error) {
	c, err := s.nodeManagementController()
	if err != nil {
		return api.NodeCatalog{}, err
	}
	return c.SelectNode(ctx, nodeID, revision)
}

func (s *Service) ChangeNodeConfiguration(ctx context.Context, r api.NodeManagementRequest) (api.NodeOperationReceipt, error) {
	c, err := s.nodeManagementController()
	if err != nil {
		return api.NodeOperationReceipt{}, err
	}
	return c.ChangeNodeConfiguration(ctx, r)
}

func (s *Service) ReconcileNodeOperation(ctx context.Context, ref api.NodeOperationRef) (api.NodeOperationReceipt, error) {
	c, err := s.nodeManagementController()
	if err != nil {
		return api.NodeOperationReceipt{}, err
	}
	return c.ReconcileNodeOperation(ctx, ref)
}

func (s *Service) AddNode(ctx context.Context, r api.NodeAddRequest) (api.NodeAddResult, error) {
	c, err := s.nodeManagementController()
	if err != nil {
		return api.NodeAddResult{}, err
	}
	return c.AddNode(ctx, r)
}

func (s *Service) DetectNode(ctx context.Context, nodeID string) (api.NodeInfo, error) {
	c, err := s.nodeManagementController()
	if err != nil {
		return api.NodeInfo{}, err
	}
	return c.DetectNode(ctx, nodeID)
}

func (s *Service) NodeJoinInstructions(ctx context.Context, nodeID string) (api.NodeJoinInstructions, error) {
	c, err := s.nodeManagementController()
	if err != nil {
		return api.NodeJoinInstructions{}, err
	}
	return c.NodeJoinInstructions(ctx, nodeID)
}

func (s *Service) SetNodeCoordinator(ctx context.Context, r api.NodeCoordinatorSelection) (api.NodeCatalog, error) {
	c, err := s.nodeManagementController()
	if err != nil {
		return api.NodeCatalog{}, err
	}
	return c.SetNodeCoordinator(ctx, r)
}
