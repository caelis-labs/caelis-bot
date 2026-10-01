package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// A marked cold Caelis Store can be reviewed as a provisional candidate. This
// does not grant a Runtime role: authentication and the actual target-current
// model are checked by an approved bounded owned Host before source retirement.
func (n *roamingNativeAssembly) candidateBackend(ctx context.Context, reg NodeRegistration, preferred string) (string, error) {
	info, e := n.catalogNode(ctx, reg.ID)
	if e != nil {
		return "", e
	}
	codexReady := false
	for _, runtime := range info.Runtimes {
		codexReady = codexReady || runtime.Backend == api.NodeCodex && runtime.Authentication == api.NodeAuthenticated && runtime.Health == api.NodeHealthy
	}
	if preferred == "codex" && codexReady {
		return "codex", nil
	}
	if n.options.RuntimeSettings != nil && n.options.OwnedRuntimeProbe != nil {
		settings, e := n.options.RuntimeSettings(ctx, reg, api.NodeCaelis)
		if e == nil && settings.Runtime == "caelis" && filepath.IsAbs(settings.CLIPath) && filepath.IsAbs(settings.CaelisStore) {
			eligible, reason, e := n.options.OwnedRuntimeProbe(ctx, reg, api.NodeCaelis)
			if e == nil && eligible {
				return "caelis", nil
			}
			if !codexReady {
				return "", errors.Join(fmt.Errorf("node %s designated Caelis Host cannot be owned: %s", reg.Label, reason), e)
			}
		}
	}
	if codexReady {
		return "codex", nil
	}
	return "", fmt.Errorf("node %s needs an authenticated Codex Runtime or a marked private Caelis Store", reg.Label)
}

func (n *roamingNativeAssembly) targetPreferences(ctx context.Context, reg NodeRegistration, b api.NodeBackend) (nodeagent.ExecutionPreferences, error) {
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

func (n *roamingNativeAssembly) ownedRuntimeReadiness(ctx context.Context, reg NodeRegistration, request nodeagent.OwnedRuntimeReadinessRequest) (nodeagent.OwnedRuntimeReadinessReceipt, error) {
	if reg.ID != request.NodeID {
		return nodeagent.OwnedRuntimeReadinessReceipt{}, errors.New("native readiness pairing changed")
	}
	var peer nodeplane.CatalogAgent
	if reg.ID == api.LocalNodeID {
		dir := filepath.Join(n.app.root, "nodeplane", "local")
		if e := nativeRoamingPrivateDir(dir); e != nil {
			return nodeagent.OwnedRuntimeReadinessReceipt{}, e
		}
		local, e := nodeagent.New(nodeagent.Options{Directory: dir, NodeID: reg.ID, OwnedRuntimeSettings: func(ctx context.Context, b api.NodeBackend) (nodeagent.OwnedRuntimeSettings, error) {
			settings, e := n.options.RuntimeSettings(ctx, reg, b)
			return nodeagent.OwnedRuntimeSettings{Backend: b, Binary: settings.CLIPath, Store: settings.CaelisStore}, e
		}, OwnedRuntimeCompanion: func(context.Context) (nodeagent.OwnedRuntimeCompanion, error) {
			path, e := n.options.Host()
			if e != nil {
				return nodeagent.OwnedRuntimeCompanion{}, e
			}
			digest, e := nativeRoamingDigest(path)
			return nodeagent.OwnedRuntimeCompanion{Path: path, SHA256: digest}, e
		}})
		if e != nil {
			return nodeagent.OwnedRuntimeReadinessReceipt{}, e
		}
		peer = local
	} else {
		controller, e := backend.NativeNodeManagementController(n.app.Backend)
		if e != nil {
			return nodeagent.OwnedRuntimeReadinessReceipt{}, e
		}
		management, ok := controller.(*nodeManagement)
		if !ok {
			return nodeagent.OwnedRuntimeReadinessReceipt{}, errors.New("retained native management is unavailable")
		}
		native, ok := management.agent.(*nativeNodeManagement)
		if !ok {
			return nodeagent.OwnedRuntimeReadinessReceipt{}, errors.New("exact native target pairing is unavailable")
		}
		peer, e = native.agent(reg.ID)
		if e != nil {
			return nodeagent.OwnedRuntimeReadinessReceipt{}, e
		}
	}
	checker, ok := peer.(interface {
		CheckOwnedRuntimeReadiness(context.Context, nodeagent.OwnedRuntimeReadinessRequest) (nodeagent.OwnedRuntimeReadinessReceipt, error)
	})
	if !ok {
		return nodeagent.OwnedRuntimeReadinessReceipt{}, errors.New("paired native readiness action is unavailable")
	}
	return checker.CheckOwnedRuntimeReadiness(ctx, request)
}

func (n *roamingNativeAssembly) confirmOwnedReadiness(ctx context.Context, p *roamingNativePlan) error {
	for _, node := range p.Nodes {
		if node.Plan.Managed == nil {
			continue
		}
		primary := node.Plan.Managed.Backend == "caelis"
		var binding *NodeRoamingWorkerRuntime
		for i := range node.RuntimeBindings {
			if node.RuntimeBindings[i].Backend == "caelis" {
				binding = &node.RuntimeBindings[i]
			}
		}
		if !primary && binding == nil {
			continue
		}
		if node.CompanionArtifact != nil {
			if e := n.stageOutgoingCompanion(ctx, node); e != nil {
				return e
			}
		}
		m := node.Plan.Managed
		request := nodeagent.OwnedRuntimeReadinessRequest{NodeID: node.Registration.ID, Backend: api.NodeCaelis, OperationID: p.OperationID, ExpectedBinary: m.CaelisBinary, ExpectedStore: m.CaelisStore, Model: m.Model}
		if !primary {
			request.ExpectedBinary, request.ExpectedStore, request.Model = binding.Binary, binding.Store, binding.Model
		}
		if n.options.OwnedRuntimeReadiness == nil {
			return errors.New("approved target native readiness action is unavailable")
		}
		receipt, e := n.options.OwnedRuntimeReadiness(ctx, node.Registration, request)
		if e != nil || receipt.NodeID != request.NodeID || receipt.Backend != request.Backend || receipt.OperationID != request.OperationID || (receipt.Outcome != "ready" && receipt.Outcome != "unavailable") || !receipt.StopConfirmed {
			return errors.Join(errors.New("original native Caelis readiness or owned Host stop is unconfirmed"), e)
		}
		modelReady := func(model string) bool {
			if model == "" {
				model = receipt.Model
			}
			if model == "" {
				return false
			}
			for _, authenticated := range receipt.AuthenticatedModels {
				if authenticated == model {
					return true
				}
			}
			return false
		}
		ready := receipt.Outcome == "ready" && receipt.Ready && modelReady(request.Model)
		if primary && !ready {
			return fmt.Errorf("node %s target-current Caelis model is unavailable: %s", node.Registration.Label, receipt.Reason)
		}
		if binding != nil && (!ready || !modelReady(binding.Model)) {
			if p.UnavailableCaelisWorkers == nil {
				p.UnavailableCaelisWorkers = map[string]bool{}
			}
			p.UnavailableCaelisWorkers[node.Registration.ID] = true
		}
	}
	return nil
}
