package nodeagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
)

func TestManagedDisableJournalCrashAndOriginalReceiptNeverReplay(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "control.json")
	r := ManagedDisableRequest{Target: api.WorkTarget{NodeID: "node", Backend: "codex", Role: api.RoleBot}, OperationID: "original-disable", Lease: nodeplane.Lease{BotID: "bot", NodeID: "node", Backend: api.NodeCodex, Epoch: "7", TTLMs: 43000}}
	j, err := OpenManagedDisableJournal(path, "node", "bot")
	if err != nil {
		t.Fatal(err)
	}
	if err = j.Begin(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	afterCrash, err := OpenManagedDisableJournal(path, "node", "bot")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := afterCrash.Lookup(t.Context(), r)
	if err != nil || receipt.Outcome != "unknown" || !afterCrash.Disabled() {
		t.Fatalf("interrupted original intent %+v %v", receipt, err)
	}
	if err = afterCrash.Begin(t.Context(), r); err == nil {
		t.Fatal("unknown original disable replayed")
	}
	ref := nodeplane.SnapshotRef{BotID: "bot", Epoch: "7", Version: "3", Digest: "complete"}
	if err = j.Finish(t.Context(), r, ref, "accepted"); err != nil {
		t.Fatal(err)
	}
	afterStop, err := OpenManagedDisableJournal(path, "node", "bot")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = afterStop.Lookup(t.Context(), r)
	if err != nil || receipt.Outcome != "accepted" || receipt.Snapshot != ref {
		t.Fatalf("durable original proof %+v %v", receipt, err)
	}
	changed := r
	changed.Lease.Epoch = "8"
	if _, err = afterStop.Lookup(t.Context(), changed); err == nil {
		t.Fatal("foreign epoch used original receipt")
	}
	changed = r
	changed.Lease.TTLMs = 1
	if _, err = afterStop.Lookup(t.Context(), changed); err != nil {
		t.Fatal("diagnostic TTL treated as grant scope", err)
	}
	if _, err = OpenManagedDisableJournal(path, "foreign", "bot"); err == nil {
		t.Fatal("foreign node adopted control journal")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = afterStop.Lookup(canceled, r); err == nil {
		t.Fatal("canceled receipt read succeeded")
	}
}
