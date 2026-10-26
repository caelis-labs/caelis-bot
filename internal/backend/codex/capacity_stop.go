package codex

import (
	"context"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/lockwait"
)

// The original stop request is durable before this scheduler runs. A native
// acceptance only supplies an exact target; it never authorizes another Turn.
// Called with s.mu held, after the binding has been written successfully.
func (s *Session) queueCapacityStopsLocked() {
	if s.client == nil || s.state.Connection != "ready" || s.closed || s.closing || s.binding.OwnerUnavailable {
		return
	}
	if s.capacityStopReconciling == nil {
		s.capacityStopReconciling = map[string]bool{}
	}
	queue := func(key, thread, request, turn string) {
		if s.capacityStopReconciling[key] {
			return
		}
		s.capacityStopReconciling[key] = true
		go s.reconcileCapacityStop(key, thread, request, turn)
	}
	if s.binding.CapacityStopRequest != "" && s.binding.CapacityStopTurn != "" && s.binding.CapacityStopStatus == "" &&
		s.binding.CapacityStopThread != "" && s.binding.CapacityStopAccepted {
		queue("bot", s.binding.CapacityStopThread, s.binding.CapacityStopRequest, s.binding.CapacityStopTurn)
	}
	for id, task := range s.binding.Tasks {
		if task != nil && !s.binding.LostOwnerTasks[id] && task.Thread != "" && task.StopPendingRequest != "" && task.StopPendingTurn != "" && task.StopPendingStatus == "" &&
			task.Requests[task.StopPendingRequest].Outcome == "accepted" {
			queue("task:"+id, task.Thread, task.StopPendingRequest, task.StopPendingTurn)
		}
	}
}

// A terminal observation is only reportable after its binding is durable.
// Failed writes retain the native fact in memory and retry the same local
// write; they never dispatch another native request.
func (s *Session) markStopTerminalSavePendingLocked(key string) {
	if s.stopTerminalSavePending == nil {
		s.stopTerminalSavePending = map[string]bool{}
	}
	s.stopTerminalSavePending[key] = true
}

