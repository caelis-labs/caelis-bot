package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
)

func (n *roamingNativeAssembly) outgoingPort(ctx context.Context, id string) (nodeagent.RoamingDeploymentPort, func(), error) {
	if n.original == nil {
		return nil, nil, errors.New("existing paired outgoing agent unavailable")
	}
	native, ok := n.original.agent.(*nativeNodeManagement)
	if !ok {
		return nil, nil, errors.New("existing native pairing unavailable")
	}
	// Reopen only the persisted paired observation route. Stage deliberately
	// closes the original management observer; that never removes pairing or
	// grants authority to stop the independent serving agent/supervisor.
	native.mu.Lock()
	var reg, broker NodeRegistration
	for _, r := range native.document.Nodes {
		if r.ID == id {
			reg = r
		}
	}
	for _, r := range native.document.Nodes {
		if r.ID == reg.BrokerNodeID {
			broker = r
		}
	}
	dial := native.options.Dial
	native.mu.Unlock()
	if reg.ID != id || reg.Join != api.NodeOutgoing {
		return nil, nil, errors.New("exact outgoing node is not enrolled")
	}
	var agent interface{ Close() error }
	var port nodeagent.RoamingDeploymentPort
	if dial != nil {
		peer, e := dial(ctx, reg)
		if e != nil {
			return nil, nil, e
		}
		port, ok = peer.(nodeagent.RoamingDeploymentPort)
		if !ok {
			return nil, nil, errors.New("joined agent has no deployment port")
		}
		agent, _ = peer.(interface{ Close() error })
	} else {
		var peer *nodeagent.Client
		var e error
		if reg.BrokerNodeID == api.LocalNodeID {
			peer, e = nodeagent.Dial(ctx, reg.SocketPath, id)
		} else {
			if broker.Join != api.NodeSSH {
				return nil, nil, errors.New("enrolled outgoing coordinator route unavailable")
			}
			peer, e = nodeagent.NewSSHClient(ctx, nodeagent.SSHConfig{Target: broker.SSHDestination}, broker.HelperPath, reg.SocketPath, id)
		}
		if e != nil {
			return nil, nil, e
		}
		port, agent = peer, peer
	}
	return port, func() {
		if agent != nil {
			_ = agent.Close()
		}
	}, nil
}
func (n *roamingNativeAssembly) outgoingMetadata(ctx context.Context, id string) (nodeagent.RoamingDeploymentMetadata, error) {
	port, close, e := n.outgoingPort(ctx, id)
	if e != nil {
		return nodeagent.RoamingDeploymentMetadata{}, e
	}

	defer close()
	r, e := port.RoamingDeployment(ctx, nodeagent.RoamingDeploymentRequest{NodeID: id, Action: "metadata"})
	if e != nil || r.Metadata == nil {
		return nodeagent.RoamingDeploymentMetadata{}, errors.Join(errors.New("actual target deployment metadata unavailable"), e)
	}
	return *r.Metadata, nil
}
func (n *roamingNativeAssembly) outgoingDeployment(ctx context.Context, x roamingNativeNode, action, operationID, state string) (nodeagent.RoamingDeploymentReceipt, error) {
	port, close, e := n.outgoingPort(ctx, x.Registration.ID)
	if e != nil {
		return nodeagent.RoamingDeploymentReceipt{}, e
	}

	defer close()
	r := nodeagent.RoamingDeploymentRequest{NodeID: x.Registration.ID, OperationID: x.Plan.OperationID, PlanID: x.Plan.PlanID, Action: action, DisableOperationID: operationID, State: state}

	return port.RoamingDeployment(ctx, r)
}

