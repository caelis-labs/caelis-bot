package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func legacyWorker(t *testing.T, s *Session, f *sessionFixture, status string) (string, string, string) {
	t.Helper()
	const id, thread, run = "task-original-recovery", "thread-original-recovery", "run-original-recovery"
	var record taskRecord
	if err := json.Unmarshal([]byte(`{"retired":true,"view":{"id":"task-original-recovery","status":"unavailable","outcome":"accepted"},"thread":"thread-original-recovery","run":"run-original-recovery","requests":{"original-request":{"outcome":"accepted","phase":"settled"}},"reportId":"original-report","reportState":"observed"}`), &record); err != nil {
		t.Fatal(err)
	}
	record.ReportID = "task-report-" + opaque(thread, run)
	s.mu.Lock()
	s.binding.Tasks = map[string]*taskRecord{id: &record}
	s.mu.Unlock()
	f.mu.Lock()
	native := nativeThread{ID: thread, Turns: []nativeTurn{{ID: run, Status: status}}}
	native.Status.Type = "notLoaded"
	f.workers[thread] = native
	f.mu.Unlock()
	return id, thread, run
}

func TestLegacyRetiredReadUsesOriginalRunWithoutPersistingViability(t *testing.T) {
	for _, status := range []string{"completed", "interrupted", "failed", "inProgress"} {
		t.Run(status, func(t *testing.T) {
			s, f, d, m := taskPair(t)
			id, thread, run := legacyWorker(t, s, f, status)
			if err := m.RefreshWatchlist(); err != nil {
				t.Fatal(err)
			}
			if s.WorkStates()[0].TaskEnded {
				t.Fatal("legacy flag terminated questions")
			}
			got, err := m.ReadTask(testContext(t), id)
			want := status
			if status == "inProgress" {
				want = "working"
			}
			if err != nil || got.Status != want || got.ID != id {
				t.Fatal(got, err)
			}
			loaded := NewSession(s.opts)
			record := loaded.binding.Tasks[id]
			if record.Thread != thread || record.Run != run || record.Requests["original-request"].Outcome != "accepted" || record.ReportID != "task-report-"+opaque(thread, run) || record.ReportState != "observed" {
				t.Fatal("identity or report receipt changed", record)
			}
			raw, err := os.ReadFile(s.opts.StateFile)
			if err != nil || strings.Contains(string(raw), `"retired"`) {
				t.Fatal("persisted viability flag", err)
			}
			if d.starts != 0 || d.sends != 0 {
				t.Fatal("read replayed input")
			}
		})
	}
}

func TestLegacyUnknownReceiptNeedsOriginalClientID(t *testing.T) {
	s, f, d, m := taskPair(t)
	id, thread, run := legacyWorker(t, s, f, "completed")
	s.mu.Lock()
	record := s.binding.Tasks[id]
	record.Pending = "uncertain-original"
	record.Requests[record.Pending] = taskReceipt{Outcome: "unknown", Phase: "dispatching", PriorRun: run}
	s.mu.Unlock()
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if got, err := m.ReadTask(testContext(t), id); err != nil || got.Status != "unknown" {
			t.Fatal(got, err)
		}
		if _, err := m.SendTask(testContext(t), api.TaskMessage{ID: id, RequestID: "new-forbidden-input", Prompt: "synthetic"}); err == nil {
			t.Fatal("unknown receipt bypassed")
		}
	}
	f.mu.Lock()
	native := f.workers[thread]
	native.Turns = []nativeTurn{{ID: "confirmed-turn", Status: "interrupted", Items: []nativeItem{{Type: "userMessage", ClientID: "uncertain-original"}}}}
	f.workers[thread] = native
	f.mu.Unlock()
	if got, err := m.ReadTask(testContext(t), id); err != nil || got.Status != "interrupted" || got.Outcome != "accepted" {
		t.Fatal(got, err)
	}
	if d.starts != 0 || d.sends != 0 {
		t.Fatal("original input replayed")
	}
}

func TestWorkerReadFencesConnectionAndRequestChanges(t *testing.T) {
	for _, change := range []string{"epoch", "binding", "request"} {
		t.Run(change, func(t *testing.T) {
			s, f, _, _ := taskPair(t)
			id, _, _ := legacyWorker(t, s, f, "completed")
			entered, release := make(chan struct{}), make(chan struct{})
			f.mu.Lock()
			previous := f.handle
			f.handle = func(msg wireMessage) (any, bool) {
				if msg.Method == "thread/turns/list" {
					close(entered)
					<-release
				}
				return previous(msg)
			}
			f.mu.Unlock()
			done := make(chan error, 1)
			go func() { _, err := s.ReadWork(testContext(t), id); done <- err }()
			<-entered
			s.mu.Lock()
			switch change {
			case "epoch":
				s.epoch++
			case "binding":
				s.binding.Tasks[id].Thread = "other-thread"
			case "request":
				s.binding.Tasks[id].Pending = "later-request"
			}
			s.mu.Unlock()
			close(release)
			if err := <-done; err == nil {
				t.Fatal("stale read adopted")
			}
			s.mu.Lock()
			status, activity := s.binding.Tasks[id].View.Status, s.binding.Tasks[id].Activity
			s.mu.Unlock()
			if status != "unknown" || activity != "" {
				t.Fatal("stale native result projected", status, activity)
			}
		})
	}
}

