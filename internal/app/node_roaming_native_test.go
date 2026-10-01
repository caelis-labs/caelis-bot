package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/codex"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/memorytransfer"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodebroker"
	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

func TestNativeRoamingPlanBindsApprovalAndChangesWithoutEffects(t *testing.T) {
	a := nativeManagementApplication(t)
	helper := filepath.Join(a.root, "reviewed-host")
	if e := os.WriteFile(helper, []byte("reviewed fixture bytes"), 0700); e != nil {
		t.Fatal(e)
	}
	n := &roamingNativeAssembly{app: a, options: NodeRoamingNativeOptions{Host: func() (string, error) { return helper, nil }}}
	n.original = nativeLocalEnrollmentManagementFixture(t)
	local := NodeRegistration{ID: api.LocalNodeID, Label: "This machine", Join: api.NodeLocal}
	in := NodeRoamingStageInput{BotID: "bot-fixture", OperationID: "original-enable", Coordinator: local, Nodes: []NodeRegistration{local}, SourceTarget: api.WorkTarget{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleBot}}
	p, e := n.prepare(t.Context(), in)
	if e != nil || p.ID == "" || !p.RequiresConfirmation {
		t.Fatal(p, e)
	}
	for _, a := range p.Actions {
		switch a.Action {
		case backend.NodeRoamingPrepareCoordinator, backend.NodeRoamingPrepareNode, backend.NodeRoamingStartBot, backend.NodeRoamingStopSource:
		default:
			t.Fatal("unclosed user action", a)
		}
	}
	wire, _ := json.Marshal(p)
	if strings.Contains(string(wire), a.root) || strings.Contains(string(wire), "AuthFile") {
		t.Fatal("native private paths escaped plan summary")
	}
	if e = n.preflight(t.Context(), in); !errors.Is(e, ErrNodeRoamingPreflight) {
		t.Fatal("unreviewed intent was not refused", e)
	}
	in.ReviewedPlanID = p.ID
	in.AllowPersistentExecution = true
	if e = n.preflight(t.Context(), in); !errors.Is(e, ErrNodeRoamingPreflight) {
		t.Fatal("unowned source was not refused", e)
	}
	in.Nodes[0].Label = "Changed enrollment"
	changed, e := n.prepare(t.Context(), in)
	if e != nil || changed.ID == p.ID {
		t.Fatal("plan did not bind enrollment", e)
	}
	if _, e = os.Stat(filepath.Join(a.root, "nodeplane", "roaming-deployment.json")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("read-only preparation wrote deployment state", e)
	}
	files, e := os.ReadDir(a.root)
	if e != nil || len(files) != 1 {
		t.Fatal("preparation added native effects", files, e)
	}
}
func TestNativeSupervisorPlanRejectsEscapedSlotsAndPairing(t *testing.T) {
	dir := t.TempDir()
	p := NodeRoamingSupervisorPlan{Version: 1, PlanID: strings.Repeat("a", 64), OperationID: "original", NodeID: "node", Helper: "/verified/helper", HelperSHA256: strings.Repeat("b", 64), Directory: dir, Broker: &NodeRoamingBrokerDeployment{BotID: "bot", NodeID: "node", Profile: filepath.Join(dir, "profile"), Socket: filepath.Join(dir, "b.sock"), PeersFile: filepath.Join(dir, "peers.json"), BootstrapPeersFile: filepath.Join(dir, "bootstrap.json")}}
	path := filepath.Join(dir, "supervisor.json")
	if e := validateNativeSupervisor(p, path); e != nil {
		t.Fatal(e)
	}
	p.Broker.Profile = filepath.Join(dir, "..", "unrelated")
	if e := validateNativeSupervisor(p, path); e == nil {
		t.Fatal("escaped native directory accepted")
	}
}