// Configuration is frozen together with the full native roster. Callers doing
// preflight/provision pass that exact roster explicitly through this helper.
func (n *roamingNativeAssembly) prepareOutgoing(ctx context.Context, p roamingNativePlan, x roamingNativeNode, action string) error {
	port, close, e := n.outgoingPort(ctx, x.Registration.ID)
	if e != nil {
		return e
	}

	defer close()
	plan, e := json.Marshal(x.Plan)
	if e != nil {
		return e
	}
	workers, e := json.Marshal(nativeRoamingWorkers(p, x))
	if e != nil {
		return e
	}
	result, e := port.RoamingDeployment(ctx, nodeagent.RoamingDeploymentRequest{NodeID: x.Registration.ID, OperationID: p.OperationID, PlanID: p.ID, Action: action, Plan: plan, Workers: workers, Preferences: &x.Preferences})
	if e != nil || result.Outcome != "accepted" {
		return errors.Join(errors.New("exact outgoing deployment unconfirmed"), e)
	}
	return nil
}
func (n *roamingNativeAssembly) readOutgoingSupervisor(ctx context.Context, x roamingNativeNode, operationID string) (NodeRoamingSupervisorState, error) {
	r, e := n.outgoingDeployment(ctx, x, "receipt", operationID, "")
	return NodeRoamingSupervisorState{PlanID: r.PlanID, NodeID: r.NodeID, OperationID: r.DisableOperationID, State: r.State}, e
}

// ExecuteJoinedRoamingDeployment is invoked only by the fixed native companion
// through the existing paired agent. It shares the production supervisor plan,
// directory checks, locks and original disable receipt with the SSH path.
func ExecuteJoinedRoamingDeployment(ctx context.Context, directory, nodeID string, r nodeagent.RoamingDeploymentRequest) (nodeagent.RoamingDeploymentReceipt, error) {
	result := nodeagent.RoamingDeploymentReceipt{NodeID: r.NodeID, OperationID: r.OperationID, PlanID: r.PlanID, DisableOperationID: r.DisableOperationID, Outcome: "unknown"}
	if nodeID != r.NodeID || nodeagent.ValidateRoamingDeploymentRequest(r) != nil || r.Action == "metadata" {
		return result, errors.New("closed exact joined deployment required")
	}
	metadata, e := nodeagent.ReadRoamingDeploymentMetadata(directory, nodeID)
	if e != nil {
		return result, e
	}
	dir := filepath.Join(directory, "roaming-"+nativeRoamingKey(r.OperationID))
	path := filepath.Join(dir, "supervisor.json")
	var p NodeRoamingSupervisorPlan
	if r.Action == "preflight" || r.Action == "prepare" {
		d := json.NewDecoder(bytes.NewReader(r.Plan))
		d.DisallowUnknownFields()
		if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
			return result, errors.New("invalid closed supervisor plan")
		}
	} else {
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 64<<10 || nodeagent.CheckPrivateDirectory(dir) != nil {
			return result, errors.New("original private supervisor record unavailable")
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return result, e
		}
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
			return result, errors.New("original supervisor plan unavailable")
		}
	}
	if p.Directory != dir || p.NodeID != nodeID || p.PlanID != r.PlanID || p.OperationID != r.OperationID || p.Helper != metadata.Helper || p.HelperSHA256 != metadata.HelperSHA256 || p.Broker != nil || validateNativeSupervisor(p, path) != nil {
		return result, errors.New("original reviewed target deployment changed")
	}
	m := p.Managed
	if m == nil || m.JoinSSHDestination != metadata.Route.Target || m.BrokerSSHDestination != metadata.Route.Target || m.JoinHelper != metadata.Route.Helper {
		return result, errors.New("deployment must use existing paired outbound route")
	}
	if r.Action == "preflight" || r.Action == "prepare" {
		if e = nodeagent.ValidateRoamingWorkers(r.Workers); e != nil {
			return result, e
		}
		if r.Preferences.Schema != 1 || r.Preferences.Revision < 1 {
			return result, errors.New("target preferences invalid")
		}
		// Existing outward authorization is checked before retiring the source.
		if e = runRoamingSSH(ctx, m.JoinSSHDestination, nodeShellQuote(m.JoinHelper)+" verify-join-directory --directory "+nodeShellQuote(filepath.Dir(metadata.Route.Directory)), nil); e != nil {
			return result, errors.New("existing outbound coordinator authorization unavailable")
		}
		if r.Action == "preflight" {
			result.Outcome = "accepted"
			return result, nil
		}
		// A interrupted prepare is unknown. Never overwrite its original private
		// slot or restart an operation whose effect might already exist.
		if _, e = os.Lstat(dir); !errors.Is(e, os.ErrNotExist) {
			return result, errors.New("original deployment exists; reconcile without replay")
		}
		if e = nativeRoamingPrivateDir(dir); e != nil {
			return result, e
		}
		for _, sub := range []string{m.AgentDirectory, m.GenerationRoot} {
			if e = nativeRoamingPrivateDir(sub); e != nil {
				return result, e
			}
		}
		files := map[string]any{"supervisor.json": p, "workers.json": r.Workers, "agent/execution.json": r.Preferences, "agent/node.json": struct {
			ID string `json:"id"`
		}{nodeID}}
		for name, value := range files {
			if e = nodeagent.WriteManagedPrivateJSON(filepath.Join(dir, name), value); e != nil {
				return result, e
			}
		}
		if e = os.WriteFile(m.AuthFile, []byte(rand.Text()+rand.Text()), 0600); e != nil {
			return result, e
		}
		result.Outcome = "accepted"
		return result, nil
	}
	switch r.Action {
	case "start":
		unlock, e := lockNativeRoamingIntent(filepath.Join(dir, "deployment-launch.lock"))
		if e != nil {
			return result, e
		}
		defer unlock()
		intent := filepath.Join(dir, "launch-intent.json")
		if _, e = os.Lstat(intent); !errors.Is(e, os.ErrNotExist) {
			return result, nil
		}
		if nativeSupervisorState(p) != "running" {
			return result, errors.New("original deployment no longer running")
		}
		if e = nodeagent.WriteManagedPrivateJSON(intent, result); e != nil {
			return result, e
		}
		n := &roamingNativeAssembly{}
		// Launch through the same detached production supervisor as local/SSH.
		if e = n.launch(ctx, roamingNativeNode{Registration: NodeRegistration{ID: api.LocalNodeID}, Plan: p, HostHelper: p.Helper}); e != nil {
			return result, e
		}
		result.Outcome = "accepted"
		return result, nil
	case "control":
		if e = SetNodeRoamingSupervisorState(path, r.DisableOperationID, r.State); e != nil {
			return result, e
		}
		result.State = r.State
		result.Outcome = "accepted"
	case "receipt":
		receipt, e := ReadNodeRoamingSupervisorState(path, r.DisableOperationID)
		if e != nil {
			return result, e
		}
		result.State = receipt.State
		result.Outcome = "observed"
	}
	return result, nil
}

