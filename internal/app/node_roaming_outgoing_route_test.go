package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type outgoingRouteMetadataFixture struct {
	*nodeManagementFixture
	metadata nodeagent.RoamingDeploymentMetadata
}

func (f *outgoingRouteMetadataFixture) RoamingDeployment(_ context.Context, r nodeagent.RoamingDeploymentRequest) (nodeagent.RoamingDeploymentReceipt, error) {
	if r.Action != "metadata" || r.NodeID != f.metadata.NodeID || nodeagent.ValidateRoamingDeploymentRequest(r) != nil {
		return nodeagent.RoamingDeploymentReceipt{}, errors.New("fixture permits only exact metadata reads")
	}
	return nodeagent.RoamingDeploymentReceipt{NodeID: r.NodeID, Outcome: "observed", Metadata: &f.metadata}, nil
}

// Build the production plan, then send its actual serialized target plan through
// the production paired validation. The synthetic SSH executable never contacts
// a host or launches a Runtime; it accepts only this target's existing alias.
func TestOutgoingBuilderPreservesSourceRouteThroughPairedValidation(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "caelis-route-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	helper := filepath.Join(directory, "caelis-node")
	elf := []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 62, 0}
	if err = os.WriteFile(helper, elf, 0700); err != nil {
		t.Fatal(err)
	}
	if err = localstate.Write(filepath.Join(directory, "node.json"), struct {
		ID string `json:"id"`
	}{"fedora"}); err != nil {
		t.Fatal(err)
	}
	route := nodeagent.OutgoingRoute{Target: "fedora-existing-ubuntu", Helper: "/ubuntu/caelis-agent", Directory: "/ubuntu/joins/fedora"}
	if err = localstate.Write(filepath.Join(directory, "outgoing-route.json"), route); err != nil {
		t.Fatal(err)
	}
	metadata, err := nodeagent.ReadRoamingDeploymentMetadata(directory, "fedora")
	if err != nil {
		t.Fatal(err)
	}
	// Builder requires a Linux candidate. Only the native metadata DTO is a
	// Linux fixture; all private path/helper/route validation below is production.
	metadata.OS, metadata.Architecture = "linux", "amd64"
	artifact := nodeagent.Artifact{Path: helper, HostPath: helper, ExpectedSHA256: metadata.HelperSHA256, HostExpectedSHA256: metadata.HelperSHA256, Arch: "amd64", SourceRevision: strings.Repeat("a", 40)}
	sshLog := filepath.Join(directory, "ssh-calls")
	ssh := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + nodeShellQuote(sshLog) + "\ncase \"$*\" in *'uname -sm'*) printf 'Linux x86_64\\n';; *'-- fedora-existing-ubuntu '*) exit 0;; *) exit 91;; esac\n"
	if err = os.WriteFile(filepath.Join(directory, "ssh"), []byte(ssh), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	coordinator := NodeRegistration{ID: "ubuntu", Label: "Coordinator", Join: api.NodeSSH, SSHDestination: "mac-management-ubuntu", Directory: "/ubuntu/node-agent", HelperPath: route.Helper, HostHelperPath: "/ubuntu/caelis-node"}
	joined := NodeRegistration{ID: "fedora", Label: "Joined source", Join: api.NodeOutgoing, BrokerNodeID: coordinator.ID, SSHDestination: route.Target, Directory: route.Directory, HelperPath: route.Helper, SocketPath: filepath.Join(route.Directory, "agent.sock")}
	remote := singleNodeFixture(joined.ID)
	remote.catalog.Nodes[0].Runtimes = []api.NodeRuntime{{Backend: api.NodeCodex, Health: api.NodeHealthy, Authentication: api.NodeAuthenticated}}
	remote.configured = &api.NodeRuntimeConfiguration{Guard: api.NodeEditGuard{NodeID: joined.ID, Backend: api.NodeCodex, Revision: "1"}, ConfigurationAvailable: true}
	peer := &outgoingRouteMetadataFixture{nodeManagementFixture: remote, metadata: metadata}
	a := nativeManagementApplication(t)
	native := &nativeNodeManagement{app: a, local: singleNodeFixture(api.LocalNodeID), document: nodeManagementDocument{Version: 1, Revision: 1, Coordinator: coordinator.ID, Nodes: []NodeRegistration{coordinator, joined}}, clients: map[string]nodeplane.CatalogAgent{coordinator.ID: singleNodeFixture(coordinator.ID), joined.ID: peer}, options: NodeManagementNativeOptions{ExecutionState: func() (string, *api.WorkTarget) { return api.LocalNodeID, nil }, Dial: func(_ context.Context, r NodeRegistration) (nodeplane.CatalogAgent, error) {
		if r != joined {
			return nil, errors.New("fixture pairing changed")
		}
		return peer, nil
	}}}
	management := NewNodeManagement(native, native).(*nodeManagement)
	a.Backend.SetNodeManagementController(management)
	n := &roamingNativeAssembly{app: a, original: management, options: NodeRoamingNativeOptions{Host: func() (string, error) { return helper, nil }, Artifact: func(string) (nodeagent.Artifact, error) { return artifact, nil }, LocalCodexBinary: "/usr/bin/true"}}
	local := NodeRegistration{ID: api.LocalNodeID, Label: "Local source", Join: api.NodeLocal}
	in := NodeRoamingStageInput{BotID: "fixture-bot", OperationID: "route-original", Coordinator: coordinator, Nodes: []NodeRegistration{local, coordinator, joined}, SourceTarget: api.WorkTarget{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleBot}}
	p, err := n.build(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	var target roamingNativeNode
	for _, node := range p.Nodes {
		if node.Registration.ID == joined.ID {
			target = node
		}
		if node.Registration.ID == api.LocalNodeID && node.Plan.Managed.JoinSSHDestination != coordinator.SSHDestination {
			t.Fatal("ordinary local source route changed")
		}
	}
	if target.Plan.Managed == nil {
		t.Fatal("builder omitted joined target")
	}
	workers, err := json.Marshal(nativeRoamingWorkers(p, target))
	if err != nil {
		t.Fatal(err)
	}
	validate := func(plan NodeRoamingSupervisorPlan) error {
		wire, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		result, err := ExecuteJoinedRoamingDeployment(t.Context(), directory, joined.ID, nodeagent.RoamingDeploymentRequest{NodeID: joined.ID, Action: "preflight", OperationID: p.OperationID, PlanID: p.ID, Plan: wire, Workers: workers, Preferences: &target.Preferences})
		if err == nil && result.Outcome != "accepted" {
			return errors.New("paired preflight was not accepted")
		}
		return err
	}
	if err = validate(target.Plan); err != nil {
		t.Fatal("built source route failed actual paired validation", err)
	}
	if target.Plan.Managed.JoinSSHDestination != route.Target || target.Plan.Managed.BrokerSSHDestination != route.Target || target.Plan.Managed.JoinHelper != route.Helper {
		t.Fatal("builder did not freeze verified source route")
	}
	before, err := os.ReadFile(sshLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{coordinator.SSHDestination, "unpaired-third-alias"} {
		for _, field := range []string{"join", "broker"} {
			changed := target.Plan
			managed := *target.Plan.Managed
			changed.Managed = &managed
			if field == "join" {
				managed.JoinSSHDestination = alias
			} else {
				managed.BrokerSSHDestination = alias
			}
			if err = validate(changed); err == nil || !strings.Contains(err.Error(), "existing paired outbound route") {
				t.Fatalf("changed %s alias accepted or rejected outside paired validation: %v", field, err)
			}
		}
	}
	changed := target.Plan
	managed := *target.Plan.Managed
	changed.Managed = &managed
	managed.JoinHelper = "/different/caelis-agent"
	if err = validate(changed); err == nil || !strings.Contains(err.Error(), "existing paired outbound route") {
		t.Fatal("changed paired helper accepted", err)
	}
	after, err := os.ReadFile(sshLog)
	if err != nil || string(before) != string(after) {
		t.Fatal("changed route dispatched authorization before rejection", err)
	}
	if _, err = os.Stat(target.Plan.Directory); !os.IsNotExist(err) {
		t.Fatal("paired preflight wrote a supervisor slot", err)
	}
	peer.metadata.Route.Target = "unpaired-third-alias"
	if _, err = n.build(t.Context(), in); err == nil || !strings.Contains(err.Error(), "actual outward pairing differs") {
		t.Fatal("changed source metadata was not refused by builder", err)
	}
}
