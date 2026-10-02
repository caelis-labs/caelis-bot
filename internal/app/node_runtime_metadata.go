package app

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// Metadata belongs to the enrolled native node, including explicit readiness checks.
// It never replaces the resident engine or starts an automatic ownership loop.
type nodeRuntimeMetadata struct{ app *Application }

func (n *nodeRuntimeMetadata) peer(reg NodeRegistration) (nodeplane.CatalogAgent, error) {
	controller, e := backend.NativeNodeManagementController(n.app.Backend)
	if e != nil {
		return nil, e
	}
	management, ok := controller.(*nodeManagement)
	if !ok {
		return nil, errors.New("retained native node management unavailable")
	}
	native, ok := management.agent.(*nativeNodeManagement)
	if !ok {
		return nil, errors.New("exact target native pairing unavailable")
	}
	return native.agent(reg.ID)
}
func (n *nodeRuntimeMetadata) catalogNode(ctx context.Context, id string) (api.NodeInfo, error) {
	catalog, e := n.app.Backend.NodeCatalog(ctx)
	if e != nil {
		return api.NodeInfo{}, e
	}
	for _, info := range catalog.Nodes {
		if info.ID == id {
			return info, nil
		}
	}
	return api.NodeInfo{}, errors.New("exact enrolled Runtime unavailable")
}
func (n *nodeRuntimeMetadata) runtimeSettings(ctx context.Context, reg NodeRegistration, b api.NodeBackend) (api.RuntimeSettings, error) {
	peer, e := n.peer(reg)
	if e != nil {
		return api.RuntimeSettings{}, e
	}
	reader, ok := peer.(interface {
		ReadOwnedRuntimeSettings(context.Context, string, api.NodeBackend) (nodeagent.OwnedRuntimeSettings, error)
	})
	if !ok {
		return api.RuntimeSettings{}, errors.New("target Runtime metadata unavailable")
	}
	value, e := reader.ReadOwnedRuntimeSettings(ctx, reg.ID, b)
	if e != nil || value.Backend != b {
		return api.RuntimeSettings{}, errors.Join(errors.New("target Runtime metadata unconfirmed"), e)
	}
	return api.RuntimeSettings{Runtime: string(b), CLIPath: value.Binary, CaelisStore: value.Store}, nil
}
func (n *nodeRuntimeMetadata) ownedRuntimeProbe(ctx context.Context, reg NodeRegistration, b api.NodeBackend) (bool, string, error) {
	peer, e := n.peer(reg)
	if e != nil {
		return false, "native-pairing-unavailable", e
	}
	reader, ok := peer.(interface {
		ProbeOwnedRuntime(context.Context, string, api.NodeBackend) (nodeagent.OwnedRuntimeProbe, error)
	})
	if !ok {
		return false, "owned-runtime-probe-unavailable", errors.New("target native Runtime probe unavailable")
	}
	value, e := reader.ProbeOwnedRuntime(ctx, reg.ID, b)
	return value.Eligible, value.Reason, e
}
func (n *nodeRuntimeMetadata) ownedRuntimeReadiness(ctx context.Context, reg NodeRegistration, r nodeagent.OwnedRuntimeReadinessRequest) (nodeagent.OwnedRuntimeReadinessReceipt, error) {
	if r.NodeID != reg.ID {
		return nodeagent.OwnedRuntimeReadinessReceipt{}, errors.New("native readiness pairing changed")
	}
	peer, e := n.peer(reg)
	if e != nil {
		return nodeagent.OwnedRuntimeReadinessReceipt{}, e
	}
	reader, ok := peer.(interface {
		CheckOwnedRuntimeReadiness(context.Context, nodeagent.OwnedRuntimeReadinessRequest) (nodeagent.OwnedRuntimeReadinessReceipt, error)
	})
	if !ok {
		return nodeagent.OwnedRuntimeReadinessReceipt{}, errors.New("target native readiness unavailable")
	}
	return reader.CheckOwnedRuntimeReadiness(ctx, r)
}

func (n *nodeRuntimeMetadata) targetPreferences(ctx context.Context, reg NodeRegistration, b api.NodeBackend) (nodeagent.ExecutionPreferences, error) {
	p := nodeagent.ExecutionPreferences{Schema: 1, Revision: 1}
	configuration, e := n.app.Backend.NodeRuntimeConfiguration(ctx, reg.ID, b)
	if e != nil {
		return p, e
	}
	if !configuration.ConfigurationAvailable && b != api.NodeCaelis {
		return p, errors.New("target Runtime preferences cannot be frozen for review")
	}
	if configuration.Conversation != nil {
		p.Conversation = *configuration.Conversation
	} else if b == api.NodeCaelis && configuration.ConfigurationAvailable {
		p.Conversation = configuration.Configuration.Main
	}
	if configuration.Worker != nil {
		p.Worker = *configuration.Worker
	} else if b == api.NodeCaelis && configuration.ConfigurationAvailable {
		p.Worker = configuration.Configuration.Main
	}
	// With no live Core configuration, the empty selector means this target's
	// native current model. Never substitute this APP/source's model or effort.
	return p, nil
}
