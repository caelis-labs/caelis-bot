package nodeagent

import (
	"context"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// ManagedNodeConnectionPort routes setup to the original independently owned
// child. Only that child journals the user action or owns its SDK observer.
type ManagedNodeConnectionPort interface {
	ManagedNodeConnections(context.Context, string, api.NodeBackend) (api.NodeRuntimeConnectionController, bool, error)
}

func (m *ManagedStarter) ManagedNodeConnections(ctx context.Context, node string, b api.NodeBackend) (api.NodeRuntimeConnectionController, bool, error) {
	if !m.hasManagedChild() {
		return nil, false, nil
	}
	if node != m.config.NodeID || b != api.NodeCaelis {
		return nil, true, errors.New("managed node connection scope changed")
	}
	client, err := m.child(ctx)
	return client, true, err
}
func (s *Service) managedNodeConnections(ctx context.Context, node string, b api.NodeBackend) (api.NodeRuntimeConnectionController, bool, error) {
	if port, ok := s.options.ManagedStart.(ManagedNodeConnectionPort); ok {
		return port.ManagedNodeConnections(ctx, node, b)
	}
	return nil, false, nil
}

func closeManagedNodeConnectionController(controller api.NodeRuntimeConnectionController) {
	if client, ok := controller.(*Client); ok {
		_ = client.Close()
	}
}
