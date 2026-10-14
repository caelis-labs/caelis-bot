package codex

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/lockwait"
)

// RecoverUnavailableOwner is called only through the fenced manual recovery
// action. Ordinary Connect remains read-only with respect to the lost owner.
// The original IDs and receipts are archived before a fresh owner is started.
func (s *Session) RecoverUnavailableOwner(ctx context.Context) (bool, error) {
	ctx, cancel := s.operation(ctx, 30*time.Second)
	defer cancel()
	if err := lockwait.Lock(ctx, &s.op); err != nil {
		return true, err
	}
	defer s.op.Unlock()
	s.mu.Lock()
	if !s.binding.OwnerUnavailable {
		s.mu.Unlock()
		return false, nil
	}
	if s.closed || s.closing || s.client != nil || s.retainedOwner != nil || s.loadErr != nil {
		s.mu.Unlock()
		return true, errors.New("原 owner 恢复状态已变化，请重新读取后再操作")
	}
	old := s.binding
	record := lostOwnerRecord{
		Endpoint: old.OwnerEndpoint, Thread: old.ThreadID,
		LastReceipt: old.LastReceipt, Pending: old.Pending,
		StopRequest: old.CapacityStopRequest, StopThread: old.CapacityStopThread,
		StopTurn: old.CapacityStopTurn, StopPhase: old.CapacityStopPhase,
		StopStatus: old.CapacityStopStatus, StopAccepted: old.CapacityStopAccepted,
		StopState: old.StopState, StopRuns: maps.Clone(old.StopRuns),
		CleanupTargets: slices.Clone(old.CleanupTargets), Children: slices.Clone(old.Children),
		PastThreads: slices.Clone(old.PastThreads), ApprovalIDs: slices.Clone(old.PendingApprovalIDs),
		ToolRenewal: old.ToolRenewal,
	}
	staged := old
	staged.LostOwners = append(slices.Clone(old.LostOwners), record)
	staged.LostOwnerTasks = maps.Clone(old.LostOwnerTasks)
	if staged.LostOwnerTasks == nil {
		staged.LostOwnerTasks = map[string]bool{}
	}
	for id := range old.Tasks {
		if old.LostOwnerTasks[id] {
			continue
		}
		staged.LostOwnerTasks[id] = true
		staged.LostOwners[len(staged.LostOwners)-1].TaskIDs = append(staged.LostOwners[len(staged.LostOwners)-1].TaskIDs, id)
	}
	slices.Sort(staged.LostOwners[len(staged.LostOwners)-1].TaskIDs)
	staged.OwnerUnavailable, staged.OwnerEndpoint = false, ""
	staged.ThreadID, staged.Pending, staged.LastReceipt = "", nil, nil
	staged.CapacityStopRequest, staged.CapacityStopThread, staged.CapacityStopTurn = "", "", ""
	staged.CapacityStopPhase, staged.CapacityStopStatus, staged.CapacityStopAccepted = "", "", false
	staged.StopState, staged.StopRuns, staged.CleanupTargets = "", nil, nil
	staged.Children, staged.PastThreads, staged.PendingApprovalIDs = nil, nil, nil
	staged.ToolRenewal = nil
	staged.Unsubmitted = false
	staged.DelegationText = ""
	s.binding = staged
	if err := s.save(); err != nil {
		s.binding = old
		s.mu.Unlock()
		return true, err
	}
	s.state.ConnectionIssue = ""
	s.state.Phase = "idle"
	s.state.Message = "原 owner 的未确认结果已保留；正在建立新连接"
	s.update()
	s.mu.Unlock()
	return true, s.connect(ctx)
}
