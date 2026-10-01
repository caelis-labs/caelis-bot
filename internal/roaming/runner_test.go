package roaming

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/memorytransfer"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type runnerBroker struct {
	payload                     []byte
	ref                         nodeplane.SnapshotRef
	lease                       nodeplane.Lease
	failCommit, failHeartbeat   bool
	claims, releases, publishes int
}

func (b *runnerBroker) Claim(_ context.Context, r nodeplane.ClaimRequest) (nodeplane.Lease, error) {
	b.claims++
	if r.Snapshot != b.ref || r.ExpectedEpoch != b.lease.Epoch {
		return nodeplane.Lease{}, errors.New("stale claim")
	}
	b.lease = nodeplane.Lease{BotID: r.BotID, NodeID: r.Target.NodeID, Backend: api.NodeBackend(r.Target.Backend), Epoch: "1", TTLMs: 60000}
	return b.lease, nil
}
func (b *runnerBroker) Heartbeat(_ context.Context, l nodeplane.Lease) (nodeplane.Lease, error) {
	if b.failHeartbeat {
		return nodeplane.Lease{}, errors.New("partition")
	}
	return l, nil
}
func (b *runnerBroker) Release(context.Context, nodeplane.Lease) error { b.releases++; return nil }
func (b *runnerBroker) SnapshotState(context.Context) (nodeplane.Lease, nodeplane.SnapshotRef, error) {
	return b.lease, b.ref, nil
}
func (b *runnerBroker) LatestSnapshot(context.Context, string) (nodeplane.SnapshotRef, error) {
	return b.ref, nil
}
func (b *runnerBroker) ReadSnapshot(_ context.Context, ref nodeplane.SnapshotRef) ([]byte, error) {
	if ref != b.ref {
		return nil, errors.New("stale snapshot")
	}
	return b.payload, nil
}
func (b *runnerBroker) CommitInstall(_ context.Context, ref nodeplane.SnapshotRef, install func() error) error {
	if b.failCommit || ref != b.ref {
		return errors.New("snapshot changed during install")
	}
	return install()
}
func (b *runnerBroker) PublishSnapshot(_ context.Context, l nodeplane.Lease, ref nodeplane.SnapshotRef, payload []byte) error {
	if l.Epoch != b.lease.Epoch {
		return ErrFenced
	}
	b.payload, b.ref = payload, ref
	b.publishes++
	return nil
}

type runnerNative struct {
	*ownerFixture
	guard   *Guard
	ref     nodeplane.SnapshotRef
	started int
}

