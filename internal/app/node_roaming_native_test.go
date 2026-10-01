package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodebroker"
	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
)

func TestNativeRoamingPlanBindsApprovalAndChangesWithoutEffects(t *testing.T) {
	a := nativeManagementApplication(t)
	helper := filepath.Join(a.root, "reviewed-host")
	if e := os.WriteFile(helper, []byte("reviewed fixture bytes"), 0700); e != nil {
		t.Fatal(e)
	}
	n := &roamingNativeAssembly{app: a, options: NodeRoamingNativeOptions{Host: func() (string, error) { return helper, nil }}}
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
	p := NodeRoamingSupervisorPlan{Version: 1, PlanID: strings.Repeat("c", 64), OperationID: "fixture-approved-enable", NodeID: api.LocalNodeID, Helper: helper, HelperSHA256: digest, Directory: dir, Broker: &NodeRoamingBrokerDeployment{BotID: "bot-fixture", NodeID: api.LocalNodeID, Profile: filepath.Join(dir, "broker"), Socket: filepath.Join(dir, "b.sock"), PeersFile: filepath.Join(dir, "peers.json"), BootstrapPeersFile: filepath.Join(dir, "bootstrap.json"), PreferredNodeID: api.LocalNodeID}}
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
		client, e = nodebroker.DialUnixForBroker(ctx, p.Broker.Socket, api.LocalNodeID)
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
	client.Close()
	if e = SetNodeRoamingSupervisorState(filename, "fixture-approved-disable", "disabling"); e != nil {
		t.Fatal(e)
	}
	// Disabling suppresses restart while preserving the current broker for a
	// final complete snapshot/control confirmation. APP observer detach is inert.
	client, e = nodebroker.DialUnixForBroker(t.Context(), p.Broker.Socket, api.LocalNodeID)
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