// A contained real-process fixture exercises the independently detached
// lifetime. The child broker is real private IPC; no Runtime or model is used.
func init() {
	if os.Getenv("CAELIS_NATIVE_SUPERVISOR_FIXTURE") != "1" || len(os.Args) < 2 {
		return
	}
	mode := os.Args[1]
	if mode != "supervise-roaming" && mode != "serve-broker" && mode != "deploy-fixture-app" {
		return
	}
	find := func(flag string) string {
		for i, a := range os.Args {
			if a == flag && i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
		return ""
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	signal.Ignore(syscall.SIGHUP)
	var e error
	switch mode {
	case "deploy-fixture-app":
		var p NodeRoamingSupervisorPlan
		b, err := os.ReadFile(find("--plan-file"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if json.Unmarshal(b, &p) != nil {
			os.Exit(2)
		}
		n := &roamingNativeAssembly{}
		e = n.launch(ctx, roamingNativeNode{Registration: NodeRegistration{ID: api.LocalNodeID}, Plan: p, HostHelper: p.Helper})
	case "supervise-roaming":
		e = RunNodeRoamingSupervisor(ctx, find("--plan-file"))
	case "serve-broker":
		owner, err := nodecoord.Open(nodecoord.Options{Directory: find("--profile"), BotID: find("--bot-id"), BrokerNodeID: find("--node-id")})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		defer owner.Close()
		e = nodebroker.ServeUnix(ctx, find("--socket"), owner, nil)
	}
	if e != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(2)
	}
	os.Exit(0)
}
func TestNativeSupervisorSurvivesLaunchingAPPExitAndHonorsDisable(t *testing.T) {
	if os.Getenv("CAELIS_NATIVE_SUPERVISOR_FIXTURE") == "1" {
		t.Fatal("fixture must run only in its child process")
	}
	// Use a short private socket slot so platform path limits are also real.
	dir, e := os.MkdirTemp("/tmp", "roam-supervisor-")
	if e != nil {
		t.Fatal(e)
	}
	dir, e = filepath.EvalSymlinks(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	helper, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	digest, e := nativeRoamingDigest(helper)
	if e != nil {
		t.Fatal(e)
	}
	p := NodeRoamingSupervisorPlan{Version: 1, PlanID: strings.Repeat("c", 64), OperationID: "fixture-approved-enable", NodeID: "fixture-coordinator", Helper: helper, HelperSHA256: digest, Directory: dir, Broker: &NodeRoamingBrokerDeployment{BotID: "bot-fixture", NodeID: "fixture-coordinator", Profile: filepath.Join(dir, "broker"), Socket: filepath.Join(dir, "b.sock"), PeersFile: filepath.Join(dir, "peers.json"), BootstrapPeersFile: filepath.Join(dir, "bootstrap.json"), PreferredNodeID: "fixture-coordinator"}}
	filename := filepath.Join(dir, "supervisor.json")
	if e = localstate.Write(filename, p); e != nil {
		t.Fatal(e)
	}
	defer func() {
		_ = SetNodeRoamingSupervisorState(filename, "fixture-approved-disable", "disabled")
		for i := 0; i < 100; i++ {
			if _, e := net.DialTimeout("unix", p.Broker.Socket, 20*time.Millisecond); e != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	launchingAPP := exec.Command(helper, "deploy-fixture-app", "--plan-file", filename)
	launchingAPP.Env = append(os.Environ(), "CAELIS_NATIVE_SUPERVISOR_FIXTURE=1")
	if b, e := launchingAPP.CombinedOutput(); e != nil {
		t.Fatalf("isolated launching APP failed: %v %s", e, b)
	}
	var client *nodebroker.Client
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		client, e = nodebroker.DialUnixForBroker(ctx, p.Broker.Socket, p.NodeID)
		cancel()
		if e == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if e != nil {
		log, _ := os.ReadFile(filepath.Join(dir, "supervisor.log"))
		t.Logf("supervisor output: %s", log)
		t.Fatal("independent broker did not survive launching APP exit", e)
	}
	if _, err := client.Claim(t.Context(), nodeplane.ClaimRequest{BotID: p.Broker.BotID, Target: api.WorkTarget{NodeID: p.NodeID, Backend: "codex", Role: api.RoleBot}, Proof: nodeplane.RuntimeProof{NodeID: p.NodeID, Backend: api.NodeCodex, Epoch: "fabricated", Controllable: true}}); err == nil {
		t.Fatal("broker-only coordinator acquired Runtime lease")
	}
	client.Close()
	if e = SetNodeRoamingSupervisorState(filename, "fixture-approved-disable", "disabling"); e != nil {
		t.Fatal(e)
	}
	// Disabling suppresses restart while preserving the current broker for a
	// final complete snapshot/control confirmation. APP observer detach is inert.
	client, e = nodebroker.DialUnixForBroker(t.Context(), p.Broker.Socket, p.NodeID)
	if e != nil {
		t.Fatal("no-restart intent prematurely stopped broker", e)
	}
	client.Close()
	if e = SetNodeRoamingSupervisorState(filename, "fixture-approved-disable", "disabled"); e != nil {
		t.Fatal(e)
	}
	stopped := false
	for i := 0; i < 100; i++ {
		c, e := net.DialTimeout("unix", p.Broker.Socket, 20*time.Millisecond)
		if e != nil {
			stopped = true
			break
		}
		c.Close()
		time.Sleep(20 * time.Millisecond)
	}
	if !stopped {
		t.Fatal("explicit disabled intent did not stop owned child")
	}
	unlock, e := lockNativeRoamingSupervisor(filepath.Join(dir, "supervisor.lock"))
	if e != nil {
		t.Fatal("independent supervisor remained owned after disable", e)
	}
	unlock()
}

func TestNativeRoamingFrozenInputAndOriginalRecovery(t *testing.T) {
	a := nativeManagementApplication(t)
	helper := filepath.Join(a.root, "reviewed-host")
	if e := os.WriteFile(helper, []byte("fixed native host"), 0700); e != nil {
		t.Fatal(e)
	}
	n := &roamingNativeAssembly{app: a, options: NodeRoamingNativeOptions{Host: func() (string, error) { return helper, nil }}}
	n.original = nativeLocalEnrollmentManagementFixture(t)
	local := NodeRegistration{ID: api.LocalNodeID, Label: "This machine", Join: api.NodeLocal}
	in := NodeRoamingStageInput{BotID: "bot-fixture", OperationID: "original-enable", Coordinator: local, Nodes: []NodeRegistration{local}, SourceTarget: api.WorkTarget{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleBot}}
	summary, e := n.prepare(t.Context(), in)
	if e != nil {
		t.Fatal(e)
	}
	in.ReviewedPlanID, in.AllowPersistentExecution = summary.ID, true
	p := n.plans[summary.ID]
	if !nativeRoamingInputMatches(p, in) {
		t.Fatal("original frozen plan not accepted")
	}
	in.OperationID = "replacement-enable"
	if nativeRoamingInputMatches(p, in) {
		t.Fatal("replacement operation reused approval")
	}
	in.OperationID = p.OperationID
	in.Nodes[0].Label = "changed"
	if nativeRoamingInputMatches(p, in) {
		t.Fatal("changed enrollment reused approval")
	}
	in.Nodes[0].Label = local.Label
	p.Phase = "preparing"
	dir := filepath.Join(a.root, "nodeplane")
	if e = os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e = nodeagent.WriteManagedPrivateJSON(filepath.Join(dir, "roaming-deployment.json"), p); e != nil {
		t.Fatal(e)
	}
	if _, e = n.stage(t.Context(), in); e == nil || !strings.Contains(e.Error(), "without replay") {
		t.Fatal("original unknown deployment was replayed", e)
	}
	recovered, e := n.recover(t.Context(), NodeRoamingRecoveryInput{StageInput: in, StageOperationID: p.OperationID, OperationID: "replacement-enable", OperationKind: "enable"})
	if e == nil || recovered.Outcome != "unknown" {
		t.Fatal("replacement recovery ID accepted", recovered, e)
	}
}

func TestNativeSupervisorDisableIntentIsOriginalAndMonotonic(t *testing.T) {
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	p := NodeRoamingSupervisorPlan{Version: 1, PlanID: strings.Repeat("a", 64), OperationID: "enable-original", NodeID: "node", Helper: "/verified/helper", HelperSHA256: strings.Repeat("b", 64), Directory: dir, Broker: &NodeRoamingBrokerDeployment{BotID: "bot", NodeID: "node", Profile: filepath.Join(dir, "broker"), Socket: filepath.Join(dir, "b.sock"), PeersFile: filepath.Join(dir, "peers.json"), BootstrapPeersFile: filepath.Join(dir, "bootstrap.json")}}
	filename := filepath.Join(dir, "supervisor.json")
	if e := localstate.Write(filename, p); e != nil {
		t.Fatal(e)
	}
	if e := SetNodeRoamingSupervisorState(filename, "disable-original", "disabling"); e != nil {
		t.Fatal(e)
	}
	if state := nativeSupervisorState(p); state != "disabling" {
		t.Fatal(state)
	}
	if e := SetNodeRoamingSupervisorState(filename, "disable-replacement", "disabled"); e == nil {
		t.Fatal("replacement disable accepted")
	}
	if e := SetNodeRoamingSupervisorState(filename, "disable-original", "disabled"); e != nil {
		t.Fatal(e)
	}
	if e := SetNodeRoamingSupervisorState(filename, "disable-original", "disabling"); e == nil {
		t.Fatal("completed disable regressed")
	}
}

func TestDefaultNativeRoamingConstructionNeedsNoHelper(t *testing.T) {
	called := false
	options := DefaultNodeRoamingOptions(nil, NodeRoamingNativeOptions{Host: func() (string, error) { called = true; return "", errors.New("absent") }})
	if called || options.PreparePlan == nil || options.Preflight == nil || options.Stage == nil || options.Recover == nil {
		t.Fatal("default assembly started native prerequisite")
	}
}

func TestDefaultNativeRecoverPreparingCrashRejectsOnlyBeforeRetirement(t *testing.T) {
	a := nativeManagementApplication(t)
	called := false
	options := DefaultNodeRoamingOptions(a, NodeRoamingNativeOptions{Host: func() (string, error) { called = true; return "", errors.New("not required for receipt recovery") }})
	original := NodeRoamingRecoveryInput{OperationID: "original-enable", StageOperationID: "original-enable", OperationKind: "enable", Phase: "preparing", SourceRetiredIntent: false}
	result, e := options.Recover(t.Context(), original)
	if e != nil || result.OperationID != original.OperationID || result.Outcome != "rejected" || result.Phase != "source-active" || called {
		t.Fatal("pre-retirement crash did not confirm original rejection", result, e)
	}
	for _, input := range []NodeRoamingRecoveryInput{
		{OperationID: original.OperationID, StageOperationID: original.StageOperationID, OperationKind: "enable", Phase: "retiring-source", SourceRetiredIntent: true},
		{OperationID: original.OperationID, StageOperationID: original.StageOperationID, OperationKind: "enable", Phase: "staging", SourceRetiredIntent: true},
		{OperationID: "replacement-enable", StageOperationID: original.StageOperationID, OperationKind: "enable", Phase: "preparing"},
	} {
		result, e = options.Recover(t.Context(), input)
		if e == nil || result.Outcome != "unknown" {
			t.Fatal("unconfirmed retirement or replacement ID was rolled back", result, e)
		}
	}
	dir := filepath.Join(a.root, "nodeplane")
	if e = os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "roaming-deployment.json"), []byte("corrupt-record"), 0600); e != nil {
		t.Fatal(e)
	}
	result, e = options.Recover(t.Context(), original)
	if e == nil || result.Outcome != "unknown" {
		t.Fatal("corrupt manifest was treated as absent", result, e)
	}
	if called {
		t.Fatal("receipt recovery started native helper")
	}
}

func TestDefaultNativeRuntimeMetadataReusesExactRetainedPair(t *testing.T) {
	a := nativeManagementApplication(t)
	dir := t.TempDir()
	var e error
	dir, e = filepath.EvalSymlinks(dir)
	if e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(dir, "target-runtime")
	if e := os.WriteFile(binary, []byte("metadata fixture executable"), 0700); e != nil {
		t.Fatal(e)
	}
	store := filepath.Join(dir, "caelis-store")
	service, e := nodeagent.New(nodeagent.Options{Directory: dir, NodeID: "node-target", Binaries: map[api.NodeBackend]string{api.NodeCaelis: binary}, Configurations: map[api.NodeBackend]nodeagent.NativeConfiguration{api.NodeCaelis: &nodeagent.CaelisConfiguration{Settings: api.RuntimeSettings{Runtime: "caelis", CLIPath: binary, CaelisStore: store}}}})
	if e != nil {
		t.Fatal(e)
	}
	reg := NodeRegistration{ID: "node-target", Label: "Target", Join: api.NodeSSH, SSHDestination: "existing-target", Directory: dir, HelperPath: "/fixed/helper"}
	native := &nativeNodeManagement{app: a, document: nodeManagementDocument{Version: 1, Nodes: []NodeRegistration{reg}}, clients: map[string]nodeplane.CatalogAgent{reg.ID: service}}
	a.Backend.SetNodeManagementController(NewNodeManagement(native, native))
	assembly := &roamingNativeAssembly{app: a}
	settings, e := assembly.runtimeSettings(t.Context(), reg, api.NodeCaelis)
	if e != nil || settings.Runtime != "caelis" || settings.CLIPath != binary || settings.CaelisStore != store {
		t.Fatal("retained target metadata mismatch", settings, e)
	}
	if _, e = os.Lstat(store); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("metadata query created target Host configuration", e)
	}
	eligible, reason, e := assembly.ownedRuntimeProbe(t.Context(), reg, api.NodeCaelis)
	expectedReason := "owned-store-setup-required"
	if !codex.OwnedRuntimeSupported() {
		expectedReason = "unsupported-platform"
	}
	if e != nil || eligible || reason != expectedReason {
		t.Fatal("default native pairing inferred owned Host from metadata", eligible, reason, e)
	}
	if _, e = os.Lstat(store); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("default native ownability probe created target store", e)
	}
	reg.ID = "replacement-node"
	if _, e = assembly.runtimeSettings(t.Context(), reg, api.NodeCaelis); e == nil {
		t.Fatal("unregistered replacement target accepted")
	}
}

func TestDefaultNativeCatalogueAfterDisableKeepsTopologyAndNextPlan(t *testing.T) {
	f := roamingControlFixture(t)
	doc := nodeManagementDocument{Version: 1, Revision: 1, Coordinator: api.LocalNodeID, Nodes: []NodeRegistration{}}
	if e := localstate.Write(filepath.Join(f.a.root, "nodeplane", "config.json"), doc); e != nil {
		t.Fatal(e)
	}
	if _, e := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); e != nil {
		t.Fatal(e)
	}
	if _, e := f.a.Backend.DisableNodeRoaming(t.Context(), controlRequest("disable-original")); e != nil {
		t.Fatal(e)
	}
	fresh := f.c.local
	if fresh == nil || fresh == f.a {
		t.Fatal("disable did not install fresh local owner")
	}
	helper := filepath.Join(f.a.root, "reviewed-host")
	if e := os.WriteFile(helper, []byte("reviewed fixture bytes"), 0700); e != nil {
		t.Fatal(e)
	}
	defaults := DefaultNodeRoamingOptions(f.a, NodeRoamingNativeOptions{Host: func() (string, error) { return helper, nil }})
	backendIdentity := f.a.Backend
	if e := AttachNodeManagement(f.a, *defaults.RefreshNodeManagement); e != nil {
		t.Fatal(e)
	}
	if e := f.a.Backend.ConfigureNodeRoaming(f.c); e != nil {
		t.Fatal(e)
	}
	catalog, e := f.a.Backend.NodeCatalog(t.Context())
	if e != nil || len(catalog.Nodes) != 1 || catalog.Nodes[0].ID != api.LocalNodeID || catalog.ActiveBotNodeID != api.LocalNodeID || catalog.Broker == nil || catalog.Broker.NodeID != api.LocalNodeID {
		t.Fatal("default restored catalogue lost topology", catalog, e)
	}
	if f.a.Backend != backendIdentity || ActiveNodeRoamingApplication(f.a) != fresh {
		t.Fatal("stable facade or actual fresh native owner changed")
	}
	plan, e := defaults.PreparePlan(t.Context(), NodeRoamingStageInput{BotID: "bot-fixture", OperationID: "enable-next", Coordinator: NodeRegistration{ID: api.LocalNodeID, Label: "This machine", Join: api.NodeLocal}, Nodes: []NodeRegistration{{ID: api.LocalNodeID, Label: "This machine", Join: api.NodeLocal}}, SourceTarget: api.WorkTarget{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleBot}})
	if e != nil || plan.ID == "" {
		t.Fatal("next default plan cannot be prepared", plan, e)
	}
	if f.local.submits.Load() != 0 {
		t.Fatal("retired original source serviced restored catalogue")
	}
}

func TestDefaultNativeCatalogueReconnectsExactStagePeerAfterDisable(t *testing.T) {
	f := roamingControlFixture(t)
	if _, e := f.a.Backend.EnableNodeRoaming(t.Context(), controlRequest("enable-original")); e != nil {
		t.Fatal(e)
	}
	defaults := DefaultNodeRoamingOptions(f.a)
	assembly := defaults.RefreshNodeManagement.LocalAgent.(roamingManagedCatalog).assembly
	remote := singleNodeFixture("node-fixture")
	remote.catalog.Revision = strings.Repeat("a", 64)
	local := singleNodeFixture(api.LocalNodeID)
	local.catalog.Revision = strings.Repeat("b", 64)
	for _, fixture := range []*nodeManagementFixture{local, remote} {
		fixture.catalog.Nodes[0].Runtimes = append(fixture.catalog.Nodes[0].Runtimes, api.NodeRuntime{Backend: api.NodeCodex, Authentication: api.NodeAuthenticated, Health: api.NodeHealthy, Roles: []api.NodeRoleCapability{{Role: api.RoleBot, Eligible: true}, {Role: api.RoleWorker, Eligible: true}}})
	}
	dial := func(id string, agent nodeplane.CatalogAgent) *nodeagent.Client {
		left, right := net.Pipe()
		go func() {
			defer right.Close()
			_ = productrpc.ServeNativeStream(t.Context(), right, right, nodeagent.Handler(agent), func(method, path string) bool { return method == http.MethodGet && path == "/v1/node/catalog" })
		}()
		client, e := nodeagent.NewClient(id, left)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = client.Close() })
		return client
	}
	peer := dial("node-fixture", remote)
	dir, e := os.MkdirTemp("/tmp", "nr-peer-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	dir, e = filepath.EvalSymlinks(dir)
	if e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("unix", filepath.Join(dir, "broker.sock"))
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	if e = os.Chmod(filepath.Join(dir, "broker.sock"), 0600); e != nil {
		t.Fatal(e)
	}
	broker, e := nodebroker.DialUnix(filepath.Join(dir, "broker.sock"))
	if e != nil {
		t.Fatal(e)
	}
	session := &roamingNativeSession{assembly: assembly, broker: broker, peers: map[string]*nodeagent.Client{api.LocalNodeID: dial(api.LocalNodeID, local)}, management: map[string]*nodeagent.Client{"node-fixture": peer}}
	assembly.sessions[broker] = session
	f.c.stage.Close = session.close
	refresh := *defaults.RefreshNodeManagement
	// Keep the real default catalogue/local-generation wrapper, substituting
	// only the existing SSH stream transport with contained native IPC.
	fallbacks := 0
	refresh.Dial = func(ctx context.Context, reg NodeRegistration) (nodeplane.CatalogAgent, error) {
		if existing, e := assembly.managementPeer(ctx, reg.ID); e == nil {
			return existing, nil
		}
		if assembly.freshLocal() == nil || reg.ID != "node-fixture" || reg.SSHDestination != "fixture-target" {
			return nil, errors.New("original enrolled fresh-local route unavailable")
		}
		fallbacks++
		return dial(reg.ID, remote), nil
	}
	// Broker liveness is exercised separately; this fixture's socket is only
	// the stage observer being detached.
	refresh.ExecutionState = func() (string, *api.WorkTarget) {
		if assembly.freshLocal() != nil {
			return api.LocalNodeID, nil
		}
		return "node-fixture", nil
	}
	refresh.BrokerStatus = nil
	if e = AttachNodeManagement(f.a, refresh); e != nil {
		t.Fatal(e)
	}
	if e = f.a.Backend.ConfigureNodeRoaming(f.c); e != nil {
		t.Fatal(e)
	}
	catalog, e := f.a.Backend.NodeCatalog(t.Context())
	if e != nil || len(catalog.Nodes) != 2 || len(catalog.Nodes[1].Runtimes) == 0 || fallbacks != 0 {
		t.Fatal("active catalogue did not cache original paired peer", catalog, e, fallbacks)
	}
	controller, _ := backend.NativeNodeManagementController(f.a.Backend)
	native := controller.(*nodeManagement).agent.(*nativeNodeManagement)
	if native.clients["node-fixture"] != peer {
		t.Fatal("fixture did not cache the exact stage client")
	}
	request := controlRequest("disable-original")
	request.ExpectedCatalogRevision = catalog.Revision
	if _, e = f.a.Backend.DisableNodeRoaming(t.Context(), request); e != nil {
		t.Fatal(e)
	}
	if _, e = peer.Catalog(t.Context()); e == nil {
		t.Fatal("stage detach left original observation client open")
	}
	catalog, e = f.a.Backend.NodeCatalog(t.Context())
	if e != nil || len(catalog.Nodes) != 2 || len(catalog.Nodes[1].Runtimes) == 0 || fallbacks != 1 || native.clients["node-fixture"] == peer {
		t.Fatal("fresh local catalogue did not reconnect exact enrollment", catalog, e, fallbacks)
	}
	request = controlRequest("enable-next")
	request.ExpectedCatalogRevision = catalog.Revision
	state, e := f.a.Backend.EnableNodeRoaming(t.Context(), request)
	if e != nil || !state.Enabled || f.staged.Load() != 2 {
		t.Fatal("next enable could not use reconnected remote catalogue", state, e)
	}
}

