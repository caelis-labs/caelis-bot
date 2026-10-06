package codex

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"
)

// lockUpdateOperation bounds the handoff behind an in-flight native request.
// No update may detach while its original dispatch result is still being
// recorded under op.
func (s *Session) lockUpdateOperation(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.op.TryLock() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待原请求回执保存超时: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (s *Session) rememberNativeOwner(c *Client) error {
	endpoint, ok := c.rpc.conn.(interface{ terminalEndpoint() string })
	if !ok || endpoint.terminalEndpoint() == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.binding.OwnerEndpoint
	s.binding.OwnerEndpoint = endpoint.terminalEndpoint()
	if err := s.save(); err != nil {
		s.binding.OwnerEndpoint = previous
		return err
	}
	return nil
}

// prepareDetachLocked persists the exact endpoint and all original request
// receipts while op excludes new submissions. Approval request IDs are kept as
// audit references only; a future observer must receive fresh native requests.
func (s *Session) prepareDetachLocked() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return s.loadErr
	}
	if s.closed || s.closing {
		return errors.New("连接已关闭，无法准备更新")
	}
	previousEndpoint := s.binding.OwnerEndpoint
	previousApprovals := slices.Clone(s.binding.PendingApprovalIDs)
	if c := s.client; c != nil && c.Err() == nil {
		if endpoint, ok := c.rpc.conn.(interface{ terminalEndpoint() string }); ok && endpoint.terminalEndpoint() != "" {
			s.binding.OwnerEndpoint = endpoint.terminalEndpoint()
		}
	}
	for _, p := range s.prompts {
		if p.view.Status == "pending" || p.view.Status == "sending" || p.view.Status == "unknown" {
			id := string(p.id)
			if !slices.Contains(s.binding.PendingApprovalIDs, id) {
				s.binding.PendingApprovalIDs = append(s.binding.PendingApprovalIDs, id)
			}
		}
	}
	needsOwner := s.run != "" || s.hasBlockingChildren() || s.hasUnresolvedTasks() || s.binding.Pending != nil || len(s.prompts) > 0 || s.state.Phase == "unknown" || s.state.Phase == "sending" || (s.binding.LastReceipt != nil && s.binding.LastReceipt.Outcome == "unknown")
	if needsOwner && (s.binding.OwnerEndpoint == "" || s.opts.StateFile == "") {
		s.binding.OwnerEndpoint, s.binding.PendingApprovalIDs = previousEndpoint, previousApprovals
		return errors.New("原生 owner 入口或持久化文件不可用，活跃工作仍留在当前进程")
	}
	if err := s.save(); err != nil {
		s.binding.OwnerEndpoint, s.binding.PendingApprovalIDs = previousEndpoint, previousApprovals
		return fmt.Errorf("原 owner 与待确认请求回执保存失败: %w", err)
	}
	return nil
}

// PrepareDetachForUpdate is the reversible update preflight. Installation can
// still be cancelled after this call; native execution remains untouched.
func (s *Session) PrepareDetachForUpdate(ctx context.Context) error {
	if err := s.lockUpdateOperation(ctx); err != nil {
		return err
	}
	defer s.op.Unlock()
	return s.prepareDetachLocked()
}

// DetachForUpdate closes only this observer transport. It neither interrupts a
// turn nor invokes the retained owner's stop closure. A new process reconnects
// to binding.OwnerEndpoint and reads the original thread and task receipts.
func (s *Session) DetachForUpdate(ctx context.Context) error {
	if err := s.lockUpdateOperation(ctx); err != nil {
		return err
	}
	defer s.op.Unlock()
	if err := s.prepareDetachLocked(); err != nil {
		return err
	}
	s.mu.Lock()
	s.closing = true
	s.closed = true
	s.reconnectSeq++
	if s.reconnectCancel != nil {
		s.reconnectCancel()
	}
	s.cancelLife()
	c := s.client
	if c == nil {
		c = s.retainedOwner
	}
	s.client = nil
	s.state.Connection = "offline"
	s.update()
	s.mu.Unlock()
	if c != nil {
		c.detachForReconnect()
	}
	return nil
}
