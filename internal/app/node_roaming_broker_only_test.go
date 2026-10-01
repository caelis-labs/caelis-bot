package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type brokerOnlyMetadataFixture struct {
	*nodeManagementFixture
	configurationCalls int
}

func (f *brokerOnlyMetadataFixture) Configuration(context.Context, string, api.NodeBackend) (api.NodeRuntimeConfiguration, error) {
	f.configurationCalls++
	return api.NodeRuntimeConfiguration{}, errors.New("coordinator has no Runtime configuration")
}

// The SSH binary is an isolated architecture/control fixture; no network or
// Runtime is used. Ubuntu metadata intentionally has no healthy/auth account.
func TestNativeRemoteCoordinatorDoesNotRequireOrDeployRuntime(t *testing.T) {
	a := nativeManagementApplication(t)
	// Only metadata is inspected; this stable synthetic profile never exists.
	a.root = "/fixture/isolated-mac"
	dir := t.TempDir()
	helper := filepath.Join(dir, "host")
	if e := os.WriteFile(helper, []byte("verified fixture bytes"), 0700); e != nil {
		t.Fatal(e)
	}
	if supplied := os.Getenv("CAELIS_BOT_TEST_HOST_BINARY"); supplied != "" {
		if !filepath.IsAbs(supplied) {
			t.Fatal("explicit inspection helper must be absolute")
		}
		helper = supplied
	}
	ssh := filepath.Join(dir, "ssh")
	if e := os.WriteFile(ssh, []byte("#!/bin/sh\ncase \"$*\" in *'uname -sm'*) printf 'Linux x86_64\\n';; *) exit 0;; esac\n"), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	coordinator := NodeRegistration{ID: "ubuntu", Label: "Coordinator", Join: api.NodeSSH, SSHDestination: "fixture-ubuntu", Directory: "/home/coordinator/.local/share/caelis-bot/node-agent", HelperPath: "/home/coordinator/.local/share/caelis-bot/node-agent/agent", HostHelperPath: "/home/coordinator/.local/share/caelis-bot/node-agent/host"}
	standby := coordinator
	standby.ID = "fedora"
	standby.Label = "Standby"
	standby.SSHDestination = "fixture-fedora"
	standby.Directory = "/home/standby/.local/share/caelis-bot/node-agent"
	brokerFixture := &brokerOnlyMetadataFixture{nodeManagementFixture: singleNodeFixture(coordinator.ID)}
	brokerFixture.catalog.Nodes[0].Runtimes = []api.NodeRuntime{{Backend: api.NodeCodex, Health: api.NodeMissing, Authentication: api.NodeAuthRequired}}
	standbyFixture := singleNodeFixture(standby.ID)
	standbyFixture.catalog.Nodes[0].Runtimes = []api.NodeRuntime{{Backend: api.NodeCodex, Health: api.NodeHealthy, Authentication: api.NodeAuthenticated, Roles: []api.NodeRoleCapability{{Role: api.RoleBot, Eligible: true}}}}
	standbyFixture.configured = &api.NodeRuntimeConfiguration{Guard: api.NodeEditGuard{NodeID: standby.ID, Backend: api.NodeCodex, Revision: "1"}, ConfigurationAvailable: true}
	native := &nativeNodeManagement{app: a, local: singleNodeFixture(api.LocalNodeID), document: nodeManagementDocument{Version: 1, Nodes: []NodeRegistration{coordinator, standby}}, clients: map[string]nodeplane.CatalogAgent{coordinator.ID: brokerFixture, standby.ID: standbyFixture}, options: NodeManagementNativeOptions{ExecutionState: func() (string, *api.WorkTarget) { return "", nil }}}
	a.Backend.SetNodeManagementController(NewNodeManagement(native, native))
	sha, e := nativeRoamingDigest(helper)
	if e != nil {
		t.Fatal(e)
	}
	if expected := os.Getenv("CAELIS_BOT_TEST_HOST_SHA256"); expected != "" && expected != sha {
		t.Fatal("inspection helper differs from explicit checksum")
	}
	runtimeReads := 0
	n := &roamingNativeAssembly{app: a, options: NodeRoamingNativeOptions{Host: func() (string, error) { return helper, nil }, Artifact: func(string) (nodeagent.Artifact, error) {
		return nodeagent.Artifact{HostPath: helper, HostExpectedSHA256: sha}, nil
	}, RuntimeSettings: func(_ context.Context, r NodeRegistration, b api.NodeBackend) (api.RuntimeSettings, error) {
		if r.ID == coordinator.ID {
			runtimeReads++
		}
		return api.RuntimeSettings{}, errors.New("fixture has no alternate runtime")
	}}}
	local := NodeRegistration{ID: api.LocalNodeID, Label: "Preferred source", Join: api.NodeLocal}
	in := NodeRoamingStageInput{BotID: "bot-fixture", OperationID: "enable-original", Coordinator: coordinator, Nodes: []NodeRegistration{local, coordinator, standby}, SourceTarget: api.WorkTarget{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleBot}, Resume: true}
	summary, e := n.prepare(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	p := n.plans[summary.ID]

	if output := os.Getenv("CAELIS_BOT_TEST_NATIVE_PLAN_OUTPUT"); output != "" {
		if !filepath.IsAbs(output) || os.Getenv("CAELIS_BOT_TEST_HOST_BINARY") == "" || os.Getenv("CAELIS_BOT_TEST_HOST_SHA256") == "" {
			t.Fatal("native inspection output requires absolute destination and explicit checksummed helper")
		}
		encoded, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(encoded)
		document := struct {
			Fixture        bool                    `json:"fixture"`
			LiveDeployment bool                    `json:"liveDeployment"`
			HelperSHA256   string                  `json:"helperSha256"`
			PlanSHA256     string                  `json:"planSha256"`
			Review         backend.NodeRoamingPlan `json:"review"`
			NativePlan     roamingNativePlan       `json:"nativePlan"`
		}{true, false, sha, hex.EncodeToString(sum[:]), summary, p}
		if err = localstate.Write(output, document); err != nil {
			t.Fatal(err)
		}
	}
	var broker roamingNativeNode
	for _, node := range p.Nodes {
		if node.Registration.ID == coordinator.ID {
			broker = node
		}
	}
	if broker.Plan.Broker == nil || broker.Plan.Managed != nil || broker.AgentSocket != "" || broker.BrokerPeerSocket != "" || len(broker.RuntimeBindings) != 0 || brokerFixture.configurationCalls != 0 || runtimeReads != 0 {
		t.Fatal("coordinator acquired Runtime authority", broker, runtimeReads, brokerFixture.configurationCalls)
	}
	for _, action := range summary.Actions {
		if action.NodeID == coordinator.ID && (action.Action == backend.NodeRoamingStartBot || action.Action == backend.NodeRoamingConnectOutgoing) {
			t.Fatal("review advertises broker Runtime", action)
		}
	}
	if e = validateNativeRoamingSockets(p); e != nil {
		t.Fatal(e)
	}
	canonicalTemp, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p.LocalDirectory) != canonicalTemp {
		t.Fatal("local sockets use long per-user TempDir", p.LocalDirectory)
	}

	for _, node := range p.Nodes {
		if e = nodeagent.ValidateRoamingSupervisor(node.Plan, filepath.Join(node.Plan.Directory, "supervisor.json")); e != nil {
			t.Fatal(node.Registration.ID, e)
		}
	}
	in.AllowPersistentExecution = true
	in.ReviewedPlanID = p.ID
	if e = n.preflight(t.Context(), in); e != nil {
		t.Fatal("authenticated standby plus broker-only coordinator rejected", e)
	}
	if e = n.confirmOwnedReadiness(t.Context(), &p); e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(nativeRoamingWorkers(p, p.Nodes[0]))
	if strings.Contains(string(encoded), coordinator.ID) {
		t.Fatal("broker acquired Worker/source route", string(encoded))
	}
	// Even a later authenticated coordinator remains dedicated by reviewed role;
	// metadata is retained and changing it does not add a Bot launch to the plan.
	brokerFixture.catalog.Nodes[0].Runtimes = standbyFixture.catalog.Nodes[0].Runtimes
	next, e := n.prepare(t.Context(), in)
	if e != nil || next.ID != p.ID {
		t.Fatal("coordinator account changed dedicated plan", next, e)
	}
}

func TestNativeBrokerOnlyProvisionContainsOnlyCacheAndSupervisor(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	log := filepath.Join(root, "ssh.log")
	shim := filepath.Join(root, "ssh")
	if e = os.WriteFile(shim, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$BROKER_FIXTURE_LOG\"\ncat > /dev/null\n"), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BROKER_FIXTURE_LOG", log)
	dir := "/home/coordinator/.local/share/caelis-bot/node-agent/roaming-" + nativeRoamingKey("original")
	ipc := nodeagent.RoamingIPCDirectory(filepath.Dir(dir), "original")
	node := roamingNativeNode{Registration: NodeRegistration{ID: "ubuntu", Join: api.NodeSSH, SSHDestination: "fixture-ubuntu", HelperPath: "/fixed/agent"}, Plan: NodeRoamingSupervisorPlan{Directory: dir, IPCDirectory: ipc, Broker: &NodeRoamingBrokerDeployment{NodeID: "ubuntu", BotID: "bot", Profile: filepath.Join(dir, "broker"), Socket: filepath.Join(ipc, "broker.sock")}}}
	p := roamingNativePlan{BotID: "bot", SourceNodeID: api.LocalNodeID, SourceBackend: "codex", Nodes: []roamingNativeNode{node}, BootstrapDirectory: filepath.Join(ipc, "bootstrap"), BootstrapSocket: filepath.Join(ipc, "bootstrap", "agent.sock")}
	n := &roamingNativeAssembly{}
	if e = n.provision(t.Context(), p); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(log)
	if e != nil {
		t.Fatal(e)
	}
	for _, forbidden := range []string{"workers.json", "product.token", "agent/node.json", "agent/execution.json", "generations"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatal("broker provisioning requested Runtime state", forbidden)
		}
	}
	for _, required := range []string{"supervisor.json", "peers.json", "bootstrap-peers.json", ipc} {
		if !strings.Contains(string(data), required) {
			t.Fatal("missing native broker state", required)
		}
	}
}

