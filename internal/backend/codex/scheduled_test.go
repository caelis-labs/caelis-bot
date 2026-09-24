package codex

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestScheduledSubmitAndReplayKeepProvenance(t *testing.T) {
	s, f := sessionPair(t, "")
	in := api.Submission{ID: "automatic-fixture", Text: "private reminder instruction", Scheduled: true}
	receipt, e := s.Submit(testContext(t), in, nil)
	if e != nil || receipt.Outcome != "accepted" {
		t.Fatal(receipt, e)
	}
	turn := nativeTurn{ID: "run-native", Status: "completed", Items: []nativeItem{{ID: "user", Type: "userMessage", ClientID: in.ID, Content: []nativeInput{{Type: "text", Text: in.Text}}}, {ID: "final", Type: "agentMessage", Text: api.SilentReminder}}}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": turn})})
	v := awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
	if !v.Quiet || !v.Scheduled || len(v.Items) != 0 {
		t.Fatal("automatic prompt/skip leaked", v)
	}
	// New adapter from durable binding, then canonical historical replay.
	restored := NewSession(s.opts)
	restored.state.Connection = "ready"
	restored.applyTurn(turn, true)
	restored.state.Phase = "completed"
	restored.update()
	v = restored.Snapshot()
	if !v.Quiet || len(v.Items) != 0 {
		t.Fatal("restart lost provenance", v)
	}
	// Identical prose in a real human message must remain visible.
	restored.applyTurn(nativeTurn{ID: "human", Status: "completed", Items: []nativeItem{{ID: "real", Type: "userMessage", ClientID: "human-fixture", Content: []nativeInput{{Type: "text", Text: in.Text}}}}}, true)
	restored.update()
	v = restored.Snapshot()
	if len(v.Items) != 1 || v.Items[0].Text != in.Text {
		t.Fatal("human message hidden", v)
	}
}

func TestLegacyScheduledHistoryIsHiddenByReservedNativeID(t *testing.T) {
	s := NewSession(SessionOptions{})
	s.state.Connection = "ready"
	s.applyTurn(nativeTurn{ID: "legacy", Status: "completed", Items: []nativeItem{{ID: "u", Type: "userMessage", ClientID: "wake-ABCDEFGHIJKLMNOPQRSTUVWXYZ", Content: []nativeInput{{Type: "text", Text: "old internal prompt"}}}}}, true)
	s.state.Phase = "completed"
	s.update()
	v := s.Snapshot()
	if len(v.Items) != 0 || !v.Quiet {
		t.Fatal("legacy automatic input leaked", v)
	}
}
