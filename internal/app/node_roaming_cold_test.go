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

func TestNativeColdCaelisChecksAllModelsInOneOriginalAction(t *testing.T) {
	reg := NodeRegistration{ID: "cold-target", Label: "Target"}
	node := roamingNativeNode{Registration: reg, Plan: NodeRoamingSupervisorPlan{Managed: &NodeRoamingManagedDeployment{Backend: "caelis", CaelisBinary: "/target/caelis", CaelisStore: "/target/owned-store", Model: "target-main"}}, RuntimeBindings: []NodeRoamingWorkerRuntime{{Backend: "caelis", Binary: "/target/caelis", Store: "/target/owned-store", Model: "target-worker"}}}
	p := roamingNativePlan{ID: strings.Repeat("a", 64), OperationID: "original-enable", Nodes: []roamingNativeNode{node}}
	available := true
	confirmed := true
	calls := 0
	n := &roamingNativeAssembly{options: NodeRoamingNativeOptions{OwnedRuntimeReadiness: func(_ context.Context, got NodeRegistration, request nodeagent.OwnedRuntimeReadinessRequest) (nodeagent.OwnedRuntimeReadinessReceipt, error) {
		calls++
		if got != reg || request.OperationID != p.OperationID || request.Model != "target-main" || request.ExpectedBinary != node.Plan.Managed.CaelisBinary || request.ExpectedStore != node.Plan.Managed.CaelisStore {
			t.Fatal("approved readiness lost frozen target or original operation", request)
		}
		models := []string{"target-main"}
		if available {
			models = append(models, "target-worker")
		}
		return nodeagent.OwnedRuntimeReadinessReceipt{NodeID: reg.ID, Backend: api.NodeCaelis, OperationID: request.OperationID, Outcome: "ready", Ready: true, Model: "target-main", AuthenticatedModels: models, StopConfirmed: confirmed}, nil
	}}}
	before, _ := json.Marshal(p.Nodes)
	if e := n.confirmOwnedReadiness(t.Context(), &p); e != nil || calls != 1 || len(p.UnavailableCaelisWorkers) != 0 {
		t.Fatal("single owned action did not prove both target-native models", e, calls, p.UnavailableCaelisWorkers)
	}
	available = false
	if e := n.confirmOwnedReadiness(t.Context(), &p); e != nil || !p.UnavailableCaelisWorkers[reg.ID] {
		t.Fatal("unavailable optional model blocked authenticated primary", e)
	}
	after, _ := json.Marshal(p.Nodes)
	if string(before) != string(after) || p.ID != strings.Repeat("a", 64) {
		t.Fatal("readiness changed frozen review bindings/model/hash")
	}
	encoded, _ := json.Marshal(nativeRoamingWorkers(p, node))
	if strings.Contains(string(encoded), "target-worker") || strings.Contains(string(encoded), "/target/owned-store") {
		t.Fatal("unavailable optional Worker was advertised", string(encoded))
	}
	confirmed = false
	if e := n.confirmOwnedReadiness(t.Context(), &p); e == nil {
		t.Fatal("unconfirmed owned Host stop was hidden as optional unavailability")
	}
}

func TestDefaultNativeColdCaelisPrimaryUsesMarkedTargetAndCurrentDefault(t *testing.T) {
	a := nativeManagementApplication(t)
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(dir, "caelis")
	if e = os.WriteFile(binary, []byte("#!/bin/sh\nprintf '{\"version\":\"0.1.0\"}\\n'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	store := filepath.Join(dir, "owned-store")
	if e = localstate.Write(filepath.Join(store, ".caelis-bot-node-owner.json"), map[string]string{"nodeId": "cold-target"}); e != nil {
		t.Fatal(e)
	}
	service, e := nodeagent.New(nodeagent.Options{Directory: dir, NodeID: "cold-target", Binaries: map[api.NodeBackend]string{api.NodeCaelis: binary}, Configurations: map[api.NodeBackend]nodeagent.NativeConfiguration{api.NodeCaelis: &nodeagent.CaelisConfiguration{Settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: binary, CaelisStore: store}}}})
	if e != nil {
		t.Fatal(e)
	}
	reg := NodeRegistration{ID: "cold-target", Label: "Cold target", Join: api.NodeSSH, Directory: dir, SSHDestination: "existing-target", HelperPath: "/private/paired/helper"}
	native := &nativeNodeManagement{app: a, local: singleNodeFixture(api.LocalNodeID), options: NodeManagementNativeOptions{ExecutionState: func() (string, *api.WorkTarget) { return "", nil }}, document: nodeManagementDocument{Version: 1, Nodes: []NodeRegistration{reg}}, clients: map[string]nodeplane.CatalogAgent{reg.ID: service}}
	a.Backend.SetNodeManagementController(NewNodeManagement(native, native))
	n := &roamingNativeAssembly{app: a}
	n.options.RuntimeSettings = n.runtimeSettings
	n.options.OwnedRuntimeProbe = n.ownedRuntimeProbe
	catalog, e := service.Catalog(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	for _, runtime := range catalog.Nodes[0].Runtimes {
		if runtime.Backend == api.NodeCaelis && (runtime.Authentication == api.NodeAuthenticated || runtime.Health == api.NodeHealthy || runtime.Roles[0].Eligible) {
			t.Fatal("cold target advertised ready owned role", runtime)
		}
	}
	selected, e := n.candidateBackend(t.Context(), reg, "codex")
	if e != nil || selected != "caelis" {
		t.Fatal("marked cold target was excluded as provisional primary", selected, e)
	}
	preferences, e := n.targetPreferences(t.Context(), reg, api.NodeCaelis)
	if e != nil || preferences.Conversation != (api.WorkExecutionSettings{}) || preferences.Worker != (api.WorkExecutionSettings{}) {
		t.Fatal("cold target-current default borrowed source preferences", preferences, e)
	}
	if e = os.Remove(filepath.Join(store, ".caelis-bot-node-owner.json")); e != nil {
		t.Fatal(e)
	}
	if _, e = n.candidateBackend(t.Context(), reg, "codex"); e == nil {
		t.Fatal("unmarked target became provisional primary")
	}
	if _, e = os.Lstat(filepath.Join(store, "runtime")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("read-only provisional primary probe started target Host", e)
	}
}