func TestNativeBrokerOnlyUnknownRecoveryNeverReplaysOrReplacesOriginal(t *testing.T) {
	a := nativeManagementApplication(t)
	dir := "/home/coordinator/.local/share/caelis-bot/node-agent/roaming-" + nativeRoamingKey("enable-original")
	ipc := nodeagent.RoamingIPCDirectory(filepath.Dir(dir), "enable-original")
	reg := NodeRegistration{ID: "ubuntu", Join: api.NodeSSH, SSHDestination: "unreachable-fixture", Directory: filepath.Dir(dir)}
	id := strings.Repeat("a", 64)
	plan := NodeRoamingSupervisorPlan{Version: 1, PlanID: id, OperationID: "enable-original", NodeID: reg.ID, Directory: dir, IPCDirectory: ipc, Helper: "/verified/host", HelperSHA256: strings.Repeat("b", 64), Broker: &NodeRoamingBrokerDeployment{BotID: "bot", NodeID: reg.ID, Profile: filepath.Join(dir, "broker"), Socket: filepath.Join(ipc, "broker.sock"), PeersFile: filepath.Join(dir, "peers.json"), BootstrapPeersFile: filepath.Join(dir, "bootstrap-peers.json")}}
	p := roamingNativePlan{ID: id, OperationID: plan.OperationID, BotID: "bot", Coordinator: reg, Phase: "provisioned", Nodes: []roamingNativeNode{{Registration: reg, Plan: plan}}, BootstrapDirectory: filepath.Join(ipc, "bootstrap"), BootstrapSocket: filepath.Join(ipc, "bootstrap", "agent.sock")}
	manifest := filepath.Join(a.root, "nodeplane", "roaming-deployment.json")
	if e := localstate.Write(manifest, p); e != nil {
		t.Fatal(e)
	}
	n := &roamingNativeAssembly{app: a}
	input := NodeRoamingRecoveryInput{OperationID: p.OperationID, StageOperationID: p.OperationID, OperationKind: "enable", SourceRetiredIntent: true, Phase: "staging", StageInput: NodeRoamingStageInput{ReviewedPlanID: id, BotID: p.BotID, Coordinator: reg}}
	original, e := os.ReadFile(manifest)
	if e != nil {
		t.Fatal(e)
	}
	for _, operation := range []string{p.OperationID, "replacement-enable"} {
		input.OperationID = operation
		result, e := n.recover(t.Context(), input)
		if e == nil || result.Outcome != "unknown" {
			t.Fatal("unconfirmed original broker deployment replayed", result, e)
		}
	}
	after, e := os.ReadFile(manifest)
	if e != nil || string(original) != string(after) {
		t.Fatal("lookup changed durable original intent", e)
	}
	stored, e := readNativeRoamingPlan(manifest)
	if e != nil || stored.Nodes[0].Plan.IPCDirectory != ipc || stored.Nodes[0].Plan.Broker.Socket != plan.Broker.Socket {
		t.Fatal("original frozen IPC paths did not survive recovery", e)
	}
}
