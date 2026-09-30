package nodebroker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodecoord"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type testPayload struct{ BotID, Epoch, Version, Body string }

func testBundle(epoch, version, body string) ([]byte, nodeplane.SnapshotRef) {
	b, _ := json.Marshal(testPayload{"bot", epoch, version, body})
	sum := sha256.Sum256(b)
	return b, nodeplane.SnapshotRef{BotID: "bot", Epoch: epoch, Version: version, Digest: hex.EncodeToString(sum[:])}
}
func testValidate(_ context.Context, b []byte) (nodeplane.SnapshotRef, error) {
	var p testPayload
	if json.Unmarshal(b, &p) != nil {
		return nodeplane.SnapshotRef{}, nodecoord.ErrSnapshot
	}
	sum := sha256.Sum256(b)
	return nodeplane.SnapshotRef{BotID: p.BotID, Epoch: p.Epoch, Version: p.Version, Digest: hex.EncodeToString(sum[:])}, nil
}
func request(ref nodeplane.SnapshotRef, expected string) nodeplane.ClaimRequest {
	return nodeplane.ClaimRequest{BotID: "bot", Target: api.WorkTarget{NodeID: "n1", Backend: "codex", Role: api.RoleBot}, ExpectedEpoch: expected, Snapshot: ref, Proof: nodeplane.RuntimeProof{NodeID: "n1", Backend: api.NodeCodex, Epoch: "owned-native", Controllable: true}}
}
func TestRealPrivateUnixBrokerPartitionReceiptReplayAndSnapshot(t *testing.T) {
	temp, e := os.MkdirTemp("/tmp", "nb-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temp) })
	dir, e := filepath.EvalSymlinks(temp)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	c, e := nodecoord.Open(nodecoord.Options{Directory: filepath.Join(dir, "state"), BotID: "bot", Verify: func(ctx context.Context, r nodeplane.ClaimRequest) error {
		if r.Target.NodeID != "n1" {
			return nodecoord.ErrIneligible
		}
		return ctx.Err()
	}, ValidateSnapshot: testValidate})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	b, s := testBundle("0", "1", "complete")
	if e = c.SeedSnapshot(context.Background(), s, b); e != nil {
		t.Fatal(e)
	}
	socket := filepath.Join(dir, "broker.sock")
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- ServeUnix(ctx, socket, c, func() { close(ready) }) }()
	select {
	case <-ready:
	case e := <-done:
		t.Fatal(e)
	case <-time.After(3 * time.Second):
		t.Fatal("broker startup bound exceeded")
	}
	client, e := DialUnix(socket)
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	r := request(s, "")
	l, e := client.Claim(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	if l.TTLMs < 55000 || l.TTLMs > 60000 {
		t.Fatal(l)
	}
	// Dispose the observer, reconnect, and reconcile the original CAS. No native
	// command is introduced and the committed epoch is retained.
	client.Close()
	client2, e := DialUnix(socket)
	if e != nil {
		t.Fatal(e)
	}
	defer client2.Close()
	replayed, e := client2.Claim(ctx, r)
	if e != nil || replayed.Epoch != l.Epoch || replayed.TTLMs > l.TTLMs {
		t.Fatalf("receipt %+v %v", replayed, e)
	}
	b, latest := testBundle(l.Epoch, "2", "deleted-tree")
	if e = client2.PublishSnapshot(ctx, l, latest, b); e != nil {
		t.Fatal(e)
	}
	got, e := client2.LatestSnapshot(ctx, "bot")
	if e != nil || got != latest {
		t.Fatalf("latest %+v %v", got, e)
	}
	body, e := client2.ReadSnapshot(ctx, latest)
	if e != nil || string(body) != string(b) {
		t.Fatal(e)
	}
	_, e = client2.ReadSnapshot(ctx, s)
	if !errors.Is(e, nodecoord.ErrSnapshot) {
		t.Fatal(e)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("partition shutdown bound exceeded")
	}
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	_, e = client2.Heartbeat(bounded, l)
	if e == nil {
		t.Fatal("partition renewed lease")
	}
	// The broker coordinator still owns the committed lease after observer loss.
	_, _, e = c.SnapshotState(context.Background())
	if e != nil {
		t.Fatal(e)
	}
}
func TestUnpairedNativeOwnerCannotClaimEvenWithSelfReportedProof(t *testing.T) {
	temp, e := os.MkdirTemp("/tmp", "nb-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temp) })
	dir, e := filepath.EvalSymlinks(temp)
	if e != nil {
		t.Fatal(e)
	}
	_ = os.Chmod(dir, 0700)
	c, e := nodecoord.Open(nodecoord.Options{Directory: dir, BotID: "bot", ValidateSnapshot: testValidate})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	b, s := testBundle("0", "1", "complete")
	if e = c.SeedSnapshot(context.Background(), s, b); e != nil {
		t.Fatal(e)
	}
	_, e = c.Claim(context.Background(), request(s, ""))
	if !errors.Is(e, nodecoord.ErrIneligible) {
		t.Fatal(e)
	}
}
