package nodecoord

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type bundle struct{ BotID, Epoch, Version, Body string }

func payload(epoch, version, body string) ([]byte, nodeplane.SnapshotRef) {
	b, _ := json.Marshal(bundle{"bot", epoch, version, body})
	sum := sha256.Sum256(b)
	return b, nodeplane.SnapshotRef{BotID: "bot", Epoch: epoch, Version: version, Digest: hex.EncodeToString(sum[:])}
}
func validate(_ context.Context, b []byte) (nodeplane.SnapshotRef, error) {
	var v bundle
	if json.Unmarshal(b, &v) != nil {
		return nodeplane.SnapshotRef{}, ErrSnapshot
	}
	sum := sha256.Sum256(b)
	return nodeplane.SnapshotRef{BotID: v.BotID, Epoch: v.Epoch, Version: v.Version, Digest: hex.EncodeToString(sum[:])}, nil
}
func fixture(t *testing.T) (*Coordinator, *clock, nodeplane.SnapshotRef) {
	t.Helper()
	cl := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	c, e := Open(Options{Directory: dir, BotID: "bot", Now: cl.now, Verify: func(context.Context, nodeplane.ClaimRequest) error { return nil }, ValidateSnapshot: validate})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(c.Close)
	b, s := payload("0", "1", "initial")
	if e = c.SeedSnapshot(context.Background(), s, b); e != nil {
		t.Fatal(e)
	}
	return c, cl, s
}
func claim(node string, s nodeplane.SnapshotRef, expected string) nodeplane.ClaimRequest {
	return nodeplane.ClaimRequest{BotID: "bot", Target: api.WorkTarget{NodeID: node, Backend: "codex", Role: api.RoleBot}, ExpectedEpoch: expected, Snapshot: s, Proof: nodeplane.RuntimeProof{NodeID: node, Backend: api.NodeCodex, Epoch: "owned-native-generation", Controllable: true}}
}
func requireError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
func TestDuplicateCASReceiptDoesNotExtendOrGrantTwoOwners(t *testing.T) {
	c, cl, s := fixture(t)
	ctx := context.Background()
	r := claim("n1", s, "")
	l, e := c.Claim(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	cl.advance(11 * time.Second)
	again, e := c.Claim(ctx, r)
	if e != nil || again.Epoch != l.Epoch || again.TTLMs != 49000 {
		t.Fatalf("duplicate receipt %+v %v", again, e)
	}
	_, e = c.Claim(ctx, claim("n2", s, "1"))
	requireError(t, e, ErrConflict)
	cl.advance(49 * time.Second)
	_, e = c.Heartbeat(ctx, l)
	requireError(t, e, ErrConflict)
	l2, e := c.Claim(ctx, claim("n2", s, l.Epoch))
	if e != nil || l2.Epoch != "2" {
		t.Fatalf("takeover %+v %v", l2, e)
	}
	_, e = c.Heartbeat(ctx, l)
	requireError(t, e, ErrConflict)
}
func TestConcurrentDuplicateAndForeignClaims(t *testing.T) {
	c, _, s := fixture(t)
	ctx := context.Background()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := map[string]bool{}
	for i := 0; i < 12; i++ {
		node := "n1"
		if i%2 == 1 {
			node = "n2"
		}
		wg.Go(func() {
			<-start
			l, e := c.Claim(ctx, claim(node, s, ""))
			if e == nil {
				mu.Lock()
				winners[l.NodeID] = true
				mu.Unlock()
			} else if !errors.Is(e, ErrConflict) {
				t.Error(e)
			}
		})
	}
	close(start)
	wg.Wait()
	if len(winners) != 1 {
		t.Fatal(winners)
	}
	r := claim("foreign", s, "")
	r.BotID = "other"
	_, e := c.Claim(ctx, r)
	requireError(t, e, ErrIneligible)
	r = claim("n1", s, "")
	r.Proof.NodeID = "other"
	_, e = c.Claim(ctx, r)
	requireError(t, e, ErrIneligible)
}
func TestStaleDeletedSnapshotCannotResurrectOrRenew(t *testing.T) {
	c, cl, initial := fixture(t)
	ctx := context.Background()
	l, e := c.Claim(ctx, claim("n1", initial, ""))
	if e != nil {
		t.Fatal(e)
	}
	b, latest := payload(l.Epoch, "2", "deleted-complete-tree")
	if e = c.PublishSnapshot(ctx, l, latest, b); e != nil {
		t.Fatal(e)
	}
	b, stale := payload(l.Epoch, "1", "old-content")
	requireError(t, c.PublishSnapshot(ctx, l, stale, b), ErrSnapshot)
	cl.advance(60 * time.Second)
	_, e = c.Claim(ctx, claim("n2", initial, l.Epoch))
	requireError(t, e, ErrSnapshot)
	l2, e := c.Claim(ctx, claim("n2", latest, l.Epoch))
	if e != nil {
		t.Fatal(e)
	}
	requireError(t, c.PublishSnapshot(ctx, l, latest, b), ErrConflict)
	b, _ = payload(l.Epoch, "2", "damaged")
	requireError(t, c.PublishSnapshot(ctx, l2, nodeplane.SnapshotRef{BotID: "bot", Epoch: l2.Epoch, Version: "3", Digest: latest.Digest}, b), ErrSnapshot)
}
func TestRestartQuarantinesRegardlessOfClockAndLocksState(t *testing.T) {
	c, cl, s := fixture(t)
	ctx := context.Background()
	l, e := c.Claim(ctx, claim("n1", s, ""))
	if e != nil {
		t.Fatal(e)
	}
	o := c.opts
	if _, e = Open(o); e == nil {
		t.Fatal("second broker obtained lock")
	}
	c.Close()
	cl.advance(-24 * time.Hour)
	c2, e := Open(o)
	if e != nil {
		t.Fatal(e)
	}
	defer c2.Close()
	_, e = c2.Claim(ctx, claim("n2", s, l.Epoch))
	requireError(t, e, ErrUnavailable)
	_, e = c2.Heartbeat(ctx, l)
	requireError(t, e, ErrUnavailable)
	cl.advance(59 * time.Second)
	_, e = c2.Claim(ctx, claim("n2", s, l.Epoch))
	requireError(t, e, ErrUnavailable)
	cl.advance(time.Second)
	l2, e := c2.Claim(ctx, claim("n2", s, l.Epoch))
	if e != nil || l2.Epoch != "2" {
		t.Fatalf("restart %+v %v", l2, e)
	}
}
func TestClockReversalAndMissingLatestFailClosed(t *testing.T) {
	c, cl, s := fixture(t)
	ctx := context.Background()
	l, e := c.Claim(ctx, claim("n1", s, ""))
	if e != nil {
		t.Fatal(e)
	}
	cl.advance(-time.Nanosecond)
	_, e = c.Heartbeat(ctx, l)
	requireError(t, e, ErrUnavailable)
	cl.advance(time.Hour)
	_, e = c.Claim(ctx, claim("n2", s, l.Epoch))
	requireError(t, e, ErrUnavailable)
	c2, cl2, s2 := fixture(t)
	if e = os.Remove(filepath.Join(c2.opts.Directory, "snapshot-"+s2.Digest+".json")); e != nil {
		t.Fatal(e)
	}
	cl2.advance(time.Minute)
	_, e = c2.Claim(ctx, claim("n1", s2, ""))
	requireError(t, e, ErrSnapshot)
}
func TestVerifierCannotPassAnExpiredOrCanceledHeartbeat(t *testing.T) {
	c, cl, s := fixture(t)
	ctx := context.Background()
	l, e := c.Claim(ctx, claim("n1", s, ""))
	if e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{})
	proceed := make(chan struct{})
	c.opts.VerifyRenew = func(ctx context.Context, _ nodeplane.Lease, _ nodeplane.ClaimRequest) error {
		close(entered)
		<-proceed
		return nil
	}
	done := make(chan error, 1)
	go func() { _, e := c.Heartbeat(ctx, l); done <- e }()
	<-entered
	cl.advance(60 * time.Second)
	close(proceed)
	requireError(t, <-done, ErrConflict)
	c2, _, s2 := fixture(t)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, e = c2.Claim(canceled, claim("n1", s2, ""))
	requireError(t, e, context.Canceled)
}
func TestUnmanagedAndUnknownOwnerNeverEligible(t *testing.T) {
	c, _, s := fixture(t)
	r := claim("n1", s, "")
	c.opts.Verify = nil
	_, e := c.Claim(context.Background(), r)
	requireError(t, e, ErrIneligible)
	c.opts.Verify = func(context.Context, nodeplane.ClaimRequest) error { return errors.New("unknown native receipt") }
	_, e = c.Claim(context.Background(), r)
	requireError(t, e, ErrIneligible)
}
func TestOfflineInstallLockedAgainstNewPublication(t *testing.T) {
	c, _, s := fixture(t)
	ctx := context.Background()
	l, e := c.Claim(ctx, claim("n1", s, ""))
	if e != nil {
		t.Fatal(e)
	}
	b, next := payload(l.Epoch, "2", "new")
	if e = c.PublishSnapshot(ctx, l, next, b); e != nil {
		t.Fatal(e)
	}
	called := false
	e = c.CommitInstall(ctx, s, func() error { called = true; return nil })
	requireError(t, e, ErrSnapshot)
	if called {
		t.Fatal("stale install ran")
	}
	if e = c.CommitInstall(ctx, next, func() error { called = true; return nil }); e != nil || !called {
		t.Fatal(e)
	}
}