func (s *Session) retryStopTerminalSaveLocked() {
	if len(s.stopTerminalSavePending) == 0 || s.stopTerminalSaveRetrying || s.closed || s.closing {
		return
	}
	s.stopTerminalSaveRetrying = true
	go func() {
		defer func() {
			s.mu.Lock()
			s.stopTerminalSaveRetrying = false
			// Another terminal can arrive after the successful write but before
			// this retry exits. Its failed write still needs a retry owner.
			s.retryStopTerminalSaveLocked()
			s.mu.Unlock()
		}()
		for {
			select {
			case <-s.life.Done():
				return
			case <-time.After(350 * time.Millisecond):
			}
			s.mu.Lock()
			if s.closed || s.closing || len(s.stopTerminalSavePending) == 0 {
				s.mu.Unlock()
				return
			}
			if s.save() == nil {
				s.update()
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
		}
	}()
}

// One original target gets at most one durable interrupt attempt. An unknown
// interrupt result is observed on the original thread, never replayed.
func (s *Session) reconcileCapacityStop(key, threadID, request, turnID string) {
	defer func() {
		s.mu.Lock()
		delete(s.capacityStopReconciling, key)
		// A reconnect may have reached ready while the old observer was still
		// exiting. Hand the saved intent to that owner exactly once.
		s.queueCapacityStopsLocked()
		s.mu.Unlock()
	}()
	for {
		if !s.reconcileCapacityStopOnce(key, threadID, request, turnID) {
			return
		}
		select {
		case <-s.life.Done():
			return
		case <-time.After(350 * time.Millisecond):
		}
	}
}

// Read-only observations continue after an interrupt acknowledgement. The
// original Turn's terminal status, or Session close, ends this observer.
func (s *Session) reconcileCapacityStopOnce(key, threadID, request, turnID string) bool {
	ctx, cancel := s.operation(context.Background(), 20*time.Second)
	defer cancel()
	if err := lockwait.Lock(ctx, &s.op); err != nil {
		return ctx.Err() == nil
	}
	defer s.op.Unlock()
	s.mu.Lock()
	phase, c, epoch, valid := s.capacityStopTargetLocked(key, threadID, request, turnID)
	s.mu.Unlock()
	if !valid || c == nil {
		return false
	}
	thread, err := readThreadState(ctx, c, threadID)
	if err != nil || thread.ID != threadID {
		return true
	}
	var observed nativeTurn
	for _, turn := range thread.Turns {
		if turn.ID == turnID {
			observed = turn
		}
	}
	if observed.ID == "" || observed.Status == "" {
		return true // A different or unidentified latest Turn is never a stop target.
	}
	s.mu.Lock()
	currentPhase, _, currentEpoch, stillValid := s.capacityStopTargetLocked(key, threadID, request, turnID)
	if !stillValid || s.client != c || currentEpoch != epoch {
		s.mu.Unlock()
		return false
	}
	if terminal(observed.Status) {
		s.settleCapacityStopLocked(key, threadID, observed)
		_ = s.save()
		s.update()
		s.mu.Unlock()
		return false
	}
	if observed.Status != "inProgress" || phase != "requested" || currentPhase != "requested" ||
		(key == "bot" && terminal(s.runs[turnID])) || (key != "bot" && s.childTerminalStatus[opaque(threadID, turnID)] != "") {
		s.mu.Unlock()
		return true
	}
	if key == "bot" {
		s.binding.CapacityStopPhase = "attempted"
	} else {
		s.binding.Tasks[key[len("task:"):]].StopPendingPhase = "attempted"
	}
	if err := s.save(); err != nil {
		if key == "bot" {
			s.binding.CapacityStopPhase = "requested"
		} else {
			s.binding.Tasks[key[len("task:"):]].StopPendingPhase = "requested"
		}
		s.mu.Unlock()
		return true
	}
	s.update()
	s.mu.Unlock()
	err = callDecode(ctx, c, "turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID}, nil)
	s.mu.Lock()
	if currentPhase, _, currentEpoch, stillValid = s.capacityStopTargetLocked(key, threadID, request, turnID); stillValid &&
		s.client == c && currentEpoch == epoch && currentPhase == "attempted" && err == nil {
		if key == "bot" {
			s.binding.CapacityStopPhase = "acknowledged"
		} else {
			s.binding.Tasks[key[len("task:"):]].StopPendingPhase = "acknowledged"
		}
		_ = s.save()
		s.update()
	}
	s.mu.Unlock()
	return true
}

// Returns a phase only for the saved original request, native thread and Turn.
// Called with s.mu held.
func (s *Session) capacityStopTargetLocked(key, threadID, request, turnID string) (string, *Client, uint64, bool) {
	if s.closed || s.closing || s.state.Connection != "ready" {
		return "", nil, 0, false
	}
	if key == "bot" {
		if s.binding.CapacityStopThread != threadID || !s.binding.CapacityStopAccepted || s.binding.CapacityStopRequest != request || s.binding.CapacityStopTurn != turnID || s.binding.CapacityStopStatus != "" {
			return "", nil, 0, false
		}
		phase := s.binding.CapacityStopPhase
		if phase == "" { // saved by an earlier version before interrupt phases existed
			phase = "requested"
		}
		return phase, s.client, s.epoch, true
	}
	t := s.binding.Tasks[key[len("task:"):]]
	if t == nil || t.Thread != threadID || t.StopPendingRequest != request || t.StopPendingTurn != turnID || t.StopPendingStatus != "" {
		return "", nil, 0, false
	}
	phase := t.StopPendingPhase
	if phase == "" {
		phase = "requested"
	}
	return phase, s.client, s.epoch, true
}

func (s *Session) settleCapacityStopLocked(key, threadID string, turn nativeTurn) {
	if key == "bot" {
		if s.binding.ThreadID == threadID {
			s.applyTurn(threadID, turn, true)
		}
		s.binding.CapacityStopStatus = turn.Status
		return
	}
	if task := s.binding.Tasks[key[len("task:"):]]; task != nil && task.Thread == threadID {
		s.observeTaskTurn(task, turn, true)
		task.StopPendingStatus = turn.Status
	}
}
