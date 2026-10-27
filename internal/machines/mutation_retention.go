package machines

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const retainedMachineReceipts = 1024

func newMachineMutationID() string {
	return fmt.Sprintf("machine-%d-%s", time.Now().UnixMilli(), rand.Text())
}
func mutationTime(id string) (int64, bool) {
	rest, ok := strings.CutPrefix(id, "machine-")
	if !ok {
		return 0, false
	}
	stamp, nonce, ok := strings.Cut(rest, "-")
	if !ok || nonce == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(stamp, 10, 64)
	return n, err == nil && n > 0
}
func mutationExpired(id string, floor int64) bool {
	if floor == 0 {
		return false
	}
	stamp, ok := mutationTime(id)
	return !ok || stamp <= floor
}
func expiredMutation(id string) api.MachineMutationResult {
	// Outcome is unavailable, not a claim that the original operation failed.
	// The persisted admission floor guarantees that a delayed copy cannot run.
	return api.MachineMutationResult{ID: id, Status: "expired", Code: "machine_receipt_expired"}
}

// Only terminal receipts compact. Uncertain/unfinished cleanup stays owned by
// its original ID. One admission floor fences retired IDs; no tombstone ledger.
func (s *Service) compactMutationReceiptsLocked(keep string) {
	terminal := []api.MachineMutationResult{}
	for _, r := range s.state.Mutations {
		if !r.CleanupPending && (r.Status == "succeeded" || r.Status == "failed") {
			terminal = append(terminal, r)
		}
	}
	if len(terminal) <= retainedMachineReceipts {
		return
	}
	sort.Slice(terminal, func(i, j int) bool {
		if terminal[i].IssuedAt != terminal[j].IssuedAt {
			return terminal[i].IssuedAt < terminal[j].IssuedAt
		}
		return terminal[i].ID < terminal[j].ID
	})
	floor := min(time.Now().UnixMilli(), max(int64(1), terminal[len(terminal)-retainedMachineReceipts-1].IssuedAt))
	s.state.MutationFloor = max(s.state.MutationFloor, floor)
	for id, r := range s.state.Mutations {
		if id != keep && !r.CleanupPending && (r.Status == "succeeded" || r.Status == "failed") && r.IssuedAt <= floor {
			delete(s.state.Mutations, id)
		}
	}
}
