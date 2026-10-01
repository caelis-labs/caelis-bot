package nodebroker

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeagent"
	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type nativeProofFixture struct {
	mu     sync.Mutex
	target api.WorkTarget
	state  nodeplane.RuntimeEligibility
}

func (n *nativeProofFixture) ReadRuntimeProof(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if target != n.target {
		return nodeplane.RuntimeEligibility{}, errors.New("foreign native owner")
	}
	return n.state, ctx.Err()
}
func (n *nativeProofFixture) install(epoch string) {
	n.mu.Lock()
	n.state.LeaseEpoch = epoch
	n.state.SafeIdle = false
	n.state.Pending = true
	n.mu.Unlock()
}
func (n *nativeProofFixture) uncertain() { n.mu.Lock(); n.state.Unknown = true; n.mu.Unlock() }

// This fixture uses the production registry, the actual framed agent protocol,
// a bounded loopback agent listener and the actual private Unix broker protocol.
// Native state is deliberately independent of claim payloads and credentials.
func TestPrivateBrokerQueriesPairedRealAgentNativeOwner(t *testing.T) {
	temp, e := os.MkdirTemp("/tmp", "bp-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(temp)
	dir, e := filepath.EvalSymlinks(temp)
	if e != nil {
		t.Fatal(e)
	}
	b, ref := testBundle("0", "1", "full")
	r := request(ref, "")
	native := &nativeProofFixture{target: r.Target, state: nodeplane.RuntimeEligibility{Proof: r.Proof, Snapshot: ref, SafeIdle: true}}
	agentDir := filepath.Join(dir, "agent")
	if e = os.Mkdir(agentDir, 0700); e != nil {
		t.Fatal(e)
	}
	agent, e := nodeagent.New(nodeagent.Options{Directory: agentDir, NodeID: r.Target.NodeID, RuntimeOwner: native})
	if e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agentDone := make(chan error, 1)
	go func() { agentDone <- nodeagent.Serve(ctx, listener, agent) }()
	conn, e := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	peer, e := nodeagent.NewClient(r.Target.NodeID, conn)
	if e != nil {
		t.Fatal(e)
	}
	defer peer.Close()
	registry := nodecoord.NewPeerRegistry()
	if e = registry.Register(r.Target, peer); e != nil {
		t.Fatal(e)
	}
	if e = registry.VerifyBootstrap(ctx, r.Target, ref); e != nil {
		t.Fatal(e)
	}
	c, e := nodecoord.Open(nodecoord.Options{Directory: filepath.Join(dir, "cache"), BotID: "bot", BrokerNodeID: "broker", ReadOwnerEligibility: registry.ReadOwnerEligibility, VerifyBootstrap: registry.VerifyBootstrap, Verify: registry.VerifyClaim, VerifyRenew: registry.VerifyRenew, ValidateSnapshot: testValidate})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	ready := make(chan struct{})
	brokerDone := make(chan error, 1)
	socket := filepath.Join(dir, "broker.sock")
	go func() { brokerDone <- ServeUnix(ctx, socket, c, func() { close(ready) }) }()
	select {
	case <-ready:
	case e := <-brokerDone:
		t.Fatal(e)
	case <-time.After(3 * time.Second):
		t.Fatal("broker fixture startup exceeded")
	}
	client, e := DialUnixForBroker(ctx, socket, "broker")
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	if wrong, err := DialUnixForBroker(ctx, socket, "foreign"); err == nil {
		wrong.Close()
		t.Fatal("foreign broker enrollment admitted")
	}
	if e = client.BootstrapSnapshot(ctx, r.Target, ref, b); e != nil {
		t.Fatal(e)
	}
	if e = client.BootstrapSnapshot(ctx, r.Target, ref, b); !errors.Is(e, nodecoord.ErrConflict) {
		t.Fatalf("genesis reset: %v", e)
	}
	forged := r
	forged.Proof.Epoch = "wire-self-reported"
	if _, e = client.Claim(ctx, forged); !errors.Is(e, nodecoord.ErrIneligible) {
		t.Fatalf("wire proof trusted: %v", e)
	}
	l, e := client.Claim(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	native.install(l.Epoch)
	if _, e = client.Heartbeat(ctx, l); e != nil {
		t.Fatal(e)
	}
	native.uncertain()
	workerRef := nodeplane.WorkLeaseRef{BotID: l.BotID, BrokerNodeID: "broker", SourceNode: l.NodeID, SourceBackend: l.Backend, Epoch: l.Epoch}
	if live, err := client.ReadWorkerLease(ctx, workerRef); err != nil || live.Epoch != l.Epoch || live.TTLMs <= 0 {
		t.Fatalf("worker authority %+v %v", live, err)
	}
	workerRef.SourceNode = "foreign"
	if _, err := client.ReadWorkerLease(ctx, workerRef); !errors.Is(err, nodecoord.ErrConflict) {
		t.Fatalf("foreign source: %v", err)
	}
	if _, e = client.Heartbeat(ctx, l); e != nil {
		t.Fatalf("controlled unknown owner lost lease: %v", e)
	}
	cancel()
	for _, done := range []chan error{agentDone, brokerDone} {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("fixture cleanup exceeded")
		}
	}
}
