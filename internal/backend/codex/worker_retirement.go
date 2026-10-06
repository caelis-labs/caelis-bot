package codex

import (
	"context"
	"errors"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/diagnosticlog"
)

// App Server's own unload grace starts only after the last connection leaves.
// Keep a short Bot-side quiet period so a just-completed worker can be read or
// continued without repeatedly tearing down its MCP session.
const workerRetirementDelay = 30 * time.Second
const workerSubscriptionLimit = 8

// Called with s.mu held. Historical terminal tasks need no native resume: the
// saved binding and read-only thread/read retain their original IDs and receipts.
func (s *Session) recoverUnresolvedChildren(c *Client, epoch uint64) {
	go s.observeNativeResources(c, epoch)
	for _, task := range s.binding.Tasks {
		if task == nil || task.Thread == "" {
			continue
		}
		if terminal(task.View.Status) && task.Pending == "" && !taskHasUnknownReceipt(task) {
			if s.childSubscribed[task.Thread] {
				s.scheduleChildRetirement(task.Thread)
			}
			continue
		}
		if s.childWatching[task.Thread] {
			continue
		}
		s.childWatching[task.Thread] = true
		go s.recoverChild(c, epoch, task.Thread)
	}
	for id := range s.children {
		if s.taskByThread(id) == nil && !s.childWatching[id] {
			s.childWatching[id] = true
			go s.recoverChild(c, epoch, id)
		}
	}
}

func taskHasUnknownReceipt(task *taskRecord) bool {
	for _, receipt := range task.Requests {
		if receipt.Outcome == "unknown" {
			return true
		}
	}
	return false
}

// Read does not load an unloaded persisted thread. Resume is reserved for a
// positively active thread (or explicit continuation), not a status label in
// the Bot ledger. A failed read leaves the original receipt unresolved.
func (s *Session) recoverChild(c *Client, epoch uint64, id string) bool {
	s.mu.Lock()
	if s.client != c || s.epoch != epoch || s.closed {
		s.mu.Unlock()
		return false
	}
	revision := s.childRevision[id]
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(s.life, 10*time.Second)
	defer cancel()
	var response struct {
		Thread nativeThread `json:"thread"`
	}
	err := callDecode(ctx, c, "thread/read", map[string]any{"threadId": id, "includeTurns": true}, &response)
	s.mu.Lock()
	if s.client != c || s.epoch != epoch || s.closed {
		s.mu.Unlock()
		return false
	}
	if s.childRevision[id] != revision {
		go s.recoverChild(c, epoch, id)
		s.mu.Unlock()
		return false
	}
	if err != nil || response.Thread.ID != id {
		delete(s.childWatching, id)
		if task := s.taskByThread(id); task != nil {
			task.View.Status = "unknown"
			_ = s.save()
		} else if _, knownActive := s.childRuns[id]; knownActive {
			s.state.Phase, s.state.Message = "unknown", workerUnconfirmed
		}
		s.update()
		s.mu.Unlock()
		return false
	}
	active := response.Thread.Status.Type == "active"
	latestTerminal := false
	latestRun := ""
	for _, turn := range response.Thread.Turns {
		if turn.ID != "" {
			latestRun = turn.ID
		}
		if task := s.taskByThread(id); task != nil {
			s.observeTaskTurn(task, turn)
		}
		if turn.Status == "inProgress" && !s.supersededTaskRun(id, turn.ID) {
			active = true
			s.childRuns[id] = turn.ID
		}
		latestTerminal = terminal(turn.Status)
		if terminal(turn.Status) {
			s.childTerminals[opaque(id, turn.ID)] = true
		}
	}
	subscribe := false
	if active {
		if _, known := s.childRuns[id]; !known {
			s.childRuns[id] = "" // Native active, turn identity not yet known.
		}
		if !s.childSubscribed[id] {
			subscribe = true
		}
	} else {
		delete(s.childRuns, id)
		if task := s.taskByThread(id); task != nil && task.View.Status == "working" {
			task.View.Status = "unknown"
		}
		if s.childSubscribed[id] {
			s.scheduleChildRetirement(id)
		} else {
			delete(s.childWatching, id)
		}
		// Idle and notLoaded are native execution facts; an unrecognized status
		// must not turn an uncertain task into a confirmed completion.
		if response.Thread.Status.Type != "idle" && response.Thread.Status.Type != "notLoaded" {
			if task := s.taskByThread(id); task != nil {
				task.View.Status = "unknown"
			}
		}
		confirmedIdle := (response.Thread.Status.Type == "idle" || response.Thread.Status.Type == "notLoaded") && latestTerminal
		if task := s.taskByThread(id); task != nil && (task.Pending != "" || taskHasUnknownReceipt(task) || task.Run == "" || task.Run != latestRun) {
			confirmedIdle = false
		}
		if confirmedIdle && s.state.Message == workerUnconfirmed {
			unresolved := false
			for child := range s.childRuns {
				if !s.childWatching[child] {
					unresolved = true
					break
				}
			}
			if !unresolved {
				s.state.Message = ""
				if s.run != "" || s.hasBlockingChildren() {
					s.state.Phase = "working"
				} else if terminal(s.runs[s.lastTurn]) {
					s.state.Phase = s.runs[s.lastTurn]
				} else {
					s.state.Phase = "idle"
				}
			}
		}
	}
	_ = s.save()
	s.update()
	s.mu.Unlock()
	if subscribe {
		return s.watchChild(c, epoch, id)
	}
	return true
}

