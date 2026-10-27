package machines

import (
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
	"time"
)

func TestRetiredMutationIDsNeverReplayAfterRestart(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.state.Profiles["original"] = profile{View: api.Machine{ID: "original", Name: "fedora"}, Label: "fedora"}
	base := time.Now().Add(-time.Hour).UnixMilli()
	first := fmt.Sprintf("machine-%d-first", base)
	s.state.Mutations[first] = api.MachineMutationResult{ID: first, Status: "failed", Code: "machine_not_submitted", IssuedAt: base}
	for i := 1; i <= retainedMachineReceipts; i++ {
		id := fmt.Sprintf("machine-%d-op", base+int64(i))
		s.state.Mutations[id] = api.MachineMutationResult{ID: id, Status: "failed", IssuedAt: base + int64(i)}
	}
	pendingID := "pending-cleanup"
	s.state.Mutations[pendingID] = api.MachineMutationResult{ID: pendingID, Status: "checking", CleanupPending: true, Machine: removedMachine("gone")}
	s.compactMutationReceiptsLocked("")
	if len(s.state.Mutations) > retainedMachineReceipts+1 {
		t.Fatal(len(s.state.Mutations))
	}
	if _, ok := s.state.Mutations[pendingID]; !ok {
		t.Fatal("unfinished cleanup retired")
	}
	if err = s.save(); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.MachineMutation(first); got.Status != "expired" {
		t.Fatal(got)
	}
	if got := s.ManageMachine(api.MachineMutation{ID: first, MachineID: "original", Action: "remove"}); got.Status != "expired" {
		t.Fatal(got)
	}
	if len(s.Machines()) != 1 {
		t.Fatal("delayed retired operation deleted connection")
	}
	if got := s.ManageMachine(api.MachineMutation{ID: newMachineMutationID(), MachineID: "original", Action: "save", Name: "fedora-new"}); got.Status != "succeeded" {
		t.Fatal(got)
	}
}
