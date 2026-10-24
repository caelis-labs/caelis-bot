package caelis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func workerOutputMessage(event, message, text string, final bool) wire.Envelope {
	e := workerHistoryMessage(event, "turn", "agent_message_chunk", text, final)
	e.SessionId = pointer("work")
	if message != "" {
		var update map[string]any
		_ = json.Unmarshal(*e.Update, &update)
		update["messageId"] = message
		data, _ := json.Marshal(update)
		e.Update = pointer(json.RawMessage(data))
	}
	return e
}
func feedWorkerOutput(s *Session, events []wire.Envelope) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, event := range events {
		s.applyScheduledEnvelope(s.state.Views["work"], event, inputScope{InstanceID: s.state.InstanceID, SessionID: "work"})
	}
}

func TestWorkerLiveProjectionSharesExactNativeHistorySegmentation(t *testing.T) {
	events := []wire.Envelope{
		workerOutputMessage("event-one", "native-one", "same body", true),
		workerOutputMessage("event-two", "native-two", "same body", true),
		workerOutputMessage("first-fragment", "", "split ", false),
		workerOutputMessage("last-fragment", "", "body", true),
		workerOutputMessage("following-segment", "", "same body", true),
	}
	head := nativeWorkHead("turn")
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/state") {
			writeFixture(w, head)
			return
		}
		workerHistoryFrames(w, events, "")
	})
	addServiceWorker(s, "task", "work", head)
	feedWorkerOutput(s, events)
	// Replaying a canonical event cannot append its text again.
	feedWorkerOutput(s, events)
	live, err := s.ObserveWork(t.Context(), "task")
	if err != nil || len(live.Items) != 4 {
		t.Fatal("separate native messages were merged", live.Items, err)
	}
	history, err := s.ReadWorkConversation(t.Context(), "task")
	if err != nil || len(history) != 4 {
		t.Fatal("native history", history, err)
	}
	for i, message := range history {
		if live.Items[i].HistoryID != message.ID || live.Items[i].Text != message.Text || live.Items[i].Status != "completed" {
			t.Fatal("live/history native identities differ", live.Items[i], message)
		}
	}
	if live.Items[0].HistoryID == live.Items[1].HistoryID || live.Items[2].Text != "split body" || live.Items[2].HistoryID != nativeWorkerMessageID("work", "turn", "assistant", "first-fragment") {
		t.Fatal("message identity inferred from text or last fragment", live.Items)
	}
	for _, item := range s.Snapshot().Items {
		if strings.Contains(item.Text, "same body") {
			t.Fatal("Worker output entered resident chat")
		}
	}
}

func TestWorkerReplacementAndReopenRetainExactBoundedOutputWhenHistoryFails(t *testing.T) {
	old := []wire.Envelope{workerOutputMessage("old-event", "native-old", "same body", true), workerOutputMessage("shared-event", "native-shared", "old shared text", true)}
	replacement := []wire.Envelope{workerOutputMessage("shared-event-new", "native-shared", "canonical shared text", true), workerOutputMessage("new-event", "native-new", "same body", true)}
	var historyUnavailable atomic.Bool
	head := nativeWorkHead("turn")
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/state") {
			writeFixture(w, head)
		} else if historyUnavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			workerHistoryFrames(w, replacement, "")
		}
	})
	addServiceWorker(s, "task", "work", head)
	feedWorkerOutput(s, old)
	_ = s.watch(t.Context(), s.client, "work", s.state.InstanceID)
	live, err := s.ObserveWork(t.Context(), "task")
	if err != nil || len(live.Items) != 3 || live.Items[0].Text != "same body" || live.Items[1].Text != "canonical shared text" || live.Items[2].Text != "same body" {
		t.Fatal("replacement lost cached IDs or merged equal text", live.Items, err)
	}
	if live.Items[0].HistoryID == live.Items[2].HistoryID {
		t.Fatal("replacement matched by text")
	}
	historyUnavailable.Store(true)
	if history, err := s.ReadWorkConversation(t.Context(), "task"); err == nil || history != nil {
		t.Fatal("history failure inferred empty success", history, err)
	}
	live, err = s.ObserveWork(t.Context(), "task")
	if err != nil || len(live.Items) != 3 || live.Items[0].Status != "completed" {
		t.Fatal("history failure erased completed live output", live.Items, err)
	}
	s.mu.Lock()
	err = s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	items := reopened.Views["work"].WorkerOutput.current()
	if len(items) != 3 || items[0].HistoryID != live.Items[0].HistoryID || items[1].Text != "canonical shared text" {
		t.Fatal("reopened current projection lost original message IDs", items)
	}
	other := newWorkerTranscript("different-session")
	other.retainMissing(reopened.Views["work"].WorkerOutput)
	if len(other.Items) != 0 {
		t.Fatal("replacement imported another Session's output")
	}
}

func TestWorkerCurrentOutputRemainsBoundedWithoutMergingNativeIDs(t *testing.T) {
	projection := newWorkerTranscript("work")
	for i := range 300 {
		if err := projection.apply(workerOutputMessage(fmt.Sprintf("event-%03d", i), fmt.Sprintf("message-%03d", i), strings.Repeat("x", 16<<10), true)); err != nil {
			t.Fatal(err)
		}
		projection.bound()
	}
	encoded, _ := json.Marshal(projection)
	if len(projection.Items) == 0 || len(projection.Items) > 200 || len(encoded) > 550<<10 {
		t.Fatal("current output became an unbounded log", len(projection.Items), len(encoded))
	}
	last := projection.current()[len(projection.Items)-1]
	if last.HistoryID != nativeWorkerMessageID("work", "turn", "assistant", "message-299") {
		t.Fatal("bound changed original native message identity", last.HistoryID)
	}
}

func TestWorkerCurrentOutputNativeFinalSurvivesPartialReplacement(t *testing.T) {
	old := newWorkerTranscript("work")
	if err := old.apply(workerOutputMessage("final-event", "original-message", "full native result", true)); err != nil {
		t.Fatal(err)
	}
	next := newWorkerTranscript("work")
	if err := next.apply(workerOutputMessage("partial-event", "original-message", "partial", false)); err != nil {
		t.Fatal(err)
	}
	next.retainMissing(old)
	items := next.current()
	if len(items) != 1 || items[0].Text != "full native result" || items[0].Status != "completed" || items[0].HistoryID != old.current()[0].HistoryID {
		t.Fatal("partial replacement lost observed native final", items)
	}
}
