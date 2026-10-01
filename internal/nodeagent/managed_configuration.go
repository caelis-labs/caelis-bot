package nodeagent

import (
	"context"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// ManagedConfigurationPort forwards configuration to the exact independently
// owned child. That child owns its original-ID journal; the catalog agent must
// not prewrite a second intent into the same target-private receipt directory.
type ManagedConfigurationPort interface {
	ReadManagedConfiguration(context.Context, string, api.NodeBackend) (api.NodeRuntimeConfiguration, bool, error)
	ChangeManagedConfiguration(context.Context, nodeplane.ManagementRequest) (api.NodeOperationReceipt, bool, error)
}

func (m *ManagedStarter) hasManagedChild() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.command != nil
}
func (m *ManagedStarter) ReadManagedConfiguration(ctx context.Context, node string, backend api.NodeBackend) (api.NodeRuntimeConfiguration, bool, error) {
	if !m.hasManagedChild() {
		return api.NodeRuntimeConfiguration{}, false, nil
	}
	client, err := m.child(ctx)
	if err != nil {
		return api.NodeRuntimeConfiguration{}, true, err
	}
	defer client.Close()
	configuration, err := client.Configuration(ctx, node, backend)
	return configuration, true, err
}
func (m *ManagedStarter) ChangeManagedConfiguration(ctx context.Context, request nodeplane.ManagementRequest) (api.NodeOperationReceipt, bool, error) {
	if !m.hasManagedChild() {
		return api.NodeOperationReceipt{}, false, nil
	}
	client, err := m.child(ctx)
	if err != nil {
		return api.NodeOperationReceipt{Ref: request.Ref, Outcome: api.NodeUnknown, Message: "original-operation-unresolved"}, true, err
	}
	defer client.Close()
	receipt, err := client.Manage(ctx, request)
	return receipt, true, err
}
