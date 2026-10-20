package codex

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRecoveryStopIntentIsScopedToCurrentWorkerTurn(t *testing.T) {
	for _, mode := range []string{"settled-old-turn", "settled-current-turn", "unknown-old-turn", "user-cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, f, d, _ := taskPair(t)
			id, thread, run := legacyWorker(t, s, f, "inProgress")
			var resumes atomic.Int32
			f.mu.Lock()
			previous := f.handle
			f.handle = func(msg wireMessage) (any, bool) {
				if msg.Method == "thread/resume" && strings.Contains(string(msg.Params), thread) {
					resumes.Add(1)
				}
				return previous(msg)
			}
			f.mu.Unlock()
			s.mu.Lock()
			record := s.binding.Tasks[id]
			record.StopPendingRequest, record.StopPendingTurn, record.StopPendingStatus = "original-stop", "previous-turn", "interrupted"
			switch mode {
			case "settled-current-turn":
				record.StopPendingTurn = run
			case "unknown-old-turn":
				record.StopPendingStatus = ""
			case "user-cancel":
				record.SuppressReport = true
			}
			s.childWatching[thread] = true
			client, epoch := s.client, s.epoch
			s.mu.Unlock()
			if !s.recoverChild(client, epoch, thread) {
				t.Fatal("original recovery read failed")
			}
			s.mu.Lock()
			subscribed, watching := s.childSubscribed[thread], s.childWatching[thread]
			s.mu.Unlock()
			want := mode == "settled-old-turn"
			if subscribed != want || watching != want || (resumes.Load() == 1) != want {
				t.Fatalf("historical stop/current cancel confused: subscribed=%v watching=%v resumes=%d", subscribed, watching, resumes.Load())
			}
			if record.StopPendingRequest != "original-stop" || d.starts != 0 || d.sends != 0 {
				t.Fatal("recovery discarded stop receipt or dispatched input")
			}
		})
	}
}

func TestReadyConnectPreservesSequentialWorkerIdleEvidence(t *testing.T) {
	s, f, _, m := taskPair(t)
	s.mu.Lock()
	s.binding.Tasks = map[string]*taskRecord{}
	for _, id := range []string{"worker-a", "worker-b"} {
		s.binding.Tasks[id] = &taskRecord{View: api.Task{ID: id, Status: "unknown", Outcome: "unknown"}, Thread: id, Run: "previous", Pending: "original-" + id, Requests: map[string]taskReceipt{"original-" + id: {Outcome: "unknown", Phase: "dispatching", PriorRun: "previous"}}}
		s.childWatching[id], s.childSubscribed[id] = true, true
	}
	client, epoch := s.client, s.epoch
	s.childObservationFailed["unreachable-child"] = true
	s.mu.Unlock()
	f.mu.Lock()
	for _, id := range []string{"worker-a", "worker-b"} {
		native := nativeThread{ID: id, Turns: []nativeTurn{{ID: "previous", Status: "completed"}}}
		native.Status.Type = "idle"
		f.workers[id] = native
	}
	f.mu.Unlock()
	if err := m.RefreshWatchlist(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"worker-a", "worker-b"} {
		if err := s.Connect(testContext(t)); err != nil {
			t.Fatal(err)
		}
		if _, err := m.ReadTask(testContext(t), id); err != nil {
			t.Fatal(err)
		}
	}
	page, err := m.QueryTasks(api.TaskQuery{})
	if err != nil || page.Reserved != 0 {
		t.Fatal("same owner Connect invalidated another worker's idle proof", page, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.childObservationFailed["unreachable-child"] {
		t.Fatal("same owner Connect renewed an exhausted unrelated read")
	}
	if s.client != client || s.epoch != epoch {
		t.Fatal("fixture replaced connection")
	}
	for _, id := range []string{"worker-a", "worker-b"} {
		if s.binding.Tasks[id].Activity != "idle" || s.binding.Tasks[id].Requests["original-"+id].Outcome != "unknown" {
			t.Fatal("lost idle evidence or changed original unknown receipt")
		}
	}
}