func (n *runnerNative) ReadRuntimeProof(ctx context.Context, target api.WorkTarget) (nodeplane.RuntimeEligibility, error) {
	l, active := n.guard.Lease()
	epoch := ""
	if active {
		epoch = l.Epoch
	} else {
		l = nodeplane.Lease{BotID: n.ref.BotID, NodeID: target.NodeID, Backend: api.NodeBackend(target.Backend), Epoch: "new-native-generation"}
	}
	p, e := n.guard.Proof(ctx, l)
	return nodeplane.RuntimeEligibility{Proof: p, Snapshot: n.ref, SafeIdle: true, LeaseEpoch: epoch}, e
}
func (n *runnerNative) Start() error {
	l, active := n.guard.Lease()
	if !active {
		return errors.New("started without lease")
	}
	if err := n.guard.Check(context.Background(), l); err != nil {
		return err
	}
	n.started++
	return nil
}
func (n *runnerNative) Close() error                                  { return nil }
func (n *runnerNative) PauseNotebook(context.Context) (func(), error) { return func() {}, nil }
func (n *runnerNative) SetNotebookSnapshot(_ context.Context, _ nodeplane.Lease, r nodeplane.SnapshotRef) error {
	n.ref = r
	return nil
}
func runnerFixture(t *testing.T) (*Runner, *runnerBroker, **runnerNative) {
	t.Helper()
	temporary, err := os.MkdirTemp("/tmp", "r47-runner-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporary) })
	root, err := filepath.EvalSymlinks(temporary)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	if err = os.MkdirAll(filepath.Join(source, "Notebook"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "bot.json"), []byte(`{"version":1,"personalVersion":1,"id":"stable-bot","schedules":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "Notebook", "MEMORY.md"), []byte("Latest corrected preference.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "conversation.json"), []byte(`{"oldNativeThread":"do-not-replay"}`), 0600); err != nil {
		t.Fatal(err)
	}
	payload, ref, err := memorytransfer.ExportNotebook(t.Context(), memorytransfer.NotebookExportOptions{Source: source, SourceStopped: true, Epoch: "0", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	broker := &runnerBroker{payload: payload, ref: ref}
	var native *runnerNative
	target := api.WorkTarget{NodeID: "owned-node", Backend: "codex", Role: api.RoleBot}
	r, err := NewRunner(RunnerOptions{BotID: "stable-bot", Target: target, GenerationRoot: root, Broker: broker, RegisterOwner: func(api.WorkTarget, nodeplane.RuntimeProofPort) error { return nil }, Factory: func(ctx context.Context, profile string, target api.WorkTarget) (ManagedRuntime, *Guard, error) {
		installed, err := memorytransfer.ReadInstalledNotebookRef(ctx, profile)
		if err != nil {
			return nil, nil, err
		}
		for _, file := range []string{"conversation.json", "tasks.json", "care.json", "dream.json"} {
			if _, err = os.Stat(filepath.Join(profile, file)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("old native state imported: %s", file)
			}
		}
		native = &runnerNative{ownerFixture: &ownerFixture{}, ref: installed}
		native.guard = NewGuard(target.NodeID, api.NodeBackend(target.Backend), native, true)
		return native, native.guard, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return r, broker, &native
}

func TestRunnerImportsLatestIntoFreshGenerationAndPublishesOnlyNotebook(t *testing.T) {
	r, b, n := runnerFixture(t)
	if err := r.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer r.guard.Revoke()
	if (*n).started != 1 || b.claims != 1 {
		t.Fatal("missing real admission pipeline")
	}
	if err := r.Publish(t.Context()); err != nil {
		t.Fatal(err)
	}
	if b.publishes != 1 || b.ref.Version != "2" || b.ref.Epoch != "1" {
		t.Fatal("whole Notebook publication missing", b.ref)
	}
	if err := r.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if b.releases != 1 || (*n).stopped.Load() != 1 {
		t.Fatal("lease released before confirmed owner stop")
	}
}
func TestRunnerLatestCASFailureCannotAssembleOrStartRuntime(t *testing.T) {
	r, b, n := runnerFixture(t)
	b.failCommit = true
	if err := r.Activate(t.Context()); err == nil {
		t.Fatal("stale install activated")
	}
	if *n != nil || b.claims != 0 {
		t.Fatal("runtime started from stale snapshot")
	}
}
func TestHeartbeatPartitionStopsOwnerWithoutReclaimOrReplay(t *testing.T) {
	r, b, n := runnerFixture(t)
	if err := r.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	b.failHeartbeat = true
	if err := r.guard.maintain(t.Context(), b, time.Millisecond); err == nil {
		t.Fatal("partition left authority active")
	}
	if err := r.guard.WaitStopped(t.Context()); err != nil {
		t.Fatal(err)
	}
	if (*n).started != 1 || (*n).stopped.Load() != 1 || b.claims != 1 || b.releases != 0 {
		t.Fatal("partition replayed or reclaimed execution")
	}
	if err := r.Publish(t.Context()); !errors.Is(err, ErrFenced) {
		t.Fatal("fenced publisher wrote snapshot", err)
	}
}

type publicationBarrier struct {
	*runnerBroker
	native                             **runnerNative
	published, heartbeat               chan struct{}
	continuePublish, continueHeartbeat chan struct{}
}

func (b *publicationBarrier) PublishSnapshot(ctx context.Context, l nodeplane.Lease, ref nodeplane.SnapshotRef, payload []byte) error {
	if err := b.runnerBroker.PublishSnapshot(ctx, l, ref, payload); err != nil {
		return err
	}
	close(b.published)
	if b.continuePublish != nil {
		select {
		case <-b.continuePublish:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (b *publicationBarrier) Heartbeat(ctx context.Context, l nodeplane.Lease) (nodeplane.Lease, error) {
	close(b.heartbeat)
	if b.continueHeartbeat != nil {
		select {
		case <-b.continueHeartbeat:
		case <-ctx.Done():
			return nodeplane.Lease{}, ctx.Err()
		}
	}
	if (*b.native).ref != b.ref {
		return nodeplane.Lease{}, errors.New("broker and native marker differ")
	}
	return l, nil
}
func TestRunnerSerializesTrustedHeartbeatWithWholePublication(t *testing.T) {
	for _, publishFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "publication-first", false: "heartbeat-first"}[publishFirst], func(t *testing.T) {
			r, b, n := runnerFixture(t)
			barrier := &publicationBarrier{runnerBroker: b, native: n, published: make(chan struct{}), heartbeat: make(chan struct{})}
			if publishFirst {
				barrier.continuePublish = make(chan struct{})
			} else {
				barrier.continueHeartbeat = make(chan struct{})
			}
			r.opts.Broker = barrier
			if err := r.Activate(t.Context()); err != nil {
				t.Fatal(err)
			}
			defer r.guard.Revoke()
			lease, _ := r.guard.Lease()
			publishDone, heartbeatDone := make(chan error, 1), make(chan error, 1)
			publish := func() { publishDone <- r.Publish(t.Context()) }
			heartbeat := func() { _, err := r.Heartbeat(t.Context(), lease); heartbeatDone <- err }
			if publishFirst {
				go publish()
				<-barrier.published
				go heartbeat()
				select {
				case <-barrier.heartbeat:
					t.Fatal("heartbeat saw broker commit before native marker")
				case <-time.After(25 * time.Millisecond):
				}
				close(barrier.continuePublish)
			} else {
				go heartbeat()
				<-barrier.heartbeat
				go publish()
				select {
				case <-barrier.published:
					t.Fatal("publication bypassed active trusted proof read")
				case <-time.After(25 * time.Millisecond):
				}
				close(barrier.continueHeartbeat)
			}
			if err := <-publishDone; err != nil {
				t.Fatal(err)
			}
			if err := <-heartbeatDone; err != nil {
				t.Fatal(err)
			}
			if (*n).ref != b.ref {
				t.Fatal("native marker did not advance")
			}
		})
	}
}

type failingMarker struct{ *runnerNative }

func (n *failingMarker) SetNotebookSnapshot(context.Context, nodeplane.Lease, nodeplane.SnapshotRef) error {
	return errors.New("disk marker failure")
}
func TestRunnerMarkerFailureRevokesCommittedOwner(t *testing.T) {
	r, b, n := runnerFixture(t)
	if err := r.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.runtime = &failingMarker{*n}
	if err := r.Publish(t.Context()); err == nil {
		t.Fatal("committed marker failure was hidden")
	}
	if b.publishes != 1 {
		t.Fatal("fixture did not commit")
	}
	if _, active := r.guard.Lease(); active {
		t.Fatal("mismatched native proof retained effect admission")
	}
	if err := r.guard.WaitStopped(t.Context()); err != nil {
		t.Fatal(err)
	}
}
