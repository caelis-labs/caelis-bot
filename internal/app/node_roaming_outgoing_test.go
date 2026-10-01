package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/localstate"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

// These contained children execute the actual joined deployment and production
// supervisor. The child agent proves IPC/lifetime only; it is not a live Bot.
func init() {
	if os.Getenv("CAELIS_JOINED_SUPERVISOR_FIXTURE") != "1" || len(os.Args) < 2 {
		return
	}
	mode := os.Args[1]
	if mode != "deploy-joined-roaming" && mode != "supervise-roaming" && mode != "serve-roaming" {
		return
	}
	flag := func(name string) string {
		for i, value := range os.Args {
			if value == name && i+1 < len(os.Args) {
				return os.Args[i+1]
			}
		}
		return ""
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	signal.Ignore(syscall.SIGHUP)
	var e error
	switch mode {
	case "deploy-joined-roaming":
		var r nodeagent.RoamingDeploymentRequest
		if e = json.NewDecoder(os.Stdin).Decode(&r); e == nil {
			var result nodeagent.RoamingDeploymentReceipt
			result, e = ExecuteJoinedRoamingDeployment(ctx, flag("--directory"), flag("--node-id"), r)
			if e == nil {
				e = json.NewEncoder(os.Stdout).Encode(result)
			}
		}
	case "supervise-roaming":
		e = RunNodeRoamingSupervisor(ctx, flag("--plan-file"))
	case "serve-roaming":
		directory := flag("--agent-directory")
		var service *nodeagent.Service
		service, e = nodeagent.New(nodeagent.Options{Directory: directory, NodeID: flag("--node-id"), Join: api.NodeOutgoing})
		if e == nil {
			var listener net.Listener
			listener, e = net.Listen("unix", filepath.Join(directory, "agent.sock"))
			if e == nil {
				e = os.Chmod(filepath.Join(directory, "agent.sock"), 0600)
				if e == nil {
					e = nodeagent.Serve(ctx, listener, service)
				}
				listener.Close()
			}
		}
	}
	if e != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestJoinedDeploymentUsesPairedAgentAndDetachedProductionSupervisor(t *testing.T) {
	directory, e := os.MkdirTemp("/tmp", "caelis-jd-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(directory)
	t.Setenv("CAELIS_JOINED_SUPERVISOR_FIXTURE", "1")
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	bytes, e := os.ReadFile(executable)
	if e != nil {
		t.Fatal(e)
	}
	helper := filepath.Join(directory, "caelis-node")
	if e = os.WriteFile(helper, bytes, 0700); e != nil {
		t.Fatal(e)
	}
	bins := filepath.Join(directory, "bin")
	if e = os.Mkdir(bins, 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", bins+string(os.PathListSeparator)+os.Getenv("PATH"))
	service, e := nodeagent.New(nodeagent.Options{Directory: directory, NodeID: "nat-node", Join: api.NodeOutgoing, Binaries: map[api.NodeBackend]string{api.NodeCodex: "/usr/bin/true"}})
	if e != nil {
		t.Fatal(e)
	}
	route := nodeagent.OutgoingRoute{Target: "existing-coordinator", Helper: "/existing/caelis-agent", Directory: "/private/joins/nat-node"}
	if e = localstate.Write(filepath.Join(directory, "outgoing-route.json"), route); e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("unix", filepath.Join(directory, "agent.sock"))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(filepath.Join(directory, "agent.sock"), 0600); e != nil {
		t.Fatal(e)
	}
	life, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = nodeagent.Serve(life, listener, service) }()
	defer func() { cancel(); <-done }()
	client, e := nodeagent.Dial(t.Context(), filepath.Join(directory, "agent.sock"), "nat-node")
	if e != nil {
		t.Fatal(e)
	}
	metadata, e := client.RoamingDeployment(t.Context(), nodeagent.RoamingDeploymentRequest{NodeID: "nat-node", Action: "metadata"})
	if e != nil {
		t.Fatal(e)
	}
	op := "original-enable"
	slot := filepath.Join(directory, "roaming-"+nativeRoamingKey(op))
	planID := strings.Repeat("a", 64)
	m := &NodeRoamingManagedDeployment{BotID: "fixture-bot", NodeID: "nat-node", Backend: "codex", CodexBinary: "/usr/bin/true", AgentDirectory: filepath.Join(slot, "agent"), GenerationRoot: filepath.Join(slot, "generations"), AuthFile: filepath.Join(slot, "product.token"), WorkersFile: filepath.Join(slot, "workers.json"), BrokerNodeID: "coordinator", BrokerSocket: "/private/broker.sock", BrokerSSHDestination: route.Target, BrokerHelper: "/existing/caelis-node", JoinSSHDestination: route.Target, JoinHelper: route.Helper, JoinDirectory: "/private/roaming-joins/nat-node"}
	// This paired plan freezes the existing coordinator enrollment separately
	// from its reverse-join directory. The isolated SSH fixture owns transport.
	m.CoordinatorIdentity = &nodeagent.NativeEnrollmentIdentity{NodeID: "coordinator", Directory: "/private/coordinator-enrollment"}
	// Accept only the closed identity verification for this original route.
	// The contained fixture never contacts or adopts a real coordinator.
	verification := "'/existing/caelis-agent' verify-join-directory --directory '/private/coordinator-enrollment' --node-id 'coordinator'"
	ssh := "#!/bin/sh\nprevious=\nlast=\nfor arg in \"$@\"; do previous=$last; last=$arg; done\n[ \"$previous\" = 'existing-coordinator' ] || exit 91\n[ \"$last\" = " + nodeShellQuote(verification) + " ] || exit 92\n"
	if e = os.WriteFile(filepath.Join(bins, "ssh"), []byte(ssh), 0700); e != nil {
		t.Fatal(e)
	}
	p := NodeRoamingSupervisorPlan{Version: 1, PlanID: planID, OperationID: op, NodeID: "nat-node", Helper: helper, HelperSHA256: metadata.Metadata.HelperSHA256, Directory: slot, Managed: m}
	wire, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	workers := json.RawMessage(`{"version":1,"nodes":[],"agents":[],"sources":[],"runtimes":[{"backend":"codex","binary":"/usr/bin/true","execution":{}}]}`)
	request := nodeagent.RoamingDeploymentRequest{NodeID: "nat-node", Action: "preflight", OperationID: op, PlanID: planID, Plan: wire, Workers: workers, Preferences: &nodeagent.ExecutionPreferences{Schema: 1, Revision: 1}}
	if result, e := client.RoamingDeployment(t.Context(), request); e != nil || result.Outcome != "accepted" {
		t.Fatal("paired preflight", result, e)
	}
	missingIdentity := p
	unboundManaged := *m
	unboundManaged.CoordinatorIdentity = nil
	missingIdentity.Managed = &unboundManaged
	unboundWire, e := json.Marshal(missingIdentity)
	if e != nil {
		t.Fatal(e)
	}
	unboundRequest := request
	unboundRequest.Plan = unboundWire
	if _, e = client.RoamingDeployment(t.Context(), unboundRequest); e == nil {
		t.Fatal("paired preflight adopted a plan without coordinator enrollment identity")
	}
	if _, e = os.Stat(slot); !os.IsNotExist(e) {
		t.Fatal("preflight wrote supervisor slot", e)
	}
	request.Action = "prepare"
	if result, e := client.RoamingDeployment(t.Context(), request); e != nil || result.Outcome != "accepted" {
		t.Fatal("paired prepare", result, e)
	}
	if _, e = client.RoamingDeployment(t.Context(), request); e == nil {
		t.Fatal("prepare replayed original operation")
	}
	request = nodeagent.RoamingDeploymentRequest{NodeID: "nat-node", Action: "start", OperationID: op, PlanID: planID}
	if result, e := client.RoamingDeployment(t.Context(), request); e != nil || result.Outcome != "accepted" {
		t.Fatal("paired start", result, e)
	}
	// Always stop this contained supervisor using its original exact plan.
	defer func() {
		_ = SetNodeRoamingSupervisorState(filepath.Join(slot, "supervisor.json"), "original-disable", "disabled")
		time.Sleep(1100 * time.Millisecond)
	}()
	await := func() (*nodeagent.Client, error) {
		var peer *nodeagent.Client
		e := boundedRoamingRetry(t.Context(), func(ctx context.Context) error {
			var err error
			peer, err = nodeagent.Dial(ctx, filepath.Join(m.AgentDirectory, "agent.sock"), "nat-node")
			if err == nil {
				_, err = peer.Catalog(ctx)
				if err != nil {
					peer.Close()
				}
			}
			return err
		})
		return peer, e
	}
	peer, e := await()
	if e != nil {
		t.Fatal("production supervisor child did not appear", e)
	}
	peer.Close()
	// An ambiguous original start never redispatches the helper.
	repeated, e := client.RoamingDeployment(t.Context(), request)
	if e != nil || repeated.Outcome != "unknown" {
		t.Fatal("start replayed instead of retaining uncertainty", repeated, e)
	}
	client.Close()
	// Observing APP detach has no stop authority over the independent child.
	client, e = nodeagent.Dial(t.Context(), filepath.Join(directory, "agent.sock"), "nat-node")
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	peer, e = await()
	if e != nil {
		t.Fatal("observer detach stopped independently owned child", e)
	}
	peer.Close()
	control := nodeagent.RoamingDeploymentRequest{NodeID: "nat-node", Action: "control", OperationID: op, PlanID: planID, DisableOperationID: "original-disable", State: "disabling"}
	if result, e := client.RoamingDeployment(t.Context(), control); e != nil || result.State != "disabling" {
		t.Fatal("original paired disable marker", result, e)
	}
	control.Action = "receipt"
	control.State = ""
	if result, e := client.RoamingDeployment(t.Context(), control); e != nil || result.State != "disabling" {
		t.Fatal("read-only original receipt", result, e)
	}
	changed := control
	changed.DisableOperationID = "different-disable"
	if _, e = client.RoamingDeployment(t.Context(), changed); e == nil {
		t.Fatal("changed disable identity accepted")
	}
	control.Action = "control"
	control.State = "disabled"
	if result, e := client.RoamingDeployment(t.Context(), control); e != nil || result.State != "disabled" {
		t.Fatal("original paired disable", result, e)
	}
}

func TestOutgoingDeploymentObservationReopensAfterOriginalManagementClose(t *testing.T) {
	directory, e := os.MkdirTemp("/tmp", "caelis-obs-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(directory)
	if e = os.WriteFile(filepath.Join(directory, "caelis-node"), []byte("#!/bin/sh\nexit 1\n"), 0700); e != nil {
		t.Fatal(e)
	}
	service, e := nodeagent.New(nodeagent.Options{Directory: directory, NodeID: "nat-node", Join: api.NodeOutgoing})
	if e != nil {
		t.Fatal(e)
	}
	route := nodeagent.OutgoingRoute{Target: "coordinator-ssh", Helper: "/paired/agent", Directory: "/broker/joins/nat-node"}
	if e = localstate.Write(filepath.Join(directory, "outgoing-route.json"), route); e != nil {
		t.Fatal(e)
	}
	reg := NodeRegistration{ID: "nat-node", Join: api.NodeOutgoing, BrokerNodeID: "coordinator", SSHDestination: route.Target, HelperPath: route.Helper, Directory: route.Directory, SocketPath: filepath.Join(route.Directory, "agent.sock")}
	dialed := 0
	native := &nativeNodeManagement{closed: true, document: nodeManagementDocument{Nodes: []NodeRegistration{reg}}, options: NodeManagementNativeOptions{Dial: func(ctx context.Context, r NodeRegistration) (nodeplane.CatalogAgent, error) {
		dialed++
		if r != reg {
			t.Fatal("observation route changed", r)
		}
		return service, nil
	}}}
	n := &roamingNativeAssembly{original: &nodeManagement{agent: native}}
	metadata, e := n.outgoingMetadata(t.Context(), reg.ID)
	if e != nil || metadata.Directory != directory || metadata.Directory == reg.Directory || dialed != 1 {
		t.Fatal("closed original observer prevented independent paired metadata", metadata, e, dialed)
	}
}
