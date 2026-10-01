package workerlease

import (
	"context"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type readerFixture struct {
	mu             sync.Mutex
	err            error
	block, entered chan struct{}
	ttl            int64
	lease          *nodeplane.Lease
}

func (r *readerFixture) ReadWorkerLease(ctx context.Context, ref nodeplane.WorkLeaseRef) (nodeplane.Lease, error) {
	r.mu.Lock()
	err, block, entered := r.err, r.block, r.entered
	ttl, lease := r.ttl, r.lease
	r.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if block != nil {
		select {
		case <-ctx.Done():
			return nodeplane.Lease{}, ctx.Err()
		case <-block:
		}
	}
	if err != nil {
		return nodeplane.Lease{}, err
	}
	if lease != nil {
		return *lease, nil
	}
	if ttl == 0 {
		ttl = 60000
	}
	return nodeplane.Lease{BotID: ref.BotID, NodeID: ref.SourceNode, Backend: ref.SourceBackend, Epoch: ref.Epoch, TTLMs: ttl, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

type sourceFixture struct{ s api.WorkDispatchSource }

func (s sourceFixture) WorkDispatchSource(context.Context) (api.WorkDispatchSource, error) {
	return s.s, nil
}
func fixture(t *testing.T) (*Fence, *readerFixture, *atomic.Int32, api.WorkDispatchSource) {
	t.Helper()
	r := &readerFixture{}
	stops := &atomic.Int32{}
	s := api.WorkDispatchSource{NodeID: "actual-primary", Backend: "codex", BindingID: "opaque-binding", OperationID: "native-op", Kind: "native_activation", Lease: api.WorkerLeaseGrant{BotID: "raw-stable-bot", BrokerNodeID: "pinned-broker", SourceNodeID: "actual-primary", Backend: "codex", Epoch: "first-epoch"}}
	f, err := New(Options{BrokerNodeID: "pinned-broker", BotID: "raw-stable-bot", SourceNode: "actual-primary", SourceBackend: "codex", Reader: r, Source: sourceFixture{s}, Ready: func(context.Context) error { return nil }, Renew: func(ctx context.Context, epoch string, deadline time.Time) error {
		if epoch != s.Lease.Epoch || time.Until(deadline) > 45*time.Second {
			return errors.New("bad native deadline")
		}
		return ctx.Err()
	}, Stop: func(context.Context) error { stops.Add(1); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close(context.Background()) })
	return f, r, stops, s
}
func TestQueuedWorkerEffectCannotPassLeaseLoss(t *testing.T) {
	f, r, stops, _ := fixture(t)
	ctx, release, err := f.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	r.mu.Lock()
	r.block = make(chan struct{})
	r.entered = make(chan struct{}, 1)
	entered := r.entered
	r.mu.Unlock()
	var effects atomic.Int32
	done := make(chan error, 1)
	go func() {
		err := f.CheckContext(ctx)
		if err == nil {
			effects.Add(1)
		}
		done <- err
	}()
	<-entered
	f.Revoke()
	if err = <-done; err == nil {
		t.Fatal("lost lease dispatched native effect")
	}
	if effects.Load() != 0 || stops.Load() != 1 || f.Capability() {
		t.Fatal("native admission did not revoke exactly once")
	}
}
func TestBrokerFailureStopsRunningWorkerAndEpochCannotChange(t *testing.T) {
	f, r, stops, s := fixture(t)
	if err := f.Admit(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.err = errors.New("broker partition")
	r.mu.Unlock()
	if err := f.Admit(t.Context(), s); err == nil {
		t.Fatal("broker loss accepted")
	}
	if stops.Load() != 1 || f.Capability() {
		t.Fatal("running worker retained admission after failed confirmation")
	}
	s.Lease.Epoch = "new-epoch"
	if err := f.Admit(t.Context(), s); err == nil {
		t.Fatal("old native generation admitted new epoch")
	}
}
func TestForeignBrokerCannotSupplyNativeAuthority(t *testing.T) {
	f, _, stops, s := fixture(t)
	s.Lease.BrokerNodeID = "renderer-selected-broker"
	if err := f.Admit(t.Context(), s); err == nil {
		t.Fatal("frame changed broker provenance")
	}
	if stops.Load() != 0 {
		t.Fatal("invalid frame destroyed a prepared native owner")
	}
}

func TestExpiryCancelsTicketsAndStopsNativeOwner(t *testing.T) {
	f, r, stops, _ := fixture(t)
	r.mu.Lock()
	r.ttl = 15100
	r.mu.Unlock()
	ctx, release, err := f.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expiry left live execution ticket")
	}
	if err := f.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if stops.Load() != 1 || f.Capability() {
		t.Fatal("expiry retained native ownership")
	}
}
func TestBrokerReplyCannotChangeSourceIdentity(t *testing.T) {
	for _, field := range []string{"bot", "source", "backend", "epoch", "missing-expiry", "ttl"} {
		t.Run(field, func(t *testing.T) {
			f, r, stops, s := fixture(t)
			reply := nodeplane.Lease{BotID: s.Lease.BotID, NodeID: s.NodeID, Backend: api.NodeBackend(s.Backend), Epoch: s.Lease.Epoch, ExpiresAt: time.Now().Add(time.Hour), TTLMs: 60000}
			switch field {
			case "bot":
				reply.BotID = "transport-hashed-bot"
			case "source":
				reply.NodeID = "another-primary"
			case "backend":
				reply.Backend = api.NodeCaelis
			case "epoch":
				reply.Epoch = "replacement-epoch"
			case "missing-expiry":
				reply.ExpiresAt = time.Time{}
			case "ttl":
				reply.TTLMs = 60001
			}
			r.mu.Lock()
			r.lease = &reply
			r.mu.Unlock()
			if err := f.Admit(t.Context(), s); err == nil {
				t.Fatal("foreign broker lease accepted")
			}
			if stops.Load() != 1 {
				t.Fatal("invalid lease retained native owner")
			}
		})
	}
}
func TestNativeDeadlineFailureIsVisibleAndStopsOwner(t *testing.T) {
	f, _, stops, s := fixture(t)
	failure := errors.New("independent watchdog acknowledgment failed")
	f.opts.Renew = func(context.Context, string, time.Time) error { return failure }
	if err := f.Admit(t.Context(), s); !errors.Is(err, failure) {
		t.Fatal("watchdog error hidden", err)
	}
	if !errors.Is(f.Err(), failure) {
		t.Fatal("watchdog failure disappeared after revocation")
	}
	if stops.Load() != 1 {
		t.Fatal("watchdog failure retained native owner")
	}
}
