package codex

import (
	"context"
	"slices"
)

// A native missing-loaded-thread error is not itself cleanup proof. Only an
// owned historical child, freshly read from the same connection as notLoaded,
// can be omitted from whole-owner cleanup. Explicit Stop targets never use this
// exception, and the caller still verifies the private process/tool cleanup.
func (s *Session) confirmUnloadedCleanupChild(ctx context.Context, c *Client, id string) bool {
	s.mu.Lock()
	epoch, revision, current := s.epoch, s.childRevision[id], s.binding.ThreadID
	task := s.taskByThread(id)
	eligible := s.cleanupChildQuiescentLocked(c, id)
	s.mu.Unlock()
	if !eligible {
		return false
	}
	thread, err := readThreadState(ctx, c, id)
	if err != nil || thread.Status.Type != "notLoaded" || nativeTaskActive(thread) {
		return false
	}
	var last nativeTurn
	if len(thread.Turns) != 0 {
		last = thread.Turns[len(thread.Turns)-1]
		if last.ID == "" || !terminal(last.Status) {
			return false
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != epoch || s.childRevision[id] != revision || s.binding.ThreadID != current ||
		s.taskByThread(id) != task || !s.cleanupChildQuiescentLocked(c, id) {
		return false
	}
	return task == nil || task.Run != "" && task.Run == last.ID
}

// Called with s.mu held. These fences do not settle any receipt or remove
// cleanup targets. Unknown work on other threads still blocks owner settlement.
func (s *Session) cleanupChildQuiescentLocked(c *Client, id string) bool {
	if s.closed || !s.bound || c == nil || c != s.client && c != s.retainedOwner ||
		id == "" || id == s.binding.ThreadID || !s.children[id] {
		return false
	}
	if _, active := s.childRuns[id]; active {
		return false
	}
	if slices.Contains(s.binding.CleanupTargets, id) {
		return false
	}
	if _, stopped := s.binding.StopRuns[id]; stopped {
		return false
	}
	if s.binding.CapacityStopThread == id && s.binding.CapacityStopRequest != "" && s.binding.CapacityStopStatus == "" {
		return false
	}
	for _, p := range s.prompts {
		if p.thread == id {
			return false
		}
	}
	if task := s.taskByThread(id); task != nil {
		return task.Pending == "" && !taskHasUnknownReceipt(task) && terminal(task.View.Status) &&
			task.Activity != "active" && (task.StopPendingRequest == "" || task.StopPendingStatus != "")
	}
	return true
}