// Connection recovery waits for exact active-thread subscriptions before the
// resident is exposed as ready. Terminal history remains read-only under #86.
func (s *Session) reconcileRecoveryWorkers(c *Client, epoch uint64) bool {
	s.mu.Lock()
	ids := make([]string, 0)
	seen := map[string]bool{}
	for _, task := range s.binding.Tasks {
		if task == nil || task.Thread == "" || seen[task.Thread] || (terminal(task.View.Status) && task.Pending == "" && !taskHasUnknownReceipt(task)) {
			continue
		}
		seen[task.Thread] = true
		ids = append(ids, task.Thread)
	}
	for id := range s.children {
		if !seen[id] && s.taskByThread(id) == nil {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		s.childWatching[id] = true
	}
	s.mu.Unlock()
	ok := true
	for _, id := range ids {
		if !s.recoverChild(c, epoch, id) {
			ok = false
		}
	}
	return ok
}

func (s *Session) childMayRetire(id string) bool {
	if !s.childSubscribed[id] || s.retireUnsupported || s.childRuns[id] != "" {
		return false
	}
	if _, active := s.childRuns[id]; active {
		return false
	}
	for _, prompt := range s.prompts {
		if prompt.thread == id {
			return false
		}
	}
	if task := s.taskByThread(id); task != nil {
		return task.Pending == "" && terminal(task.View.Status) && task.Run != "" && !taskHasUnknownReceipt(task)
	}
	// Generic children have no persisted task status; the exact terminal
	// turn is checked in thread/read before any unsubscribe.
	return true
}

// Called with s.mu held. Sequence changes invalidate a pending timer on any
// new turn, approval resolution, connection replacement or explicit resume.
func (s *Session) scheduleChildRetirement(id string) {
	s.childRetireSeq[id]++
	if !s.childMayRetire(id) {
		return
	}
	seq, c, epoch := s.childRetireSeq[id], s.client, s.epoch
	delay := s.workerIdleDelay
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			s.retireChild(c, epoch, id, seq)
		case <-s.life.Done():
		}
	}()
}

func (s *Session) retireChild(c *Client, epoch uint64, id string, seq uint64) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	if s.client != c || s.epoch != epoch || s.childRetireSeq[id] != seq || !s.childMayRetire(id) || s.state.Connection != "ready" || s.closed || s.closing {
		s.mu.Unlock()
		return
	}
	revision := s.childRevision[id]
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(s.life, 8*time.Second)
	defer cancel()
	var read struct {
		Thread nativeThread `json:"thread"`
	}
	if err := callDecode(ctx, c, "thread/read", map[string]any{"threadId": id, "includeTurns": true}, &read); err != nil || read.Thread.ID != id {
		s.retirementFailure(c, epoch, id, seq, "thread/read", err)
		return
	}
	if read.Thread.Status.Type != "idle" && read.Thread.Status.Type != "notLoaded" {
		return
	}
	var last nativeTurn
	for _, turn := range read.Thread.Turns {
		if turn.ID != "" {
			last = turn
		}
	}
	if !terminal(last.Status) {
		return
	}
	s.mu.Lock()
	if s.client != c || s.epoch != epoch || s.childRetireSeq[id] != seq || s.childRevision[id] != revision || !s.childMayRetire(id) {
		s.mu.Unlock()
		return
	}
	if task := s.taskByThread(id); task != nil && (task.Run != last.ID || !terminal(last.Status)) {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	var response struct {
		Status string `json:"status"`
	}
	if err := callDecode(ctx, c, "thread/unsubscribe", map[string]string{"threadId": id}, &response); err != nil {
		s.retirementFailure(c, epoch, id, seq, "thread/unsubscribe", err)
		return
	}
	if response.Status != "unsubscribed" && response.Status != "notSubscribed" && response.Status != "notLoaded" {
		s.retirementFailure(c, epoch, id, seq, "thread/unsubscribe", ErrProtocol)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != c || s.epoch != epoch {
		return
	}
	delete(s.childSubscribed, id)
	delete(s.childWatching, id)
	if response.Status == "unsubscribed" {
		s.childRetired[id] = true
		s.retiredWorkers++
	} else {
		delete(s.childRetired, id)
	}
	s.childRetireSeq[id]++
	// An event or approval may have arrived while the RPC was in flight.
	// Reattach the same native thread if it is now needed for observation.
	if s.childRevision[id] != revision {
		s.childWatching[id] = true
		go s.recoverChild(c, epoch, id)
	}
	s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "info", Component: "codex", Code: "worker_subscription_retired", Method: "thread/unsubscribe", Thread: id, Phase: response.Status})
	go s.observeNativeResources(c, epoch)
}

func (s *Session) retirementFailure(c *Client, epoch uint64, id string, seq uint64, method string, err error) {
	if err == nil {
		err = ErrProtocol
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != c || s.epoch != epoch || s.childRetireSeq[id] != seq {
		return
	}
	s.retireFailures++
	var native *NativeError
	if errors.As(err, &native) && native.Code == -32601 {
		s.retireUnsupported = true
	} else {
		s.scheduleChildRetirement(id)
	}
	s.opts.Diagnostics.Write(diagnosticlog.Record{Level: "error", Component: "codex", Code: "worker_retirement_failed", Method: method, Thread: id, Reason: diagnosticlog.Reason(err.Error())})
}

func (s *Session) subscribedWorkerCount() int { return len(s.childSubscribed) }