type recoveryTerminalConnection struct{ net.Conn }

func (*recoveryTerminalConnection) terminalEndpoint() string {
	return "unix:///synthetic-original-owner.sock"
}

func TestSharedRestartReadsLegacyWorkerAndReopensOriginalTerminal(t *testing.T) {
	s, f, d, m := taskPair(t)
	id, thread, run := legacyWorker(t, s, f, "interrupted")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Terminal target validation needs a portable executable path; the fixture
	// supplies the connection below and never launches this binary.
	s.opts.Binary = binary
	s.reconnectDelay = func(int) time.Duration { return time.Millisecond }
	var reads, resumes atomic.Int32
	f.mu.Lock()
	previous := f.handle
	f.handle = func(msg wireMessage) (any, bool) {
		if msg.Method == "thread/read" {
			reads.Add(1)
		}
		if msg.Method == "thread/resume" && strings.Contains(string(msg.Params), thread) {
			resumes.Add(1)
		}
		return previous(msg)
	}
	old := f.peer
	f.mu.Unlock()
	s.start = func(context.Context, Options) (*Client, error) {
		a, b := net.Pipe()
		f.mu.Lock()
		f.peer = b
		f.connections++
		f.mu.Unlock()
		go f.serve(b)
		return &Client{rpc: newTransportOptions(&recoveryTerminalConnection{a}, nil, true)}, nil
	}
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	_ = old.Close()
	awaitState(t, s, func(v api.Snapshot) bool {
		states := s.WorkStates()
		return v.Connection == "ready" && len(states) == 1 && states[0].Task.Status == "interrupted"
	})
	target, err := m.WorkTerminal(testContext(t), id)
	if err != nil || target.Thread != thread || target.Endpoint != "unix:///synthetic-original-owner.sock" {
		t.Fatal(target, err)
	}
	s.mu.Lock()
	same := s.binding.Tasks[id].Run == run
	s.binding.LostOwnerTasks = map[string]bool{id: true}
	s.mu.Unlock()
	if _, err := s.WorkTerminal(testContext(t), id); err == nil {
		t.Fatal("unproven original home attached to new owner")
	}
	if !same || reads.Load() == 0 || resumes.Load() != 0 || d.starts != 0 || d.sends != 0 {
		t.Fatal("reconnect replayed execution", reads.Load(), resumes.Load())
	}
}

func TestNativeReadErrorsKeepOriginalRetryAndReceipts(t *testing.T) {
	for _, code := range []int{-32600, -32603, -32000} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			s, f, d, m := taskPair(t)
			id, _, _ := legacyWorker(t, s, f, "completed")
			var failing atomic.Bool
			failing.Store(true)
			f.mu.Lock()
			previous := f.handle
			f.handle = func(msg wireMessage) (any, bool) {
				if msg.Method == "thread/read" && failing.Load() {
					return &NativeError{Code: code, Message: "synthetic original read rejection"}, true
				}
				return previous(msg)
			}
			f.mu.Unlock()
			if err := m.RefreshWatchlist(); err != nil {
				t.Fatal(err)
			}
			if _, err := m.WorkTerminal(testContext(t), id); err == nil {
				t.Fatal("failed read opened terminal")
			}
			failing.Store(false)
			if got, err := m.ReadTask(testContext(t), id); err != nil || got.Status != "completed" {
				t.Fatal(got, err)
			}
			if d.starts != 0 || d.sends != 0 {
				t.Fatal("retry sent input")
			}
		})
	}
}

func TestOldTerminalEventCannotReleaseNewWorkerActivity(t *testing.T) {
	s, f, _, _ := taskPair(t)
	id, _, _ := legacyWorker(t, s, f, "completed")
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.binding.Tasks[id]
	record.SupersededRuns = []string{"old-run"}
	record.Run = "current-run"
	record.View.Status = "working"
	record.Activity = "active"
	s.observeTaskTurn(record, nativeTurn{ID: "old-run", Status: "completed"})
	if record.Activity != "active" || record.View.Status != "working" || record.Run != "current-run" {
		t.Fatal("old terminal released current capacity")
	}
}

func TestCancelledWorkerObservationNeverResumesExecution(t *testing.T) {
	s, f, d, _ := taskPair(t)
	id, thread, _ := legacyWorker(t, s, f, "inProgress")
	var resumed atomic.Int32
	f.mu.Lock()
	previous := f.handle
	f.handle = func(msg wireMessage) (any, bool) {
		if msg.Method == "thread/resume" && strings.Contains(string(msg.Params), thread) {
			resumed.Add(1)
		}
		return previous(msg)
	}
	f.mu.Unlock()
	s.mu.Lock()
	s.binding.Tasks[id].SuppressReport = true
	c, epoch := s.client, s.epoch
	s.mu.Unlock()
	if !s.recoverChild(c, epoch, thread) {
		t.Fatal("original active facts not readable")
	}
	if resumed.Load() != 0 || d.starts != 0 || d.sends != 0 {
		t.Fatal("cancelled task resumed during observation")
	}
	if !s.WorkStates()[0].StopRequested {
		t.Fatal("cancel intent lost")
	}
}
