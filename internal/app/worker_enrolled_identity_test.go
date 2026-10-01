package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodes"
)

const enrolledWorkerID = "node-FT2AO3JXSZJ3QZQHSNNLZ77MV4"

func TestEnrolledWorkerIdentitySurvivesSaveLoadAndExactStartupSelection(t *testing.T) {
	for _, transport := range []string{"", "registered-agent"} {
		t.Run(transport, func(t *testing.T) {
			config := backend.WorkerNodeConfig{ID: enrolledWorkerID, Label: "Enrolled Worker", Backend: "codex", Transport: transport}
			if transport == "" {
				config.SSH, config.Socket, config.WorkspaceRoot = "fixture-host", "/private/worker.sock", "/private/work"
			}
			assertEnrolledWorkerSaveLoad(t, config)
		})
	}
}

func assertEnrolledWorkerSaveLoad(t *testing.T, config backend.WorkerNodeConfig) {
	t.Helper()
	root := t.TempDir()
	registry, err := nodes.New("fixture", newTestEngine())
	if err != nil {
		t.Fatal(err)
	}
	c := openWorkerNodes(filepath.Join(root, "worker-nodes.json"), registry, nil)
	t.Cleanup(func() { _ = c.Close() })
	service := &backend.Service{}
	service.ConfigureWorkerNodes(c)
	saved, err := service.SaveWorkerNode(config, service.WorkerNodes().Revision)
	if err != nil || len(saved.Nodes) != 1 || saved.Nodes[0].Config != config || saved.Nodes[0].State != "candidate" {
		t.Fatal("enrolled identity was rejected, rewritten or admitted", saved, err)
	}
	doc, err := loadWorkerNodeDocument(c.path)
	if err != nil || len(doc.Nodes) != 1 || doc.Nodes[0] != config {
		t.Fatal("saved enrolled identity cannot be loaded exactly", doc, err)
	}
	target := configuredWorkerTarget(config)
	if err := ValidateConfiguredWorkerTargets(root, []api.WorkTarget{target}); err != nil {
		t.Fatal("exact enrolled startup selection rejected", err)
	}
	lower := target
	lower.NodeID = strings.ToLower(target.NodeID)
	if ValidateConfiguredWorkerTargets(root, []api.WorkTarget{lower}) == nil {
		t.Fatal("case-folded identity substituted for enrolled target")
	}
	if _, err := registry.ResolveWorkTarget(&target); err == nil {
		t.Fatal("saving identity advertised native readiness")
	}
}

func TestEnrolledWorkerIdentityFromNativeRoamingProjectionLoadsExactly(t *testing.T) {
	local := roamingNativeNode{Registration: NodeRegistration{ID: api.LocalNodeID, Label: "Local"}, Plan: NodeRoamingSupervisorPlan{Managed: &NodeRoamingManagedDeployment{Backend: "codex"}}}
	remote := roamingNativeNode{Registration: NodeRegistration{ID: enrolledWorkerID, Label: "Enrolled Worker"}, Plan: NodeRoamingSupervisorPlan{Managed: &NodeRoamingManagedDeployment{Backend: "codex"}}, RuntimeBindings: []NodeRoamingWorkerRuntime{{Backend: "codex", Binary: "/native/codex"}}, BrokerPeerSocket: "/private/agent.sock"}
	wire, err := json.Marshal(nativeRoamingWorkers(roamingNativePlan{Nodes: []roamingNativeNode{local, remote}}, local))
	if err != nil {
		t.Fatal(err)
	}
	var roster struct {
		Nodes []backend.WorkerNodeConfig `json:"nodes"`
	}
	if err := json.Unmarshal(wire, &roster); err != nil || len(roster.Nodes) != 1 || roster.Nodes[0].ID != enrolledWorkerID || roster.Nodes[0].Transport != "registered-agent" {
		t.Fatal("native roaming roster changed enrolled identity", roster, err)
	}
	assertEnrolledWorkerSaveLoad(t, roster.Nodes[0])
}

func TestEnrolledWorkerIdentityStillRejectsUnsafeCharactersAndLength(t *testing.T) {
	for _, id := range []string{"", "-node", "node/ESCAPE", "node\\ESCAPE", "node..ESCAPE", "node_ESCAPE", "node:\\ESCAPE", "node\nESCAPE", "node\x00ESCAPE", "1node", strings.Repeat("A", 65)} {
		config := backend.WorkerNodeConfig{ID: id, Label: "Worker", Backend: "codex", Transport: "registered-agent"}
		if validateWorkerNode(config) == nil {
			t.Fatal("unsafe Worker identity accepted", id)
		}
	}
}
