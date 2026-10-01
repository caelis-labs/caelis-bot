//go:build darwin || linux

package codex

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

type leaseReaderFixture struct {
	mu    sync.Mutex
	lease nodeplane.Lease
	err   error
	calls int
}

func (r *leaseReaderFixture) ReadWorkerLease(ctx context.Context, ref nodeplane.WorkLeaseRef) (nodeplane.Lease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.lease, r.err
}
func leaseFixtureGrant() api.WorkerLeaseGrant {
	return api.WorkerLeaseGrant{BotID: "bot", BrokerNodeID: "broker", SourceNodeID: "host", Backend: "codex", Epoch: "epoch-1"}
}
func TestWorkerLeaseDeadlineAndOriginalReceiptRecovery(t *testing.T) {
	d := workerPair(t)
	d.startUnknown = true
	in, view, err := d.start(t, 92)
	if err == nil || view.Outcome != "unknown" {
		t.Fatal("unknown fixture intent missing", view, err)
	}
	grant := api.WorkerLeaseGrant{BotID: "bot", BrokerNodeID: "broker", SourceNodeID: "host-node", Backend: "codex", Epoch: "epoch"}
	reader := &leaseReaderFixture{lease: nodeplane.Lease{BotID: "bot", NodeID: "host-node", Backend: api.NodeCodex, Epoch: "epoch", TTLMs: 60000}}
	d.w.lease = newWorkerLeaseFence(d.w, WorkerLeaseOptions{BrokerNodeID: "broker", RawBotID: "bot", SourceNode: "host-node", SourceBackend: "codex", Reader: reader, BindPower: func(context.Context, func(), func()) (func(), error) { return func() {}, nil }})
	f := d.w.lease
	f.enabled = true
	f.renew = func(context.Context, string, time.Time) error { return nil }
	if err = f.check(testContext(t), grant); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	deadline := f.deadline
	f.mu.Unlock()
	if remaining := time.Until(deadline); remaining > 45*time.Second || remaining < 44*time.Second {
		t.Fatal("remaining TTL not conservatively measured", remaining)
	}
	f.mu.Lock()
	f.deadline = time.Now().Add(-time.Millisecond)
	f.mu.Unlock()
	go f.maintain()
	select {
	case <-f.life.Done():
	case <-time.After(time.Second):
		t.Fatal("expired grant retained native admission")
	}
	recovered, recoveryErr := d.w.StartWork(testContext(t), in)
	if recoveryErr != nil || recovered.ID != view.ID || recovered.Outcome != "unknown" {
		t.Fatal("original retained receipt became unavailable", recovered, recoveryErr)
	}
	d.f.mu.Lock()
	starts := d.starts
	d.f.mu.Unlock()
	if starts != 1 {
		t.Fatal("unknown original intent redispatched", starts)
	}
}

func TestLeasedWorkerClosePreservesUnverifiedOwnedStopError(t *testing.T) {
	d := workerPair(t)
	// The protocol fixture owns no independently captured native process tree.
	// A successful socket close cannot substitute for that missing stop proof.
	d.w.lease = newWorkerLeaseFence(d.w, WorkerLeaseOptions{})
	err := d.w.Close(testContext(t))
	if err == nil || !strings.Contains(err.Error(), "Worker owned process fence unavailable") {
		t.Fatalf("unverified owned stop error hidden: %v", err)
	}
	if err = d.w.Close(testContext(t)); err == nil {
		t.Fatal("repeated close forgot original unknown stop")
	}
}
