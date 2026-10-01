package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
)

func (n *roamingNativeAssembly) coordinatorEnrollmentIdentity(reg NodeRegistration) (nodeagent.NativeEnrollmentIdentity, error) {
	if reg.ID != api.LocalNodeID {
		if reg.Join != api.NodeSSH || validateNodeRegistration(reg) != nil {
			return nodeagent.NativeEnrollmentIdentity{}, errors.New("enrolled SSH coordinator identity required")
		}
		return nodeagent.NativeEnrollmentIdentity{NodeID: reg.ID, Directory: reg.Directory}, nil
	}
	if n.original == nil {
		return nodeagent.NativeEnrollmentIdentity{}, errors.New("actual local coordinator enrollment unavailable")
	}
	native, ok := n.original.agent.(*nativeNodeManagement)
	if !ok {
		return nodeagent.NativeEnrollmentIdentity{}, errors.New("actual local coordinator pairing unavailable")
	}
	native.mu.Lock()
	local := native.local
	native.mu.Unlock()
	port, ok := local.(interface {
		NativeEnrollmentIdentity() (nodeagent.NativeEnrollmentIdentity, error)
	})
	if !ok {
		return nodeagent.NativeEnrollmentIdentity{}, errors.New("actual local coordinator enrollment identity unavailable")
	}
	identity, err := port.NativeEnrollmentIdentity()
	if err != nil {
		return identity, err
	}
	// The port does not create/adopt an enrollment. Validate its actual file
	// again; never replace this underlying ID with the public local alias.
	return nodeagent.ReadNativeEnrollmentIdentity(identity.Directory, identity.NodeID)
}

func nativeRoamingBootstrapRoute(p roamingNativePlan) (string, string, error) {
	if p.Coordinator.ID == api.LocalNodeID {
		return "", "", nil
	}
	for _, node := range p.Nodes {
		m := node.Plan.Managed
		if node.Registration.ID == api.LocalNodeID && m != nil && m.JoinSSHDestination != "" && m.JoinHelper != "" && m.BrokerSSHDestination == m.JoinSSHDestination && m.CoordinatorIdentity != nil && *m.CoordinatorIdentity == p.CoordinatorIdentity {
			if _, err := strictRoamingSSH(m.JoinSSHDestination); err != nil {
				return "", "", err
			}
			return m.JoinSSHDestination, m.JoinHelper, nil
		}
	}
	return "", "", errors.New("original local-source coordinator route unavailable")
}

// This bounded read-only verification precedes published review and source
// retirement. Every command is formed from frozen enrolled native identities.
func (n *roamingNativeAssembly) verifyFrozenSourceRoutes(ctx context.Context, p roamingNativePlan) error {
	if err := validateNativeRoamingSeal(p); err != nil {
		return err
	}
	if p.Coordinator.ID == api.LocalNodeID {
		if _, err := nodeagent.ReadNativeEnrollmentIdentity(p.CoordinatorIdentity.Directory, p.CoordinatorIdentity.NodeID); err != nil {
			return err
		}
	}
	for _, node := range p.Nodes {
		if node.Registration.ID == p.Coordinator.ID || node.Plan.Managed == nil {
			continue
		}
		m := node.Plan.Managed
		if m.CoordinatorIdentity == nil || *m.CoordinatorIdentity != p.CoordinatorIdentity || m.JoinSSHDestination != m.BrokerSSHDestination {
			return errors.New("frozen source coordinator pairing changed")
		}
		if node.Registration.Join == api.NodeOutgoing {
			if err := n.prepareOutgoing(ctx, p, node, "preflight"); err != nil {
				return err
			}
			continue
		}
		verification, err := nodeagent.RoamingCoordinatorVerificationCommand(*m)
		if err != nil {
			return err
		}
		if node.Registration.ID == api.LocalNodeID {
			if err := runRoamingSSH(ctx, m.JoinSSHDestination, verification, nil); err != nil {
				return errors.New("this machine's exact coordinator authorization unavailable")
			}
			continue
		}
		args, err := strictRoamingSSH(m.JoinSSHDestination)
		if err != nil {
			return err
		}
		quoted := []string{"ssh"}
		for _, arg := range args {
			quoted = append(quoted, nodeShellQuote(arg))
		}
		quoted = append(quoted, nodeShellQuote(verification))
		sourceVerification := nodeShellQuote(node.Registration.HelperPath) + " verify-join-directory --directory " + nodeShellQuote(node.Registration.Directory) + " --node-id " + nodeShellQuote(node.Registration.ID)
		if err := runRoamingSSH(ctx, node.Registration.SSHDestination, sourceVerification+" && "+strings.Join(quoted, " "), nil); err != nil {
			return fmt.Errorf("node %s exact source/coordinator authorization unavailable", node.Registration.Label)
		}
	}
	return nil
}