func (n *roamingNativeAssembly) stageOutgoingCompanion(ctx context.Context, x roamingNativeNode) error {
	if x.CompanionArtifact == nil {
		return nil
	}
	artifact := *x.CompanionArtifact
	if nodeagent.VerifyArtifact(artifact) != nil || artifact.HostExpectedSHA256 != x.Plan.HelperSHA256 {
		return errors.New("reviewed packaged companion changed")
	}
	port, close, e := n.outgoingPort(ctx, x.Registration.ID)
	if e != nil {
		return e
	}

	defer close()
	f, e := os.Open(artifact.HostPath)
	if e != nil {
		return e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return e
	}
	var offset int64
	buffer := make([]byte, 256<<10)
	for offset < info.Size() {
		count, e := io.ReadFull(f, buffer)
		if e != nil && e != io.ErrUnexpectedEOF {
			return e
		}
		request := nodeagent.RoamingDeploymentRequest{NodeID: x.Registration.ID, OperationID: x.Plan.OperationID, PlanID: x.Plan.PlanID, Action: "helper", Companion: &nodeagent.RoamingCompanionChunk{SHA256: artifact.HostExpectedSHA256, Architecture: artifact.Arch, SourceRevision: artifact.SourceRevision, Offset: offset, Total: info.Size(), Bytes: buffer[:count]}}
		receipt, e := port.RoamingDeployment(ctx, request)
		if e != nil || receipt.Outcome != "accepted" {
			return errors.Join(errors.New("original companion stage unconfirmed; reconcile without replay"), e)
		}
		offset += int64(count)
	}
	return nil
}
