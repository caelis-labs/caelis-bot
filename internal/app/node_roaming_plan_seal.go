package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// Only mutable lifecycle facts are excluded. Actual registrations, helper
// bytes/paths, source routes, enrollment identities, bindings and preferences
// are part of the exact reviewed deployment throughout dispatch and recovery.
func nativeRoamingPlanDigest(p roamingNativePlan) (string, error) {
	if p.SealVersion != 1 {
		return "", errors.New("original deployment has no supported immutable seal")
	}
	p.ID, p.Phase, p.RestoreDirectory, p.DisableOperationID = "", "", "", ""
	p.RestoredSnapshot = nodeplane.SnapshotRef{}
	p.DisableLease = nodeplane.Lease{}
	p.UnavailableCaelisWorkers = nil
	p.Nodes = append([]roamingNativeNode(nil), p.Nodes...)
	for i := range p.Nodes {
		p.Nodes[i].Plan.PlanID = ""
	}
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func validateNativeRoamingSeal(p roamingNativePlan) error {
	digest, err := nativeRoamingPlanDigest(p)
	if err != nil || digest != p.ID {
		return errors.New("original deployment immutable seal unavailable or changed; reconcile original operation")
	}
	if err := nodeagent.ValidateRoamingCoordinatorIdentity(nodeagent.RoamingManagedDeployment{BrokerNodeID: p.Coordinator.ID, CoordinatorIdentity: &p.CoordinatorIdentity}); err != nil {
		return err
	}
	ids := make([]string, 0, len(p.Enrollment))
	known := map[string]bool{}
	for _, reg := range p.Enrollment {
		if reg.ID == "" || known[reg.ID] {
			return errors.New("sealed enrollment identity changed")
		}
		known[reg.ID] = true
		ids = append(ids, reg.ID)
	}
	if !known[api.LocalNodeID] || !known[p.Coordinator.ID] || len(p.Nodes) != len(p.Enrollment) {
		return errors.New("sealed source/coordinator roster changed")
	}
	if err := nodeplane.ValidateCoordinatorSourceRoutes(p.Coordinator.ID, ids, p.SourceRoutes); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, node := range p.Nodes {
		id := node.Registration.ID
		if !known[id] || seen[id] || node.Plan.NodeID != id || node.Plan.OperationID != p.OperationID || node.Plan.PlanID != p.ID {
			return errors.New("sealed target enrollment changed")
		}
		seen[id] = true
		m := node.Plan.Managed
		if m == nil {
			continue
		}
		if m.NodeID != id || m.BrokerNodeID != p.Coordinator.ID || m.CoordinatorIdentity == nil || *m.CoordinatorIdentity != p.CoordinatorIdentity || m.JoinSSHDestination != m.BrokerSSHDestination {
			return errors.New("sealed coordinator route identity changed")
		}
		for _, route := range p.SourceRoutes {
			if route.SourceNodeID == id && route.SSHDestination != m.JoinSSHDestination {
				return errors.New("sealed explicit source route changed")
			}
		}
		if node.Registration.Join == api.NodeOutgoing {
			metadata := node.SourceMetadata
			if metadata == nil || metadata.NodeID != id || metadata.Directory != node.Registration.Directory || metadata.Helper != node.HostHelper || metadata.HelperSHA256 != node.Plan.HelperSHA256 || m.JoinSSHDestination != metadata.Route.Target || m.BrokerSSHDestination != metadata.Route.Target || m.JoinHelper != metadata.Route.Helper {
				return errors.New("sealed outgoing metadata pairing changed")
			}
		}
	}
	return nil
}
