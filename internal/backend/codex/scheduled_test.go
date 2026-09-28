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
	if result := s.BackgroundResult(in.ID); !result.Complete || result.Visible {
		t.Fatal("silent result missing", result)
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

func TestBackgroundResultRetainedAfterNewTurnAndReload(t *testing.T) {
	s, f := sessionPair(t, "")
	in := api.Submission{ID: "care-result-test", Text: "fixture", Scheduled: true}
	if r, err := s.Submit(testContext(t), in, nil); err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	turn := nativeTurn{ID: "run-native", Status: "completed", Items: []nativeItem{{ID: "u", Type: "userMessage", ClientID: in.ID}, {ID: "a", Type: "agentMessage", Text: "visible result"}}}
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": turn})})
	awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
	result := s.BackgroundResult(in.ID)
	if !result.Complete || !result.Visible || result.ObservedAt.IsZero() {
		t.Fatal(result)
	}
	restored := NewSession(s.opts)
	restored.applyTurn(nativeTurn{ID: "later", Status: "completed"}, true)
	restored.update()
	if got := restored.BackgroundResult(in.ID); got != result {
		t.Fatal("lost result", got)
	}
}

func TestBackgroundApprovalRemainsVisibleAfterSilentCompletion(t *testing.T) {
	s, f := sessionPair(t, "hold")
	in := api.Submission{ID: "care-approval", Text: "fixture", Scheduled: true}
	if r, err := s.Submit(testContext(t), in, nil); err != nil || r.Outcome != "accepted" {
		t.Fatal(r, err)
	}
	f.emit(approvalMessage("care-native-approval"))
	awaitState(t, s, func(v api.Snapshot) bool { return len(v.Approvals) == 1 })
	observed := s.BackgroundResult(in.ID)
	if !observed.Visible || observed.Complete {
		t.Fatal("approval not observed", observed)
	}
	turn := nativeTurn{ID: "run-native", Status: "completed", Items: []nativeItem{{ID: "u", Type: "userMessage", ClientID: in.ID}, {ID: "a", Type: "agentMessage", Text: api.SilentReminder}}}
	f.emit(wireMessage{Method: "serverRequest/resolved", Params: raw(map[string]any{"threadId": "thread-native", "requestId": "care-native-approval"})})
	f.emit(wireMessage{Method: "turn/completed", Params: raw(map[string]any{"threadId": "thread-native", "turn": turn})})
	awaitState(t, s, func(v api.Snapshot) bool { return v.Phase == "completed" })
	restored := NewSession(s.opts)
	got := restored.BackgroundResult(in.ID)
	if !got.Complete || !got.Visible || !got.ObservedAt.Equal(observed.ObservedAt) {
		t.Fatal("approval was refunded", got)
	}
}

func TestBackgroundReviewVisibilityKeepsNativeProvenance(t *testing.T) {
	for _, tc := range []struct {
		thread, turn, status string
		visible              bool
	}{
		{"root", "care-turn", "denied", true},
		{"root", "care-turn", "timedOut", true},
		{"root", "care-turn", "aborted", true},
		{"root", "care-turn", "failed", true},
		{"root", "care-turn", "approved", false},
		{"root", "care-turn", "inProgress", false},
		{"root", "old-turn", "denied", false},
		{"worker", "care-turn", "denied", false},
		{"foreign", "care-turn", "denied", false},
	} {
		t.Run(tc.thread+"/"+tc.turn+"/"+tc.status, func(t *testing.T) {
			s := NewSession(SessionOptions{StateFile: t.TempDir() + "/binding.json"})
			s.binding.ThreadID = "root"
			s.binding.Scheduled["care"] = "care-turn"
			s.children["worker"] = true
			s.runs["care-turn"] = "inProgress"
			event := Notification{Method: "item/autoApprovalReview/completed", Params: raw(map[string]any{
				"threadId": tc.thread, "turnId": tc.turn, "reviewId": "review",
				"review": map[string]string{"status": tc.status},
			})}
			s.applyEvent(event)
			s.update()
			observed := s.BackgroundResult("care")
			if tc.visible {
				v := s.Snapshot()
				if v.Quiet || len(v.Reviews) != 1 {
					t.Fatal("counted review notice was hidden", v)
				}
			}
			if observed.Visible != tc.visible || observed.Complete {
				t.Fatalf("wrong review attribution: %+v", observed)
			}
			s.applyEvent(event) // Native retries must not consume a second interruption.
			s.applyTurn(nativeTurn{ID: "care-turn", Status: "completed", Items: []nativeItem{
				{ID: "u", Type: "userMessage", ClientID: "care"},
				{ID: "a", Type: "agentMessage", Text: api.SilentReminder},
			}}, false)
			s.update()
			got := NewSession(s.opts).BackgroundResult("care")
			if !got.Complete || got.Visible != tc.visible || !got.ObservedAt.Equal(observed.ObservedAt) {
				t.Fatalf("review visibility was lost or counted twice: %+v -> %+v", observed, got)
			}
		})
	}
}