type nativeRecoveryProofFunc func(context.Context, api.WorkTarget) (nodeplane.RuntimeEligibility, error)

func (f nativeRecoveryProofFunc) ReadRuntimeProof(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	return f(ctx, target)
}

func TestDefaultNativeRecoverDisableBeforeDispatchObservesOriginalStage(t *testing.T) {
	dir, e := os.MkdirTemp("/tmp", "nr-recover-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	dir, e = filepath.EvalSymlinks(dir)
	if e != nil {
		t.Fatal(e)
	}
	a := nativeManagementApplication(t)
	a.root = filepath.Join(dir, "app")
	source := filepath.Join(dir, "source")
	if e = os.MkdirAll(filepath.Join(source, "Notebook"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = localstate.Write(filepath.Join(source, "bot.json"), map[string]any{"version": 1, "personalVersion": 1, "id": "bot-fixture", "schedules": []any{}}); e != nil {
		t.Fatal(e)
	}
	if e = localstate.Write(filepath.Join(source, "bot-initialization.json"), map[string]any{"version": 1, "id": "intro-original", "status": "accepted", "runtime": "codex"}); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "Notebook", "MEMORY.md"), []byte("# Complete fixture\n"), 0600); e != nil {
		t.Fatal(e)
	}
	payload, seed, e := memorytransfer.ExportNotebook(t.Context(), memorytransfer.NotebookExportOptions{Source: source, SourceStopped: true, Epoch: "0", Version: "1"})
	if e != nil {
		t.Fatal(e)
	}
	owned := filepath.Join(dir, "owned")
	coordinator, e := nodecoord.Open(nodecoord.Options{Directory: filepath.Join(owned, "broker"), BrokerNodeID: api.LocalNodeID, BotID: seed.BotID, ValidateSnapshot: memorytransfer.ValidateNotebookPayload, Verify: func(context.Context, nodeplane.ClaimRequest) error { return nil }})
	if e != nil {
		t.Fatal(e)
	}
	defer coordinator.Close()
	if e = coordinator.SeedSnapshot(t.Context(), seed, payload); e != nil {
		t.Fatal(e)
	}
	target := api.WorkTarget{NodeID: api.LocalNodeID, Backend: "codex", Role: api.RoleBot}
	lease, e := coordinator.Claim(t.Context(), nodeplane.ClaimRequest{BotID: seed.BotID, Target: target, Snapshot: seed, Proof: nodeplane.RuntimeProof{NodeID: api.LocalNodeID, Backend: api.NodeCodex, Epoch: "native-original", Controllable: true}})
	if e != nil {
		t.Fatal(e)
	}
	payload, ref, e := memorytransfer.ExportNotebook(t.Context(), memorytransfer.NotebookExportOptions{Source: source, SourceStopped: true, Epoch: lease.Epoch, Version: "2"})
	if e != nil {
		t.Fatal(e)
	}
	if e = coordinator.PublishSnapshot(t.Context(), lease, ref, payload); e != nil {
		t.Fatal(e)
	}
	brokerSocket := filepath.Join(owned, "broker.sock")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan struct{})
	brokerDone := make(chan error, 1)
	go func() { brokerDone <- nodebroker.ServeUnix(ctx, brokerSocket, coordinator, func() { close(ready) }) }()
	select {
	case <-ready:
	case e := <-brokerDone:
		t.Fatal(e)
	case <-time.After(3 * time.Second):
		t.Fatal("contained broker readiness exceeded")
	}
	unknown := false
	if e = os.MkdirAll(filepath.Join(owned, "agent"), 0700); e != nil {
		t.Fatal(e)
	}
	agent, e := nodeagent.New(nodeagent.Options{Directory: filepath.Join(owned, "agent"), NodeID: api.LocalNodeID, RuntimeOwner: nativeRecoveryProofFunc(func(_ context.Context, got api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
		if got != target {
			return nodeplane.RuntimeEligibility{}, errors.New("owner scope changed")
		}
		return nodeplane.RuntimeEligibility{Proof: nodeplane.RuntimeProof{NodeID: api.LocalNodeID, Backend: api.NodeCodex, Epoch: "native-original", Controllable: true}, Snapshot: ref, LeaseEpoch: lease.Epoch, Pending: true, Unknown: unknown}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	agentSocket := filepath.Join(owned, "agent.sock")
	listener, e := net.Listen("unix", agentSocket)
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	if e = os.Chmod(agentSocket, 0600); e != nil {
		t.Fatal(e)
	}
	go func() { _ = nodeagent.Serve(ctx, listener, agent) }()
	local := NodeRegistration{ID: api.LocalNodeID, Label: "This machine", Join: api.NodeLocal}
	id := strings.Repeat("a", 64)
	plan := NodeRoamingSupervisorPlan{Version: 1, PlanID: id, OperationID: "enable-original", NodeID: api.LocalNodeID, Helper: "/fixed/verified-helper", HelperSHA256: strings.Repeat("b", 64), Directory: owned, Broker: &NodeRoamingBrokerDeployment{BotID: ref.BotID, NodeID: api.LocalNodeID, Profile: filepath.Join(owned, "broker"), Socket: brokerSocket, PeersFile: filepath.Join(owned, "peers.json"), BootstrapPeersFile: filepath.Join(owned, "bootstrap.json")}, Managed: &NodeRoamingManagedDeployment{BotID: ref.BotID, NodeID: api.LocalNodeID, Backend: "codex", AgentDirectory: filepath.Join(owned, "agent"), GenerationRoot: filepath.Join(owned, "generations"), AuthFile: filepath.Join(owned, "token"), BrokerNodeID: api.LocalNodeID, BrokerSocket: brokerSocket}}
	if e = nodeagent.WriteManagedPrivateJSON(filepath.Join(owned, "supervisor.json"), plan); e != nil {
		t.Fatal(e)
	}
	saved := roamingNativePlan{ID: id, OperationID: plan.OperationID, BotID: ref.BotID, SourceNodeID: api.LocalNodeID, SourceBackend: "codex", Coordinator: local, Enrollment: []NodeRegistration{local}, Phase: "owners-ready", Nodes: []roamingNativeNode{{Registration: local, Plan: plan, HostHelper: plan.Helper, AgentSocket: agentSocket}}}
	sealNativeRecoveryFixture(t, &saved)
	id, plan = saved.ID, saved.Nodes[0].Plan
	if e = nodeagent.WriteManagedPrivateJSON(filepath.Join(owned, "supervisor.json"), plan); e != nil {
		t.Fatal(e)
	}
	manifest := filepath.Join(a.root, "nodeplane", "roaming-deployment.json")
	if e = os.MkdirAll(filepath.Dir(manifest), 0700); e != nil {
		t.Fatal(e)
	}
	if e = nodeagent.WriteManagedPrivateJSON(manifest, saved); e != nil {
		t.Fatal(e)
	}
	original, _ := os.ReadFile(manifest)
	input := NodeRoamingRecoveryInput{OperationID: "disable-original", StageOperationID: saved.OperationID, OperationKind: "disable", Phase: "quiescing", SourceRetiredIntent: true, StageInput: NodeRoamingStageInput{ReviewedPlanID: id, BotID: ref.BotID, Coordinator: local, Nodes: []NodeRegistration{local}, SourceTarget: target, AllowPersistentExecution: true}}
	options := DefaultNodeRoamingOptions(a)
	for _, phase := range []string{"quiescing", "active"} {
		input.Phase = phase
		result, e := options.Recover(t.Context(), input)
		if e != nil || result.OperationID != input.OperationID || result.Outcome != "rejected" || result.Phase != "active" || !validRoamingStage(result.Stage) || result.Local != nil {
			t.Fatal("original pre-dispatch disable was not read-only rejected", result, e)
		}
		result.Stage.Close()
	}
	after, _ := os.ReadFile(manifest)
	if string(after) != string(original) {
		t.Fatal("recovery rewrote native execution intent")
	}
	if _, e = os.Lstat(filepath.Join(owned, "deployment-state.json")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("recovery dispatched a supervisor mutation", e)
	}
	unknown = true
	result, e := options.Recover(t.Context(), input)
	if e == nil || result.Outcome != "unknown" || validRoamingStage(result.Stage) {
		t.Fatal("unconfirmed actual owner was adopted", result, e)
	}
	unknown = false
	saved.DisableOperationID = input.OperationID
	if e = nodeagent.WriteManagedPrivateJSON(manifest, saved); e != nil {
		t.Fatal(e)
	}
	result, e = options.Recover(t.Context(), input)
	if e == nil || result.Outcome != "unknown" {
		t.Fatal("recorded native disable was treated as undispatched", result, e)
	}
}

func TestDefaultNativeDisableRecoveryFromPreparedColdGeneration(t *testing.T) {
	a := nativeManagementApplication(t)
	if e := os.Chmod(a.root, 0700); e != nil {
		t.Fatal(e)
	}
	var e error
	a.root, e = filepath.EvalSymlinks(a.root)
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(a.root, "cold-source")
	if e = os.MkdirAll(filepath.Join(source, "Notebook"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = localstate.Write(filepath.Join(source, "bot.json"), map[string]any{"version": 1, "personalVersion": 1, "id": "bot-fixture", "schedules": []any{}}); e != nil {
		t.Fatal(e)
	}
	if e = localstate.Write(filepath.Join(source, "bot-initialization.json"), map[string]any{"version": 1, "id": "intro-original", "status": "accepted", "runtime": "codex"}); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "Notebook", "MEMORY.md"), []byte("# Memory\n\nComplete stopped fixture.\n"), 0600); e != nil {
		t.Fatal(e)
	}
	payload, ref, e := memorytransfer.ExportNotebook(t.Context(), memorytransfer.NotebookExportOptions{Source: source, SourceStopped: true, Epoch: "1", Version: "2"})
	if e != nil {
		t.Fatal(e)
	}
	restore := filepath.Join(a.root, "nodeplane", "local-restores", "generation-fixture")
	if e = os.MkdirAll(filepath.Dir(restore), 0700); e != nil {
		t.Fatal(e)
	}
	applied, e := memorytransfer.ApplyNotebook(t.Context(), memorytransfer.NotebookApplyOptions{Payload: payload, Destination: restore, DestinationStopped: true, Expected: ref, Commit: func(_ context.Context, r nodeplane.SnapshotRef, install func() error) error {
		if r != ref {
			return errors.New("wrong cold descriptor")
		}
		return install()
	}})
	if e != nil || !applied.Activated {
		t.Fatal(applied, e)
	}
	if e = localstate.Write(filepath.Join(restore, "runtime.json"), api.RuntimeSettings{Runtime: "codex"}); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(a.root, "nodeplane", "approved-deployment")
	if e = os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	id := strings.Repeat("c", 64)
	plan := NodeRoamingSupervisorPlan{Version: 1, PlanID: id, OperationID: "enable-original", NodeID: api.LocalNodeID, Helper: "/fixed/verified-helper", HelperSHA256: strings.Repeat("d", 64), Directory: dir, Managed: &NodeRoamingManagedDeployment{BotID: ref.BotID, NodeID: api.LocalNodeID, Backend: "codex", AgentDirectory: filepath.Join(dir, "agent"), GenerationRoot: filepath.Join(dir, "generations"), AuthFile: filepath.Join(dir, "token"), BrokerNodeID: api.LocalNodeID, BrokerSocket: filepath.Join(dir, "broker.sock")}}
	filename := filepath.Join(dir, "supervisor.json")
	if e = localstate.Write(filename, plan); e != nil {
		t.Fatal(e)
	}
	local := NodeRegistration{ID: api.LocalNodeID, Label: "This machine", Join: api.NodeLocal}
	saved := roamingNativePlan{ID: id, OperationID: plan.OperationID, BotID: ref.BotID, SourceNodeID: api.LocalNodeID, SourceBackend: "codex", Coordinator: local, Enrollment: []NodeRegistration{local}, Nodes: []roamingNativeNode{{Registration: local, Plan: plan, HostHelper: plan.Helper}}, Phase: "local-prepared", RestoreDirectory: restore, RestoredSnapshot: ref, DisableOperationID: "disable-original", DisableLease: nodeplane.Lease{BotID: ref.BotID, NodeID: api.LocalNodeID, Backend: api.NodeCodex, Epoch: ref.Epoch}}
	sealNativeRecoveryFixture(t, &saved)
	id, plan = saved.ID, saved.Nodes[0].Plan
	if e = localstate.Write(filename, plan); e != nil {
		t.Fatal(e)
	}
	if e = nodeagent.WriteManagedPrivateJSON(filepath.Join(a.root, "nodeplane", "roaming-deployment.json"), saved); e != nil {
		t.Fatal(e)
	}
	options := DefaultNodeRoamingOptions(a)
	input := NodeRoamingRecoveryInput{OperationID: saved.DisableOperationID, StageOperationID: saved.OperationID, OperationKind: "disable", SourceRetiredIntent: true, Phase: "restoring-local", StageInput: NodeRoamingStageInput{ReviewedPlanID: id, BotID: ref.BotID, Coordinator: local}}
	result, e := options.Recover(t.Context(), input)
	if e == nil || result.Outcome != "unknown" || result.Local != nil {
		t.Fatal("missing no-restart marker permitted fresh local assembly", result, e)
	}
	if e = SetNodeRoamingSupervisorState(filename, saved.DisableOperationID, "disabling"); e != nil {
		t.Fatal(e)
	}
	result, e = options.Recover(t.Context(), input)
	if e != nil || result.Outcome != "accepted" || result.Phase != "local" || result.Local == nil {
		t.Fatal("confirmed cold generation not recovered", result, e)
	}
	defer result.Local.Close()
	result.Local.mu.Lock()
	started := result.Local.started
	result.Local.mu.Unlock()
	if started {
		t.Fatal("recovery started local work before controller receipt")
	}
	marker, e := ReadNodeRoamingSupervisorState(filename, saved.DisableOperationID)
	if e != nil || marker.State != "disabled" {
		t.Fatal("original supervisor intent not finalized", marker, e)
	}
	final, e := readNativeRoamingPlan(filepath.Join(a.root, "nodeplane", "roaming-deployment.json"))
	if e != nil || final.Phase != "restored-local" || final.RestoreDirectory != restore || final.RestoredSnapshot != ref {
		t.Fatal("recovered cold receipt changed", final, e)
	}
	result.Local.Close()
	current := api.RuntimeSettings{Runtime: "codex", CLIPath: "/current/native/codex"}
	if e = localstate.Write(filepath.Join(restore, "runtime.json"), current); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(restore, "Notebook", "MEMORY.md"), []byte("# Edited local memory\n"), 0600); e != nil {
		t.Fatal(e)
	}
	reenable := NodeRoamingRecoveryInput{OperationID: "enable-next", StageOperationID: "enable-next", OperationKind: "enable", Phase: "preparing", SourceRetiredIntent: false, LocalGenerationDirectory: restore, StageInput: NodeRoamingStageInput{ReviewedPlanID: strings.Repeat("e", 64), Coordinator: local}}
	result, e = options.Recover(t.Context(), reenable)
	if e != nil || result.Outcome != "rejected" || result.Phase != "source-active" || result.Local == nil || result.Local == a || result.Local.root != restore || result.Local.Backend.RuntimeSettings() != current {
		t.Fatal("re-enable crash did not preserve actual current source", result, e)
	}
	defer result.Local.Close()
	reenable.Phase = "local"
	recoveredAgain, e := options.Recover(t.Context(), reenable)
	if e != nil || recoveredAgain.Outcome != "rejected" || recoveredAgain.Local == nil || recoveredAgain.Local.root != restore {
		t.Fatal("durable rejected fresh-local source failed next restart", recoveredAgain, e)
	}
	defer recoveredAgain.Local.Close()
	currentBody, _ := os.ReadFile(filepath.Join(restore, "Notebook", "MEMORY.md"))
	if string(currentBody) != "# Edited local memory\n" {
		t.Fatal("old Notebook bytes replaced current local source")
	}
}

func TestNativeWorkerRosterPreservesBothBackendsAndSameNodeAlternative(t *testing.T) {
	bindings := []NodeRoamingWorkerRuntime{{Backend: "codex", Binary: "/native/codex", Execution: api.WorkExecutionSettings{Model: "target-codex"}}, {Backend: "caelis", Binary: "/native/caelis", Store: "/native/owned-store", Model: "target-caelis", Execution: api.WorkExecutionSettings{Model: "target-caelis"}}}
	local := roamingNativeNode{Registration: NodeRegistration{ID: api.LocalNodeID, Label: "This machine"}, Plan: NodeRoamingSupervisorPlan{Managed: &NodeRoamingManagedDeployment{Backend: "codex"}}, RuntimeBindings: bindings, BrokerPeerSocket: "/private/approved/local.sock"}
	remote := roamingNativeNode{Registration: NodeRegistration{ID: "node-target", Label: "Target"}, Plan: NodeRoamingSupervisorPlan{Managed: &NodeRoamingManagedDeployment{Backend: "caelis"}}, RuntimeBindings: bindings, BrokerPeerSocket: "/private/approved/target.sock"}
	wire, e := json.Marshal(nativeRoamingWorkers(roamingNativePlan{Nodes: []roamingNativeNode{local, remote}}, local))
	if e != nil {
		t.Fatal(e)
	}
	var result struct {
		Nodes   []map[string]any                           `json:"nodes"`
		Agents  []struct{ NodeID, Backend, Socket string } `json:"agents"`
		Sources []struct {
			NodeID   string
			Backends []string
		} `json:"sources"`
		Runtimes []NodeRoamingWorkerRuntime `json:"runtimes"`
	}
	if e = json.Unmarshal(wire, &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Nodes) != 3 || len(result.Agents) != 3 || len(result.Runtimes) != 2 || len(result.Sources) != 2 {
		t.Fatal("runtime alternative lost", string(wire))
	}
	seen := map[string]bool{}
	for _, node := range result.Nodes {
		if len(node) != 4 || node["transport"] != "registered-agent" {
			t.Fatal("private native paths escaped Worker metadata", node)
		}
		seen[node["id"].(string)+"/"+node["backend"].(string)] = true
	}
	if !seen["local/caelis"] || !seen["node-target/codex"] || !seen["node-target/caelis"] || seen["local/codex"] {
		t.Fatal("Worker target is not exact source-independent backend", seen)
	}
	if result.Runtimes[1].Store != "/native/owned-store" || result.Runtimes[0].Execution.Model != "target-codex" {
		t.Fatal("target-native settings not retained", result.Runtimes)
	}
}

func TestNativeCaelisCandidateRequiresOwnedStoreProbeBeforeRetirement(t *testing.T) {
	registration := NodeRegistration{ID: "node-target", Label: "Target", Join: api.NodeSSH}
	settings := api.RuntimeSettings{Runtime: "caelis", CLIPath: "/native/target/caelis", CaelisStore: "/native/target/owned-store"}
	node := roamingNativeNode{Registration: registration, Plan: NodeRoamingSupervisorPlan{Managed: &NodeRoamingManagedDeployment{Backend: "caelis", CaelisBinary: settings.CLIPath, CaelisStore: settings.CaelisStore}}}
	eligible := false
	probes := 0
	assembly := &roamingNativeAssembly{options: NodeRoamingNativeOptions{OwnedRuntimeProbe: func(_ context.Context, reg NodeRegistration, b api.NodeBackend) (bool, string, error) {
		probes++
		if reg != registration || b != api.NodeCaelis {
			t.Fatal("candidate probe lost target pairing")
		}
		return eligible, "shared-host-store", nil
	}, RuntimeSettings: func(context.Context, NodeRegistration, api.NodeBackend) (api.RuntimeSettings, error) {
		return settings, nil
	}}}
	if e := assembly.confirmCandidateRuntime(t.Context(), node); e == nil || !strings.Contains(e.Error(), "shared-host-store") {
		t.Fatal("authenticated installed metadata bypassed owned Host gate", e)
	}
	eligible = true
	if e := assembly.confirmCandidateRuntime(t.Context(), node); e != nil {
		t.Fatal("designated owned target was refused", e)
	}
	settings.CaelisStore = "/changed/target/store"
	if e := assembly.confirmCandidateRuntime(t.Context(), node); e == nil {
		t.Fatal("probe for changed target slot authorized frozen deployment")
	}
	node.Plan.Managed.Backend = "codex"
	if e := assembly.confirmCandidateRuntime(t.Context(), node); e != nil || probes != 3 {
		t.Fatal("Caelis alternative gated primary Codex candidate", e, probes)
	}
}

func TestNativeWorkerBindingsOmitUnavailableCaelisAndStaleCodexStore(t *testing.T) {
	a := nativeManagementApplication(t)
	agent := singleNodeFixture(api.LocalNodeID)
	agent.catalog.Nodes[0].Runtimes = append(agent.catalog.Nodes[0].Runtimes, api.NodeRuntime{Backend: api.NodeCodex, Authentication: api.NodeAuthenticated, Health: api.NodeHealthy})
	a.Backend.SetNodeManagementController(NewNodeManagement(agent, nil))
	reads := 0
	assembly := &roamingNativeAssembly{app: a, options: NodeRoamingNativeOptions{OwnedRuntimeProbe: func(context.Context, NodeRegistration, api.NodeBackend) (bool, string, error) {
		return false, "shared-host-store", nil
	}, RuntimeSettings: func(_ context.Context, _ NodeRegistration, b api.NodeBackend) (api.RuntimeSettings, error) {
		reads++
		return api.RuntimeSettings{Runtime: string(b), CLIPath: "/native/" + string(b), CaelisStore: "/stale/cross-backend/store"}, nil
	}}}
	bindings := assembly.workerBindings(t.Context(), roamingNativePlan{SourceBackend: "codex", LocalWorkExecution: api.WorkExecutionSettings{Model: "local-configured-worker"}}, NodeRegistration{ID: api.LocalNodeID}, &NodeRoamingManagedDeployment{Backend: "codex"})
	if len(bindings) != 1 || bindings[0].Backend != "codex" || bindings[0].Store != "" || bindings[0].Execution.Model != "local-configured-worker" || reads != 1 {
		t.Fatal("unowned alternative or stale cross-backend native settings escaped", bindings, reads)
	}
}

func TestDefaultNativeLocalCaelisProbeUsesActualProfileStore(t *testing.T) {
	store, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(store, 0700); e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(store, ".caelis-bot-node-owner.json")
	if e = localstate.Write(marker, map[string]string{"nodeId": api.LocalNodeID}); e != nil {
		t.Fatal(e)
	}
	a := nativeManagementApplication(t)
	options := DefaultNodeRoamingOptions(a, NodeRoamingNativeOptions{RuntimeSettings: func(_ context.Context, reg NodeRegistration, b api.NodeBackend) (api.RuntimeSettings, error) {
		if reg.ID != api.LocalNodeID || b != api.NodeCaelis {
			t.Fatal("local native profile probe lost scope")
		}
		return api.RuntimeSettings{Runtime: "caelis", CLIPath: "/native/local/caelis", CaelisStore: store}, nil
	}})
	assembly := options.RefreshNodeManagement.LocalAgent.(roamingManagedCatalog).assembly
	eligible, reason, e := assembly.ownedRuntimeProbe(t.Context(), NodeRegistration{ID: api.LocalNodeID}, api.NodeCaelis)
	if e != nil || !eligible || reason != "" {
		t.Fatal("actual marked local Store was replaced by generic agent fallback", eligible, reason, e)
	}
	if e = localstate.Write(marker, map[string]string{"nodeId": "different-node"}); e != nil {
		t.Fatal(e)
	}
	eligible, reason, e = assembly.ownedRuntimeProbe(t.Context(), NodeRegistration{ID: api.LocalNodeID}, api.NodeCaelis)
	if e != nil || eligible || reason != "owned-store-node-mismatch" {
		t.Fatal("foreign local Store marker was adopted", eligible, reason, e)
	}
	files, e := os.ReadDir(store)
	if e != nil || len(files) != 1 || files[0].Name() != filepath.Base(marker) {
		t.Fatal("read-only local probe initialized native Host state", files, e)
	}
}
