package codex

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
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
	return s.recoverUnavailableOwner(ctx, false)
}

// The caller holds op. Worker-only explicit recovery shares the native archive
// contract with resident recovery; it does not migrate any existing task.
func (s *Session) recoverUnavailableOwner(ctx context.Context, newWork bool) (bool, error) {
	s.mu.Lock()
	if !s.binding.OwnerUnavailable && !s.binding.OwnerClosing {
		s.mu.Unlock()
		return false, nil
	}
	if s.closed || s.closing || s.client != nil || s.retainedOwner != nil || s.loadErr != nil {
		s.mu.Unlock()
		return true, errors.New("原 owner 恢复状态已变化，请重新读取后再操作")
	}
	wasClosing := s.binding.OwnerClosing
	life, err := s.reconcilePrivateOwnerLocked()
	if err != nil {
		s.update()
		s.mu.Unlock()
		return true, err
	}
	if life == ownerAlive {
		s.update()
		s.mu.Unlock()
		return true, s.connect(ctx) // The exact original owner survived.
	}
	if wasClosing {
		s.update()
		s.mu.Unlock()
		return true, errors.New("原 owner 已确认退出；请查看状态并明确选择建立新连接")
	}
	old := s.binding
	record := lostOwnerRecord{
		Endpoint: old.OwnerEndpoint, Process: old.OwnerProcess, Thread: old.ThreadID,
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
	staged.OwnerUnavailable, staged.OwnerClosing, staged.OwnerEndpoint, staged.OwnerProcess = false, false, "", nil
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
	if newWork {
		// The host may have explicitly pinned the private endpoint it recorded
		// before admission. Only this verified, archived process permits clearing
		// that stale startup hint. Shared endpoints never enter this branch.
		if old.OwnerProcess.valid() && s.opts.Socket == strings.TrimPrefix(old.OwnerEndpoint, "unix://") {
			s.opts.Socket, s.opts.RequiredSocket = "", false
		}
		s.resetProjection()
	}
	s.ownerUnavailableVerified = false
	s.state.ConnectionIssue = ""
	s.state.Phase = "idle"
	s.state.Message = "原 owner 的未确认结果已保留；正在建立新连接"
	s.update()
	s.mu.Unlock()
	return true, s.connect(ctx)
}

func (s *Session) recoverNewWork(ctx context.Context) error {
	ctx, cancel := s.operation(ctx, 30*time.Second)
	defer cancel()
	if err := lockwait.Lock(ctx, &s.op); err != nil {
		return err
	}
	defer s.op.Unlock()
	s.mu.Lock()
	if !s.workerOnly || s.closed || s.closing || s.loadErr != nil {
		s.mu.Unlock()
		return errors.New("original Worker recovery state unavailable")
	}
	if !s.binding.OwnerProcess.valid() {
		// No private process proof means ordinary shared-server reconnect only.
		// A failed endpoint or unreadable PID must never authorize replacement.
		privateUnknown := s.binding.OwnerClosing || s.binding.OwnerUnavailable || s.retainedOwner != nil && !s.retainedOwner.UsesSharedServer()
		s.mu.Unlock()
		if privateUnknown {
			return errors.New("original Worker process identity unavailable")
		}
		return s.connect(ctx)
	}
	life, err := s.reconcilePrivateOwnerLocked()
	if err != nil || life != ownerGone {
		s.update()
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return s.connect(ctx) // Exact original private process is still alive.
	}
	s.mu.Unlock()
	// Capture native pending IDs before the old event projection is detached.
	if err := s.prepareDetachLocked(); err != nil {
		return err
	}
	s.mu.Lock()
	old, retained := s.client, s.retainedOwner
	s.client, s.retainedOwner = nil, nil
	s.epoch++ // Old listeners and child reads cannot publish into the new owner.
	s.reconnectSeq++
	if s.reconnectCancel != nil {
		s.reconnectCancel()
	}
	s.reconnectActive, s.reconnectCancel = false, nil
	s.state.Connection = "offline"
	s.update()
	s.mu.Unlock()
	if old != nil {
		old.detachForReconnect()
	}
	if retained != nil && retained != old {
		retained.detachForReconnect()
	}
	_, err = s.recoverUnavailableOwner(ctx, true)
	return err
}
