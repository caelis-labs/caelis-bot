package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodebroker"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/productrpc"
)

// The SSH fixture relays bytes to a real private paired agent socket. It cannot
// execute the outgoing host's proxy-product command on the coordinator.
func init() {
	if len(os.Args) != 3 || os.Args[1] != "outgoing-product-agent-fixture" {
		return
	}
	c, err := net.Dial("unix", os.Args[2])
	if err != nil {
		os.Exit(2)
	}
	go func() { _, _ = io.Copy(c, os.Stdin); _ = c.Close() }()
	_, _ = io.Copy(os.Stdout, c)
	_ = c.Close()
	os.Exit(0)
}

type outgoingProductTarget struct {
	mu       sync.Mutex
	endpoint nodeagent.ManagedProductEndpoint
	token    string
	disables atomic.Int32
}

func (p *outgoingProductTarget) ReadManagedProduct(_ context.Context, target api.WorkTarget) (nodeagent.ManagedProductEndpoint, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if target != (api.WorkTarget{NodeID: p.endpoint.Lease.NodeID, Backend: string(p.endpoint.Lease.Backend), Role: api.RoleBot}) {
		return nodeagent.ManagedProductEndpoint{}, errors.New("owner target changed")
	}
	return p.endpoint, nil
}

func (p *outgoingProductTarget) ProxyManagedProduct(ctx context.Context, request nodeagent.ManagedProductRequest) (nodeagent.ManagedProductResponse, error) {
	current, err := p.ReadManagedProduct(ctx, request.Target)
	if err != nil || !nodeagent.ValidManagedProductRequest(request) || current.Identity != request.Identity {
		return nodeagent.ManagedProductResponse{}, errors.New("owner generation changed")
	}
	r, err := http.NewRequestWithContext(ctx, request.Method, current.Endpoint+request.Path, bytes.NewReader(request.Body))
	if err != nil {
		return nodeagent.ManagedProductResponse{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+p.token)
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		return nodeagent.ManagedProductResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return nodeagent.ManagedProductResponse{Status: response.StatusCode, ContentType: "application/json", Body: body}, err
}

func (p *outgoingProductTarget) PrepareManagedDisable(context.Context, nodeagent.ManagedDisableRequest) (nodeplane.SnapshotRef, error) {
	p.disables.Add(1)
	return nodeplane.SnapshotRef{}, errors.New("observer cannot disable")
}
func (p *outgoingProductTarget) ReconcileManagedDisable(context.Context, nodeagent.ManagedDisableRequest) (nodeagent.ManagedDisableReceipt, error) {
	return nodeagent.ManagedDisableReceipt{}, errors.New("observer cannot disable")
}

func TestNativeOutgoingProductResolvesThroughPairedAgentAndDetachesOnly(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "roam-product-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	port := &thinProductPort{snapshot: api.Snapshot{Connection: "ready", CanSend: true, Revision: 1, Approvals: []api.Approval{{ID: "native-approval", Target: "native-target", Status: "pending", Choices: []api.Choice{{ID: "once", Label: "Allow once"}}}}}}
	token := strings.Repeat("private-target-fixture-", 3)
	server, err := productrpc.NewServer(port, productrpc.Options{NodeID: "nat-node", BotID: productrpc.ProfileBotID("bot-fixture"), Token: token, JournalFile: filepath.Join(dir, "product-journal.json")})
	if err != nil {
		t.Fatal(err)
	}
	port.server = server
	product := httptest.NewServer(server)
	t.Cleanup(product.Close)
	lease := nodeplane.Lease{BotID: "bot-fixture", NodeID: "nat-node", Backend: api.NodeCodex, Epoch: "1", ExpiresAt: time.Now().Add(time.Hour), TTLMs: 60000}
	owner := &outgoingProductTarget{token: token, endpoint: nodeagent.ManagedProductEndpoint{BotID: lease.BotID, Lease: lease, Identity: server.Identity(), Endpoint: product.URL, AuthFile: "/outgoing/private/product.token"}}
	agent, err := nodeagent.New(nodeagent.Options{Directory: dir, NodeID: lease.NodeID, Join: api.NodeOutgoing, ManagedProduct: owner})
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	life, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); _ = nodeagent.Serve(life, listener, agent) }()
	t.Cleanup(func() { cancel(); <-done })
	shared, err := nodeagent.Dial(t.Context(), socket, lease.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.Close() })
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "ssh-arguments")
	script := "#!/bin/sh\ncase \"$*\" in *proxy-product*) exit 73;; esac\nprintf '%s\\n' \"$*\" >> " + nodeShellQuote(log) + "\nexec " + nodeShellQuote(executable) + " outgoing-product-agent-fixture " + nodeShellQuote(socket) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	reg := NodeRegistration{ID: lease.NodeID, Label: "Outgoing", Join: api.NodeOutgoing, SSHDestination: "existing-coordinator", HelperPath: "/coordinator/caelis-agent"}
	node := roamingNativeNode{Registration: reg, HostHelper: "/outgoing/caelis-node", BrokerPeerSocket: "/coordinator/private/joins/nat/agent.sock", Plan: NodeRoamingSupervisorPlan{Managed: &NodeRoamingManagedDeployment{AuthFile: owner.endpoint.AuthFile}}}
	session := &roamingNativeSession{plan: roamingNativePlan{BotID: lease.BotID, Nodes: []roamingNativeNode{node}}, peers: map[string]*nodeagent.Client{lease.NodeID: shared}, observerCtx: life}
	native := &roamingNativeAssembly{sessions: map[*nodebroker.Client]*roamingNativeSession{nil: session}}
	location, err := native.resolve(t.Context(), lease, reg)
	if err != nil || location.ClientFactory == nil || location.Generation != server.Identity().Generation {
		t.Fatal("native outgoing resolver did not install paired transport", err)
	}
	changedPairing := location.Pairing
	changedPairing.NodeID = "different-node"
	if _, _, err := location.ClientFactory(changedPairing); err == nil {
		t.Fatal("caller replaced frozen observer pairing")
	}
	engine, err := newProductEngine(t.TempDir(), location.Pairing, location.ClientFactory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close(context.Background()) })
	if err := engine.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	snapshot := engine.Snapshot()
	if len(snapshot.Approvals) != 1 {
		t.Fatal("actual product projection missing", snapshot)
	}
	if err := engine.Decide(t.Context(), api.Decision{ID: snapshot.Approvals[0].ID, Choice: "once"}); err != nil {
		t.Fatal(err)
	}
	port.mu.Lock()
	decision := port.decision
	port.mu.Unlock()
	if decision.ID != "native-approval" || decision.Choice != "once" {
		t.Fatal("approval lost exact target", decision)
	}
	client, detach, err := location.ClientFactory(location.Pairing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if receipt, err := client.Receipt(t.Context(), "original-unknown-request"); err != nil || receipt.Outcome != "unknown" {
		t.Fatal("original receipt was not preserved", receipt, err)
	}
	owner.mu.Lock()
	owner.endpoint.Identity.Generation = "later-generation"
	owner.mu.Unlock()
	if _, err := client.State(t.Context()); err == nil {
		t.Fatal("observer crossed product generation")
	}
	client.Close()
	_ = detach.Close()
	_ = engine.Close(t.Context())
	if _, err := shared.Catalog(t.Context()); err != nil {
		t.Fatal("observer closed shared management connection", err)
	}
	port.mu.Lock()
	stops := port.stops
	port.mu.Unlock()
	if stops != 0 || owner.disables.Load() != 0 {
		t.Fatal("detach stopped independent owner")
	}
	direct, err := productrpc.NewClient(productrpc.ClientOptions{URL: product.URL, ExpectedNode: lease.NodeID, ExpectedBot: server.Identity().BotID, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	defer direct.Close()
	if identity, err := direct.Connect(t.Context()); err != nil || identity != server.Identity() {
		t.Fatal("observer detach ended target product service", err)
	}
	for _, mutation := range []string{"node", "epoch", "generation"} {
		t.Run(mutation, func(t *testing.T) {
			owner.mu.Lock()
			owner.endpoint.Identity = server.Identity()
			owner.endpoint.Lease = lease
			if mutation == "node" {
				owner.endpoint.Identity.NodeID = "different-node"
			} else if mutation == "epoch" {
				owner.endpoint.Lease.Epoch = "2"
			} else {
				owner.endpoint.Identity.Generation = "different-generation"
			}
			owner.mu.Unlock()
			if client, detach, err := location.ClientFactory(location.Pairing); err == nil {
				client.Close()
				_ = detach.Close()
				t.Fatal("stale factory adopted changed owner")
			}
		})
	}
	arguments, err := os.ReadFile(log)
	if err != nil || strings.Contains(string(arguments), "proxy-product") || !strings.Contains(string(arguments), "'/coordinator/caelis-agent' proxy-agent --socket '/coordinator/private/joins/nat/agent.sock'") {
		t.Fatal("observer did not use enrolled paired coordinator socket", err)
	}
}
