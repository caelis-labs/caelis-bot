package codex

import (
	"context"
	"errors"
	"time"
)

// InterruptTurn uses the same mutation lock as Submit and StartWork. It never
// reacquires Interrupt's lock or selects a newer run after validating expected.
func (s *Session) InterruptTurn(ctx context.Context, expected string, before func()) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	c, thread, run := s.client, s.binding.ThreadID, s.run
	if expected == "" || c == nil || run == "" || opaque(run) != expected || s.closed || s.closing || s.state.Connection != "ready" {
		s.mu.Unlock()
		return errors.New("observed turn is no longer active")
	}
	targets := map[string]string{thread: run}
	for id, childRun := range s.childRuns {
		if childRun == "" {
			s.mu.Unlock()
			return errors.New("owned background target unconfirmed")
		}
		targets[id] = childRun
	}
	for _, task := range s.binding.Tasks {
		task.SuppressReport = true
		task.ReportState = "observed"
	}
	if err := s.save(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	if before != nil {
		before()
	}
	ctx, cancel := s.operation(ctx, 20*time.Second)
	defer cancel()
	s.cancelPendingElicitations(ctx, c)
	c.captureTools()
	for id, target := range targets {
		if err := callDecode(ctx, c, "turn/interrupt", map[string]string{"threadId": id, "turnId": target}, nil); err != nil {
			return errors.New("exact interruption outcome unconfirmed")
		}
	}
	s.mu.Lock()
	s.state.Phase = "interrupting"
	s.update()
	s.mu.Unlock()
	for {
		s.mu.Lock()
		active := false
		changedTarget := s.run != "" && s.run != run
		for id, target := range targets {
			if id == thread {
				active = active || s.run == target
			} else {
				current, exists := s.childRuns[id]
				active = active || exists && current == target
				changedTarget = changedTarget || exists && current != target
			}
		}
		changed := s.changed
		s.mu.Unlock()
		if changedTarget {
			return errors.New("newer owned turn appeared during interruption")
		}
		if !active {
			break
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return errors.New("exact interruption terminal fact unconfirmed")
		case <-c.Done():
			return errors.New("connection lost before exact interruption terminal fact")
		}
	}
	cleanupErr := s.cleanTerminals(ctx, c)
	if c.UsesSharedServer() {
		s.mu.Lock()
		s.state.Phase = "interrupted"
		if cleanupErr != nil {
			s.state.Message = "工作已停止，但后台工具清理尚未确认"
		}
		s.update()
		s.mu.Unlock()
		return cleanupErr
	}
	// Match the existing owned-server cleanup boundary: capture descendants
	// before interrupt, clean native terminals, recycle only our process, then
	// restore observation of the same binding without replaying a turn.
	s.mu.Lock()
	s.client = nil
	s.epoch++
	s.state.Connection = "connecting"
	s.state.Phase = "interrupting"
	s.update()
	s.mu.Unlock()
	c.Close()
	if c.toolCleanupError() != nil {
		return s.connectionError("任务已中断，但工具清理未能确认，请重新连接核对", c.toolCleanupError())
	}
	if err := s.connect(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	if s.run == "" && s.state.Connection == "ready" {
		s.state.Phase = "interrupted"
	}
	s.update()
	s.mu.Unlock()
	return nil
}
