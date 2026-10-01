package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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

func TestDefaultNativeColdCaelisPreflightStopsOwnedHostBeforeSourceFence(t *testing.T) {
	// Reuse the actual public-protocol foreground/watchdog fixtures belonging
	// to the production owned-readiness helper; no model or user process runs.
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	fixture := filepath.Join(dir, "caelis-readiness-fixture")
	compile := exec.CommandContext(t.Context(), "go", "test", "-c", "github.com/caelis-labs/caelis-bot/internal/backend/caelis", "-o", fixture)
	compile.Env = append(os.Environ(), "GOWORK=off")
	if output, e := compile.CombinedOutput(); e != nil {
		t.Fatalf("compile contained native fixture: %v\n%s", e, output)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	binary := filepath.Join(dir, "caelis")
	helper := filepath.Join(dir, "caelis-node")
	if e = os.WriteFile(binary, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'caelis 0.1.0'; exit 0; fi\nexec "+quote(fixture)+" -test.run='^TestOwnedReadinessProcessHelper$' -- \"$@\"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(helper, []byte("#!/bin/sh\nexec "+quote(fixture)+" -test.run='^TestOwnedCaelisWatchdogHelper$' -- \"$@\"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(dir, "app")
	store := filepath.Join(dir, "target-caelis-store")
	if e = os.MkdirAll(store, 0700); e != nil {
		t.Fatal(e)
	}
	if e = localstate.Write(filepath.Join(store, ".caelis-bot-node-owner.json"), map[string]string{"nodeId": api.LocalNodeID}); e != nil {
		t.Fatal(e)
	}
	if e = localstate.Write(filepath.Join(root, "runtime.json"), api.RuntimeSettings{Runtime: "codex", CLIPath: "/source/unowned-codex"}); e != nil {
		t.Fatal(e)
	}
	if e = localstate.Write(filepath.Join(root, "runtime-profiles", "caelis.json"), struct {
		Version int `json:"version"`
		api.RuntimeSettings
	}{1, api.RuntimeSettings{Runtime: "caelis", CLIPath: binary, CaelisStore: store}}); e != nil {
		t.Fatal(e)
	}
	if e = localstate.Write(filepath.Join(root, "work-execution.json"), api.WorkExecutionSettings{Model: "gpt-source-only", Effort: "high", ServiceTier: "priority"}); e != nil {
		t.Fatal(e)
	}
	a, e := New(root, Host{})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	options := DefaultNodeRoamingOptions(a, NodeRoamingNativeOptions{Host: func() (string, error) { return helper, nil }})
	n := options.RefreshNodeManagement.LocalAgent.(roamingManagedCatalog).assembly
	local := NodeRegistration{ID: api.LocalNodeID, Label: "This machine", Join: api.NodeLocal}
	input := NodeRoamingStageInput{BotID: "bot-fixture", OperationID: "cold-original-enable", Coordinator: local, Nodes: []NodeRegistration{local}, SourceTarget: api.WorkTarget{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleBot}}
	plan, e := options.PreparePlan(t.Context(), input)
	if e != nil {
		t.Fatal(e)
	}
	n.mu.Lock()
	frozen := n.plans[plan.ID]
	n.mu.Unlock()
	found := false
	for _, binding := range frozen.Nodes[0].RuntimeBindings {
		if binding.Backend == "caelis" {
			found = true
			if binding.Binary != binary || binding.Store != store || binding.Model != "" || binding.Execution != (api.WorkExecutionSettings{}) {
				t.Fatal("cold target copied source model or lost private native binding", binding)
			}
		}
	}
	if !found {
		t.Fatal("marked cold Caelis alternative was excluded by unknown catalogue auth")
	}
	if _, e = os.Lstat(filepath.Join(store, "fixture-pids")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("read-only preparation started a native Host", e)
	}
	input.ReviewedPlanID = plan.ID
	if e = options.Preflight(t.Context(), input); !errors.Is(e, ErrNodeRoamingPreflight) {
		t.Fatal("missing explicit persistent approval was accepted", e)
	}
	if _, e = os.Lstat(filepath.Join(store, "fixture-pids")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("unapproved preflight started target Host", e)
	}
	input.AllowPersistentExecution = true
	e = options.Preflight(t.Context(), input)
	if !errors.Is(e, ErrNodeRoamingPreflight) || !strings.Contains(e.Error(), "owned shutdown authority") {
		t.Fatal("bounded target readiness did not precede unchanged unowned source guard", e)
	}
	service, e := nodeagent.New(nodeagent.Options{Directory: filepath.Join(root, "nodeplane", "local"), NodeID: api.LocalNodeID})
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := service.ReadOwnedRuntimeReadiness(t.Context(), api.LocalNodeID, api.NodeCaelis, input.OperationID)
	if e != nil || receipt.Outcome != "ready" || !receipt.Ready || !receipt.StopConfirmed || receipt.Model != "native-owned-current" {
		t.Fatal("actual default owned readiness lacks original confirmed-stop receipt", receipt, e)
	}
	if a.sourceRetired || a.started {
		t.Fatal("source admission changed before target proof/source ownership guard")
	}
	pids, e := os.ReadFile(filepath.Join(store, "fixture-pids"))
	if e != nil || len(pids) == 0 {
		t.Fatal("real target owned foreground fixture did not run", e)
	}
	requestBytes, _ := os.ReadFile(filepath.Join(store, "fixture-requests"))
	for _, request := range strings.Split(strings.TrimSpace(string(requestBytes)), "\n") {
		if request != "GET /status" && request != "GET /initialize" && request != "POST /completion/slash-arguments" {
			t.Fatal("readiness made a model/config/application/session request", request)
		}
	}
	if _, e = os.Lstat(filepath.Join(store, "runtime", "service", "discovery.json")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("owned readiness left a target controller discovery", e)
	}
	if e = options.Preflight(t.Context(), input); !errors.Is(e, ErrNodeRoamingPreflight) {
		t.Fatal("repeated original preflight unexpectedly retired source", e)
	}
	currentPids, _ := os.ReadFile(filepath.Join(store, "fixture-pids"))
	if string(currentPids) != string(pids) {
		t.Fatal("original readiness receipt lookup relaunched target Host")
	}
	readinessDir := filepath.Join(root, "nodeplane", "local", "owned-readiness")
	entries, e := os.ReadDir(readinessDir)
	if e != nil || len(entries) != 1 {
		t.Fatal("original bounded action journal missing", entries, e)
	}
	if e = os.WriteFile(filepath.Join(readinessDir, entries[0].Name(), "intent.json"), []byte("interrupted-original-record"), 0600); e != nil {
		t.Fatal(e)
	}
	e = options.Preflight(t.Context(), input)
	if e == nil || !strings.Contains(e.Error(), "owned Host stop is unconfirmed") || a.sourceRetired || a.started {
		t.Fatal("unknown original action did not preserve source", e)
	}
	currentPids, _ = os.ReadFile(filepath.Join(store, "fixture-pids"))
	if string(currentPids) != string(pids) {
		t.Fatal("unknown original action replayed Host")
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