func TestDeletedDurableEpochCannotResetBroker(t *testing.T) {
	c, _, s := fixture(t)
	if _, e := c.Claim(context.Background(), claim("n1", s, "")); e != nil {
		t.Fatal(e)
	}
	o := c.opts
	c.Close()
	if e := os.Remove(filepath.Join(o.Directory, "state.json")); e != nil {
		t.Fatal(e)
	}
	if reopened, e := Open(o); e == nil {
		reopened.Close()
		t.Fatal("deleted durable epoch reset broker")
	}
}

func TestLiveWorkerLeaseReadsRecheckPairedOwnerAndDeadline(t *testing.T) {
	c, cl, s := fixture(t)
	r := claim("n1", s, "")
	p := &ownedPeer{state: nodeplane.RuntimeEligibility{Proof: r.Proof, Snapshot: s, SafeIdle: true}}
	registry := NewPeerRegistry()
	if e := registry.Register(r.Target, p); e != nil {
		t.Fatal(e)
	}
	c.opts.Verify = registry.VerifyClaim
	c.opts.VerifyRenew = registry.VerifyRenew
	c.opts.ReadOwnerEligibility = registry.ReadOwnerEligibility
	c.opts.BrokerNodeID = "broker"
	ctx := context.Background()
	l, e := c.Claim(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	p.state.LeaseEpoch = l.Epoch
	p.state.Unknown = true
	cl.advance(17 * time.Second)
	ref := nodeplane.WorkLeaseRef{BotID: "bot", BrokerNodeID: "broker", SourceNode: "n1", SourceBackend: api.NodeCodex, Epoch: l.Epoch}
	live, e := c.ReadWorkerLease(ctx, ref)
	if e != nil || live.TTLMs != 43000 {
		t.Fatalf("fresh remaining %+v %v", live, e)
	}
	foreign := ref
	foreign.BrokerNodeID = "other"
	_, e = c.ReadWorkerLease(ctx, foreign)
	requireError(t, e, ErrIneligible)
	foreign = ref
	foreign.Epoch = "other"
	_, e = c.ReadWorkerLease(ctx, foreign)
	requireError(t, e, ErrConflict)
	c.opts.ReadOwnerEligibility = func(context.Context, api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
		cl.advance(43 * time.Second)
		return p.state, nil
	}
	_, e = c.ReadWorkerLease(ctx, ref)
	requireError(t, e, ErrConflict)
}

func TestGenesisRequiresExactPairedStoppedSourceAndNeverResets(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	b, ref := payload("0", "1", "authoritative genesis")
	r := claim("source", ref, "")
	peer := &ownedPeer{state: nodeplane.RuntimeEligibility{Proof: r.Proof, Snapshot: ref, SafeIdle: true}}
	registry := NewPeerRegistry()
	if err = registry.Register(r.Target, peer); err != nil {
		t.Fatal(err)
	}
	c, err := Open(Options{Directory: dir, BotID: "bot", Verify: registry.VerifyClaim, VerifyBootstrap: registry.VerifyBootstrap, ValidateSnapshot: validate})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()
	foreign := r.Target
	foreign.NodeID = "foreign"
	requireError(t, c.BootstrapSnapshot(ctx, foreign, ref, b), ErrIneligible)
	peer.state.Unknown = true
	requireError(t, c.BootstrapSnapshot(ctx, r.Target, ref, b), ErrIneligible)
	peer.state.Unknown = false
	requireError(t, c.BootstrapSnapshot(ctx, r.Target, ref, append(append([]byte{}, b...), byte('x'))), ErrSnapshot)
	if _, err = c.LatestSnapshot(ctx, "bot"); !errors.Is(err, ErrSnapshot) {
		t.Fatalf("failed genesis installed: %v", err)
	}
	if err = c.BootstrapSnapshot(ctx, r.Target, ref, b); err != nil {
		t.Fatal(err)
	}
	requireError(t, c.BootstrapSnapshot(ctx, r.Target, ref, b), ErrConflict)
	if got, err := c.ReadSnapshot(ctx, ref); err != nil || string(got) != string(b) {
		t.Fatalf("complete genesis unavailable: %v", err)
	}
}
