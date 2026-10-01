//go:build darwin || linux

package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

type roamingEpochFixture struct {
	mu    sync.Mutex
	lease nodeplane.Lease
}

func (r *roamingEpochFixture) CurrentLease(context.Context, string) (nodeplane.Lease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	lease := r.lease
	lease.ExpiresAt = time.Now().Add(time.Minute)
	return lease, nil
}
func (r *roamingEpochFixture) ReadWorkerLease(ctx context.Context, ref nodeplane.WorkLeaseRef) (nodeplane.Lease, error) {
	return r.CurrentLease(ctx, ref.BotID)
}
func (r *roamingEpochFixture) WorkDispatchSource(ctx context.Context) (api.WorkDispatchSource, error) {
	l, _ := r.CurrentLease(ctx, "")
	return api.WorkDispatchSource{NodeID: l.NodeID, Backend: string(l.Backend), BindingID: "actual-native-binding", OperationID: "actual-native-operation-" + l.Epoch, Kind: "native_activation", Lease: api.WorkerLeaseGrant{BotID: l.BotID, BrokerNodeID: "paired-broker", SourceNodeID: l.NodeID, Backend: string(l.Backend), Epoch: l.Epoch}}, nil
}
func TestRoamingWorkerOriginalLedgerRequiresConfirmedStop(t *testing.T) {
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: "worker-node", Backend: "codex", Role: api.RoleWorker}, BotID: "bot", SourceNode: "main-node", SourceBackend: "codex"}
	book, e := openRoamingWorkerOrigins(t.TempDir(), pair)
	if e != nil {
		t.Fatal(e)
	}
	if e = book.begin("generation-1", "epoch-1"); e != nil {
		t.Fatal(e)
	}
	if e = book.begin("generation-2", "epoch-2"); e == nil {
		t.Fatal("unknown prior owned stop permitted new generation")
	}
	if e = book.state("generation-1", "stopped"); e != nil {
		t.Fatal(e)
	}
	if e = book.begin("generation-2", "epoch-2"); e != nil {
		t.Fatal(e)
	}
}

func TestRoamingWorkerUntrackedPreviousOwnerCannotBeReplaced(t *testing.T) {
	directory := t.TempDir()
	pair := workerwire.Pair{Target: api.WorkTarget{NodeID: "worker-node", Backend: "codex", Role: api.RoleWorker}, BotID: "bot", SourceNode: "main-node", SourceBackend: "codex"}
	if e := os.WriteFile(filepath.Join(directory, "worker-bindings.json"), []byte(`{"retained":"unknown"}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := openRoamingWorkerOrigins(directory, pair); e == nil {
		t.Fatal("untracked original native receipts silently replaced")
	}
}
