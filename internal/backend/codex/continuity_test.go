package codex

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestContinuitySamplesOnlyLiveOrdinaryResidentTurns(t *testing.T) {
	s := NewSession(SessionOptions{StateFile: filepath.Join(t.TempDir(), "binding.json"), BotTools: &api.ToolConnection{}, Execution: api.ExecutionSettings{Model: "fixture-model"}})
	s.binding.ThreadID, s.bound = "resident", true
	now := time.Now().Round(0)
	emit := func(method, body string) {
		s.applyEvent(Notification{Method: method, Params: json.RawMessage(body), ReceivedAt: now})
	}
	start := func(turn string) {
		emit("turn/started", fmt.Sprintf(`{"threadId":"resident","turn":{"id":%q,"status":"inProgress"}}`, turn))
	}
	finish := func(turn string) {
		emit("turn/completed", fmt.Sprintf(`{"threadId":"resident","turn":{"id":%q,"status":"completed"}}`, turn))
	}
	usage := func(thread, turn string, input, total int) {
		emit("thread/tokenUsage/updated", fmt.Sprintf(`{"threadId":%q,"turnId":%q,"tokenUsage":{"last":{"inputTokens":%d,"outputTokens":10,"totalTokens":%d},"total":{"totalTokens":%d}}}`, thread, turn, input, input+10, total))
	}
	start("one")
	usage("worker", "one", 90000, 1)
	usage("resident", "old", 90000, 1)
	usage("resident", "one", 70000, 100000)
	usage("resident", "one", 90000, 200000)
	usage("resident", "one", 1, 200000) // duplicate total is not a new observation
	finish("one")
	finish("one")
	if o := s.ContinuityObservation(); o.Turns != 1 || o.InputCount != 1 || o.Inputs[0] != 70000 {
		t.Fatal(o)
	}
	start("two")
	usage("resident", "two", 0, 300000) // local re-estimate, missing input
	finish("two")
	if o := s.ContinuityObservation(); o.InputKnown || o.InputCount != 1 || o.Turns != 2 {
		t.Fatal(o)
	}
	start("compact-turn")
	usage("resident", "compact-turn", 90000, 400000)
	for range 2 {
		emit("item/completed", `{"threadId":"resident","turnId":"compact-turn","item":{"id":"compact","type":"contextCompaction"}}`)
	}
	usage("resident", "compact-turn", 90000, 500000)
	finish("compact-turn")
	if o := s.ContinuityObservation(); o.Compactions != 1 || o.InputKnown || o.Turns != 3 {
		t.Fatal(o)
	}
	start("system")
	s.binding.HostReportTurns = map[string]hostReportTurn{"original": {Thread: "resident", Turn: "system"}}
	usage("resident", "system", 80000, 600000)
	finish("system")
	start("review")
	emit("item/started", `{"threadId":"resident","turnId":"review","item":{"id":"review","type":"enteredReviewMode"}}`)
	usage("resident", "review", 80000, 700000)
	finish("review")
	if o := s.ContinuityObservation(); o.Turns != 3 {
		t.Fatal("system/review activity counted", o)
	}
	// Untimestamped history and loading have no sampling side effects.
	s.applyEvent(Notification{Method: "turn/started", Params: json.RawMessage(`{"threadId":"resident","turn":{"id":"history","status":"inProgress"}}`)})
	finish("history")
	if s.ContinuityObservation().Turns != 3 {
		t.Fatal("history manufactured a turn")
	}
}

func TestContinuityOwnerFenceUsesNativeBinding(t *testing.T) {
	s, _ := sessionPair(t, "")
	current := s.Snapshot().RuntimeOwner
	if current == "" {
		t.Fatal("missing published owner")
	}
	message := api.SystemMessage{ID: "continuity-owner-current", Text: "Optional review", Source: api.SystemContinuityReview, RuntimeOwner: current}
	receipt, err := s.SubmitSystem(t.Context(), message)
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal("published native owner incorrectly deferred", receipt, err)
	}
	message.ID, message.RuntimeOwner = "continuity-owner-old", "another-native-owner"
	receipt, err = s.SubmitSystem(t.Context(), message)
	if err != nil || receipt.Outcome != "rejected" || !receipt.Deferred {
		t.Fatal("mismatched native owner was not fenced", receipt, err)
	}
}
