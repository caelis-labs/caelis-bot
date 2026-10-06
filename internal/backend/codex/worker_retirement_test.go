package codex

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func waitWorkerRetired(t *testing.T, s *Session, id string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		s.mu.Lock()
		retired := !s.childSubscribed[id] && !s.childWatching[id]
		s.mu.Unlock()
		if retired {
			return
		}
		select {
		case <-deadline:
			t.Fatal("worker subscription was not retired", id)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestCompletedWorkerRetiresOnlyBotSubscriptionAndContinuesOriginalThread(t *testing.T) {
	s, f, d, manager := taskPair(t)
	s.workerIdleDelay = 10 * time.Millisecond
	var unsubscribed, resumed, archived atomic.Int32
	f.mu.Lock()
	previous := f.handle
	f.handle = func(m wireMessage) (any, bool) {
		switch m.Method {
		case "thread/unsubscribe":
			unsubscribed.Add(1)
			return map[string]string{"status": "unsubscribed"}, true
		case "thread/archive", "thread/delete", "turn/interrupt":
			archived.Add(1)
		case "thread/resume":
			resumed.Add(1)
		}
		return previous(m)
	}
	f.mu.Unlock()
	sendSynthetic(t, s, "parent-user-86")
	v := newTask(t, manager, "create-retired-worker-86")
	s.mu.Lock()
	id, run := s.binding.Tasks[v.ID].Thread, s.binding.Tasks[v.ID].Run
	s.mu.Unlock()
	finishTask(s, f, v.ID, "completed")
	waitWorkerRetired(t, s, id)
	s.mu.Lock()
	reportID, reportState := s.binding.Tasks[v.ID].ReportID, s.binding.Tasks[v.ID].ReportState
	s.mu.Unlock()
	if unsubscribed.Load() != 1 || archived.Load() != 0 {
		t.Fatal("retirement changed execution/history", unsubscribed.Load(), archived.Load())
	}
	f.emit(wireMessage{Method: "thread/closed", Params: raw(map[string]any{"threadId": id})})
	deadline := time.After(time.Second)
	for {
		if s.DiagnosticStatus()["nativeWorkerUnloads"] == uint64(1) {
			break
		}
		select {
		case <-deadline:
			t.Fatal("native unload was not correlated with the retired subscription")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got, err := s.ReadWork(testContext(t), v.ID); err != nil || got.Status != "completed" {
		t.Fatal("read lost completed task", got, err)
	}
	s.mu.Lock()
	if s.binding.Tasks[v.ID].ReportID != reportID || s.binding.Tasks[v.ID].ReportState != reportState {
		t.Fatal("read duplicated a completion report")
	}
	s.mu.Unlock()
	if resumed.Load() != 0 {
		t.Fatal("read loaded historical worker")
	}
	if got, err := s.SendWork(testContext(t), api.TaskMessage{ID: v.ID, RequestID: "continue-original-86", Prompt: "Continue on the same thread"}); err != nil || got.ID != v.ID {
		t.Fatal("explicit continuation failed", got, err)
	}
	if resumed.Load() != 1 || d.resumed["threadId"] != id || run == "" {
		t.Fatal("continuation did not reattach original native thread", resumed.Load(), d.resumed, id, run)
	}
}

func TestExternalActivityOnRetiredThreadReattachesOriginalObservation(t *testing.T) {
	s, f, _, manager := taskPair(t)
	s.workerIdleDelay = 10 * time.Millisecond
	var resumed, unsubscribed atomic.Int32
	f.mu.Lock()
	previous := f.handle
	f.handle = func(m wireMessage) (any, bool) {
		switch m.Method {
		case "thread/unsubscribe":
			unsubscribed.Add(1)
			return map[string]string{"status": "unsubscribed"}, true
		case "thread/resume":
			resumed.Add(1)
		}
		return previous(m)
	}
	f.mu.Unlock()
	sendSynthetic(t, s, "parent-external-86")
	v := newTask(t, manager, "create-external-worker-86")
	s.mu.Lock()
	id := s.binding.Tasks[v.ID].Thread
	s.mu.Unlock()
	finishTask(s, f, v.ID, "completed")
	waitWorkerRetired(t, s, id)
	f.mu.Lock()
	thread := f.workers[id]
	thread.Status.Type = "active"
	thread.Turns = []nativeTurn{{ID: "external-turn", Status: "inProgress"}}
	f.workers[id] = thread
	f.mu.Unlock()
	f.emit(wireMessage{Method: "thread/status/changed", Params: raw(map[string]any{"threadId": id, "status": map[string]string{"type": "active"}})})
	deadline := time.After(time.Second)
	for {
		s.mu.Lock()
		active := s.childSubscribed[id] && s.childRuns[id] == "external-turn"
		s.mu.Unlock()
		if active {
			break
		}
		select {
		case <-deadline:
			t.Fatal("active external turn did not reattach", id)
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got, err := s.ReadWork(testContext(t), v.ID); err != nil || got.Status != "working" {
		t.Fatal("external activity was not observed on original task", got, err)
	}
	time.Sleep(30 * time.Millisecond)
	if resumed.Load() != 1 || unsubscribed.Load() != 1 {
		t.Fatal("active external turn was retired or duplicated", resumed.Load(), unsubscribed.Load())
	}
	finishTask(s, f, v.ID, "completed")
	waitWorkerRetired(t, s, id)
	if unsubscribed.Load() != 2 {
		t.Fatal("second idle cycle retained subscription", unsubscribed.Load())
	}
}

func TestWorkerRetirementGuardsUnknownApprovalAndActive(t *testing.T) {
	for _, gate := range []string{"active", "unknown", "approval", "pending"} {
		t.Run(gate, func(t *testing.T) {
			s, f := sessionPair(t, "hold")
			s.workerIdleDelay = 5 * time.Millisecond
			var calls atomic.Int32
			f.mu.Lock()
			f.handle = func(m wireMessage) (any, bool) {
				if m.Method == "thread/unsubscribe" {
					calls.Add(1)
					return map[string]string{"status": "unsubscribed"}, true
				}
				return nil, false
			}
			f.mu.Unlock()
			s.mu.Lock()
			task := &taskRecord{Thread: "worker-guard", Run: "turn-guard", View: api.Task{ID: "task-guard", Status: "completed"}, Requests: map[string]taskReceipt{}}
			s.binding.Tasks = map[string]*taskRecord{"task-guard": task}
			s.children[task.Thread] = true
			s.childWatching[task.Thread] = true
			s.childSubscribed[task.Thread] = true
			s.childTerminals[opaque(task.Thread, task.Run)] = true
			switch gate {
			case "active":
				s.childRuns[task.Thread] = task.Run
			case "unknown":
				task.View.Status = "unknown"
				task.Requests["receipt"] = taskReceipt{Outcome: "unknown"}
			case "approval":
				s.prompts["approval"] = &prompt{thread: task.Thread, turn: task.Run}
			case "pending":
				task.Pending = "original-request"
			}
			s.scheduleChildRetirement(task.Thread)
			s.mu.Unlock()
			time.Sleep(30 * time.Millisecond)
			if calls.Load() != 0 {
				t.Fatal("protected worker was unsubscribed", gate)
			}
		})
	}
}

func TestWorkerRetirementRaceAndNativeFailure(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.workerIdleDelay = 5 * time.Millisecond
	readStarted, releaseRead := make(chan struct{}), make(chan struct{})
	var unsubscribed atomic.Int32
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "thread/read" && strings.Contains(string(m.Params), "worker-race") {
			close(readStarted)
			<-releaseRead
			thread := nativeThread{ID: "worker-race", Turns: []nativeTurn{{ID: "turn-race", Status: "completed"}}}
			thread.Status.Type = "idle"
			return map[string]any{"thread": thread}, true
		}
		if m.Method == "thread/unsubscribe" {
			unsubscribed.Add(1)
			return map[string]string{"status": "unsubscribed"}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	s.mu.Lock()
	task := &taskRecord{Thread: "worker-race", Run: "turn-race", View: api.Task{ID: "task-race", Status: "completed"}, Requests: map[string]taskReceipt{}}
	s.binding.Tasks = map[string]*taskRecord{"task-race": task}
	s.children[task.Thread], s.childWatching[task.Thread], s.childSubscribed[task.Thread] = true, true, true
	s.childTerminals[opaque(task.Thread, task.Run)] = true
	s.scheduleChildRetirement(task.Thread)
	s.mu.Unlock()
	<-readStarted
	s.mu.Lock()
	s.childRevision[task.Thread]++
	s.childRuns[task.Thread] = "new-active-turn"
	s.childRetireSeq[task.Thread]++
	s.mu.Unlock()
	close(releaseRead)
	time.Sleep(30 * time.Millisecond)
	if unsubscribed.Load() != 0 {
		t.Fatal("retirement crossed a newly active turn")
	}

	// The same subscription can retire after a later terminal fact. A transient
	// native failure leaves it subscribed and retries the same thread only.
	s.mu.Lock()
	delete(s.childRuns, task.Thread)
	task.Run = "turn-race"
	s.childRetireSeq[task.Thread]++
	s.mu.Unlock()
	var attempts atomic.Int32
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "thread/read" && strings.Contains(string(m.Params), "worker-race") {
			thread := nativeThread{ID: "worker-race", Turns: []nativeTurn{{ID: "turn-race", Status: "completed"}}}
			thread.Status.Type = "idle"
			return map[string]any{"thread": thread}, true
		}
		if m.Method == "thread/unsubscribe" {
			if attempts.Add(1) == 1 {
				return &NativeError{Code: -32000, Message: "fixture transient failure"}, true
			}
			return map[string]string{"status": "unsubscribed"}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	s.mu.Lock()
	s.scheduleChildRetirement(task.Thread)
	s.mu.Unlock()
	waitWorkerRetired(t, s, task.Thread)
	if attempts.Load() != 2 || s.DiagnosticStatus()["workerRetirementFailures"] != uint64(1) {
		t.Fatal("native failure was not retried safely", attempts.Load(), s.DiagnosticStatus())
	}
}

func TestActivityDuringUnsubscribeRechecksOriginalThread(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.workerIdleDelay = 5 * time.Millisecond
	unsubStarted, releaseUnsub := make(chan struct{}), make(chan struct{})
	var resumes atomic.Int32
	var nativeActive atomic.Bool
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if !strings.Contains(string(m.Params), "worker-unsub-race") {
			return nil, false
		}
		switch m.Method {
		case "thread/read", "thread/resume":
			status := "idle"
			turn := nativeTurn{ID: "old-turn", Status: "completed"}
			if nativeActive.Load() {
				status = "active"
				turn = nativeTurn{ID: "new-turn", Status: "inProgress"}
			}
			if m.Method == "thread/resume" {
				resumes.Add(1)
			}
			thread := nativeThread{ID: "worker-unsub-race", Turns: []nativeTurn{turn}}
			thread.Status.Type = status
			return map[string]any{"thread": thread}, true
		case "thread/unsubscribe":
			close(unsubStarted)
			<-releaseUnsub
			return map[string]string{"status": "unsubscribed"}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	s.mu.Lock()
	task := &taskRecord{Thread: "worker-unsub-race", Run: "old-turn", View: api.Task{ID: "task-unsub-race", Status: "completed"}, Requests: map[string]taskReceipt{}}
	s.binding.Tasks = map[string]*taskRecord{task.View.ID: task}
	s.children[task.Thread], s.childWatching[task.Thread], s.childSubscribed[task.Thread] = true, true, true
	s.scheduleChildRetirement(task.Thread)
	s.mu.Unlock()
	<-unsubStarted
	nativeActive.Store(true)
	f.emit(wireMessage{Method: "thread/status/changed", Params: raw(map[string]any{"threadId": task.Thread, "status": map[string]string{"type": "active"}})})
	close(releaseUnsub)
	deadline := time.After(time.Second)
	for {
		s.mu.Lock()
		active := s.childSubscribed[task.Thread] && s.childRuns[task.Thread] == "new-turn"
		s.mu.Unlock()
		if active {
			break
		}
		select {
		case <-deadline:
			t.Fatal("active turn during unsubscribe was not reobserved")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if resumes.Load() != 1 {
		t.Fatal("original thread was not reattached exactly once", resumes.Load())
	}
}

func TestWorkerSubscriptionPressureBlocksOnlyNewStarts(t *testing.T) {
	s, f, _, manager := taskPair(t)
	sendSynthetic(t, s, "parent-pressure-86")
	s.mu.Lock()
	for i := range workerSubscriptionLimit {
		s.childSubscribed[string(rune('a'+i))] = true
	}
	s.mu.Unlock()
	_, err := manager.StartTask(testContext(t), api.TaskStart{RequestID: "pressure-request-86", Title: "Pressure", Prompt: "Run a bounded fixture"})
	if err == nil || !strings.Contains(err.Error(), "连接过多") {
		t.Fatal("new worker ignored native subscription pressure", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.workers != nil && len(f.workers) != 0 {
		t.Fatal("pressure gate dispatched a native worker")
	}
}

func TestUnsupportedUnsubscribeStopsRetryWithoutChangingReceipt(t *testing.T) {
	s, f := sessionPair(t, "hold")
	s.workerIdleDelay = 5 * time.Millisecond
	var attempts atomic.Int32
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		var p struct {
			ThreadID string `json:"threadId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if p.ThreadID == "worker-unsupported" && m.Method == "thread/read" {
			thread := nativeThread{ID: p.ThreadID, Turns: []nativeTurn{{ID: "original-turn", Status: "completed"}}}
			thread.Status.Type = "idle"
			return map[string]any{"thread": thread}, true
		}
		if p.ThreadID == "worker-unsupported" && m.Method == "thread/unsubscribe" {
			attempts.Add(1)
			return &NativeError{Code: -32601, Message: "unsupported"}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	s.mu.Lock()
	task := &taskRecord{Thread: "worker-unsupported", Run: "original-turn", View: api.Task{ID: "original-task", Status: "completed"}, Requests: map[string]taskReceipt{"original-receipt": {Outcome: "accepted"}}}
	s.binding.Tasks = map[string]*taskRecord{task.View.ID: task}
	s.children[task.Thread], s.childWatching[task.Thread], s.childSubscribed[task.Thread] = true, true, true
	s.scheduleChildRetirement(task.Thread)
	s.mu.Unlock()
	deadline := time.After(time.Second)
	for {
		s.mu.Lock()
		unsupported := s.retireUnsupported
		s.mu.Unlock()
		if unsupported {
			break
		}
		select {
		case <-deadline:
			t.Fatal("missing unsupported capability fact")
		case <-time.After(5 * time.Millisecond):
		}
	}
	time.Sleep(30 * time.Millisecond)
	s.mu.Lock()
	retained := s.childSubscribed[task.Thread] && task.Requests["original-receipt"].Outcome == "accepted"
	s.mu.Unlock()
	if attempts.Load() != 1 || !retained {
		t.Fatal("unsupported unsubscribe altered original task", attempts.Load(), task)
	}
}
