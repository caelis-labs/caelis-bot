package codex

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestContinuitySystemSourcesSurviveNativeDeliveryAndRestart(t *testing.T) {
	sources := []api.SystemSource{api.SystemWorkerResult, api.SystemWorkerApproval, api.SystemWorkerQuestion, api.SystemReminder, api.SystemCallback, api.SystemCommandResult, api.SystemContinuityReview, api.SystemSource("legacy")}
	for _, source := range sources {
		for _, compacted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/compact=%t", source, compacted), func(t *testing.T) {
				s, f := sessionPair(t, "normal")
				s.opts.BotTools = &api.ToolConnection{}
				s.opts.Execution.Model = "fixture-model"
				posts := 0
				f.mu.Lock()
				f.handle = func(m wireMessage) (any, bool) {
					if m.Method == "turn/start" {
						posts++
						return map[string]any{"turn": nativeTurn{ID: "source-turn", Status: "inProgress"}}, true
					}
					return nil, false
				}
				f.mu.Unlock()
				message := api.SystemMessage{ID: "original-system", Source: source, Text: "Source is assigned by the producer, not this text."}
				if source == "legacy" {
					message.Source = api.SystemWorkerResult
				}
				if receipt, err := s.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != "accepted" {
					t.Fatal(receipt, err)
				}
				if source == "legacy" {
					s.mu.Lock()
					old := s.binding.HostReportTurns[message.ID]
					old.SystemSource = ""
					s.binding.HostReportTurns[message.ID] = old
					err := s.save()
					s.mu.Unlock()
					if err != nil {
						t.Fatal(err)
					}
				}
				restored := NewSession(s.opts)
				s.mu.Lock()
				s.binding = restored.binding
				now := time.Now().Round(0)
				emit := func(method, body string) {
					s.applyEvent(Notification{Method: method, Params: json.RawMessage(body), ReceivedAt: now})
				}
				for range 2 {
					emit("turn/started", `{"threadId":"thread-native","turn":{"id":"source-turn","status":"inProgress"}}`)
					emit("thread/tokenUsage/updated", `{"threadId":"thread-native","turnId":"source-turn","tokenUsage":{"last":{"inputTokens":500,"outputTokens":10,"totalTokens":510},"total":{"totalTokens":510}}}`)
					if compacted {
						emit("item/completed", `{"threadId":"thread-native","turnId":"source-turn","item":{"id":"native-compact","type":"contextCompaction","status":"completed"}}`)
					}
					emit("turn/completed", `{"threadId":"thread-native","turn":{"id":"source-turn","status":"completed"}}`)
				}
				observed := s.binding.Continuity.Observation
				s.mu.Unlock()
				want := uint64(1)
				if source == api.SystemContinuityReview || source == "legacy" {
					want = 0
				}
				samples, compactions := int(want), uint64(0)
				if compacted {
					samples = 0
					compactions = want
				}
				if observed.Turns != want || observed.InputCount != samples || observed.Compactions != compactions {
					t.Fatalf("source=%s turns=%d samples=%d want=%d", source, observed.Turns, observed.InputCount, want)
				}
				if receipt, err := s.SubmitSystem(t.Context(), message); err != nil || receipt.Outcome != "accepted" {
					t.Fatal(receipt, err)
				}
				f.mu.Lock()
				defer f.mu.Unlock()
				if posts != 1 {
					t.Fatal("original system operation replayed", fmt.Sprint(posts))
				}
			})
		}
	}
}

func TestContinuityCompletionBeforeSystemReceiptCannotSelfCount(t *testing.T) {
	s, f := sessionPair(t, "normal")
	s.opts.BotTools = &api.ToolConnection{}
	sent, release := make(chan struct{}), make(chan struct{})
	f.mu.Lock()
	f.handle = func(m wireMessage) (any, bool) {
		if m.Method == "turn/start" {
			close(sent)
			<-release
			return map[string]any{"turn": nativeTurn{ID: "early-turn", Status: "completed"}}, true
		}
		return nil, false
	}
	f.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		_, err := s.SubmitSystem(t.Context(), api.SystemMessage{ID: "original-review", Source: api.SystemContinuityReview, Text: "Optional review."})
		done <- err
	}()
	<-sent
	s.mu.Lock()
	now := time.Now().Round(0)
	s.applyEvent(Notification{Method: "turn/started", Params: json.RawMessage(`{"threadId":"thread-native","turn":{"id":"early-turn","status":"inProgress"}}`), ReceivedAt: now})
	s.applyEvent(Notification{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread-native","turn":{"id":"early-turn","status":"completed"}}`), ReceivedAt: now})
	count := s.binding.Continuity.Turns
	s.mu.Unlock()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("completion before original prompt receipt counted the maintenance invitation", count)
	}
}
