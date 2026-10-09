package caelis

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"net/http"
	"testing"
)

func TestScheduledCanonicalReplayUsesInputOperationIdentity(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) {})
	s.state.Operations["automatic"] = journal{Scheduled: true, Outcome: "accepted"}
	v := s.state.Views["main"]
	user := json.RawMessage(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"private instruction"}}`)
	reply := json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"[[CAELIS_REMINDER_SKIP]]"}}`)
	events := []wire.Envelope{
		{EventId: pointer("u"), TurnId: pointer("turn"), InputOperationId: pointer("automatic"), Update: &user},
		{EventId: pointer("a"), TurnId: pointer("turn"), Update: &reply},
		{EventId: pointer("end"), Kind: "caelis/lifecycle", TurnId: pointer("turn"), Lifecycle: &wire.LifecycleEvent{State: "completed"}},
	}
	for _, e := range events {
		s.applyScheduledEnvelope(v, e)
	}
	got := s.Snapshot()
	if len(got.Items) != 0 || !got.Quiet {
		t.Fatal(got)
	}
	if e := s.saveLocked(); e != nil {
		t.Fatal(e)
	}
	restored, e := loadBinding(s.path)
	if e != nil {
		t.Fatal(e)
	}
	s.state = restored
	v = s.state.Views["main"]
	got = s.Snapshot()
	if !got.Quiet || len(got.Items) != 0 {
		t.Fatal("durable reload leaked", got)
	}
	// Replacement replay must classify the same native operation, without dispatch.
	replacement := &view{State: v.State, Seen: map[string]bool{}}
	for _, e := range events {
		s.applyScheduledEnvelope(replacement, e)
	}
	s.state.Views["main"] = replacement
	got = s.Snapshot()
	if len(got.Items) != 0 || !got.Quiet {
		t.Fatal("replacement leaked", got)
	}
	s.applyScheduledEnvelope(replacement, wire.Envelope{EventId: pointer("human"), TurnId: pointer("human-turn"), InputOperationId: pointer("human-operation"), Update: &user})
	got = s.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "user" {
		t.Fatal("human hidden", got)
	}
}

func TestBackgroundResultRequiresCaughtUpHistoryAndSurvivesReload(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
	s.state.Operations["care-fixture"] = journal{Scheduled: true, Outcome: "accepted", TurnID: "care-turn", Path: "/application/sessions/main/prompt"}
	v := s.state.Views["main"]
	v.State.Run.TurnId = pointer("care-turn")
	v.State.Run.Status = pointer("completed")
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	if s.BackgroundResult("care-fixture").Complete {
		t.Fatal("bootstrap before transcript treated as silent")
	}
	v.Items = []api.Item{{Kind: "activation", TurnKey: "care-turn"}, {Kind: "assistant", TurnKey: "care-turn", Text: "visible result"}}
	v.CommandCaughtUp = true
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	result := s.BackgroundResult("care-fixture")
	if !result.Visible || !result.Complete {
		t.Fatal(result)
	}
	restored, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.state = restored
	s.state.Views["main"].Items = nil
	s.state.LastReceipt = api.Receipt{ID: "new-human", Outcome: "accepted"}
	if got := s.BackgroundResult("care-fixture"); got != result {
		t.Fatal("lost retained result", got)
	}
}

func TestBackgroundApprovalRemainsVisibleAfterSilentCompletion(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
	s.state.Operations["care-fixture"] = journal{Scheduled: true, Outcome: "accepted", TurnID: "care-turn", Path: "/application/sessions/main/prompt"}
	v := s.state.Views["main"]
	v.CommandCaughtUp = true
	v.State.Run.TurnId = pointer("care-turn")
	v.State.Run.Status = pointer("running")
	v.State.Approval.Active = testApproval()
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	observed := s.BackgroundResult("care-fixture")
	if !observed.Visible || observed.Complete {
		t.Fatal("approval not observed", observed)
	}
	v.State.Approval.Active = nil
	v.State.Run.Status = pointer("completed")
	v.Items = []api.Item{{Kind: "activation", TurnKey: "care-turn"}, {Kind: "assistant", TurnKey: "care-turn", Text: api.SilentReminder}}
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	restored, err := loadBinding(s.path)
	if err != nil {
		t.Fatal(err)
	}
	s.state = restored
	got := s.BackgroundResult("care-fixture")
	if !got.Complete || !got.Visible || !got.ObservedAt.Equal(observed.ObservedAt) {
		t.Fatal("approval was refunded", got)
	}
}

func TestBackgroundGuardianReviewDoesNotCountAsVisibleFeedback(t *testing.T) {
	for _, tc := range []struct {
		status, turn string
		visible      bool
	}{
		{"in_progress", "care-turn", false}, {"approved", "care-turn", false},
		{"denied", "care-turn", false}, {"failed", "care-turn", false}, {"timed_out", "care-turn", false},
		{"denied", "other-turn", false},
	} {
		t.Run(tc.status+"/"+tc.turn, func(t *testing.T) {
			s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
			s.state.Session.Profile.Permissions = &wire.ApplicationPermissions{ApprovalMode: pointer("auto-review")}
			s.state.Operations["care"] = journal{Scheduled: true, Outcome: "accepted", TurnID: "care-turn", Path: "/application/sessions/main/prompt"}
			v := s.state.Views["main"]
			v.CommandCaughtUp = true
			v.State.Run.TurnId, v.State.Run.Status = pointer("care-turn"), pointer("running")
			v.State.Approval.Active = testApproval()
			e := wire.Envelope{Kind: "caelis/approval_review", SessionId: pointer("main"), TurnId: &tc.turn, ApprovalRequestId: pointer("review"), ApprovalReview: &wire.ApprovalReview{ToolCallId: pointer("call"), Status: &tc.status}}
			if tc.status == "approved" || tc.status == "denied" {
				e.Delivery.Mode = wire.DeliveryModeMirror
			}
			applyEnvelope(v, e)
			if err := s.saveLocked(); err != nil {
				t.Fatal(err)
			}
			if got := s.BackgroundResult("care"); got.Visible != tc.visible || got.Complete {
				t.Fatal("incorrect pending interruption", got)
			}
			v.State.Approval.Active = nil
			v.State.Run.Status = pointer("completed")
			v.Items = []api.Item{{Kind: "assistant", TurnKey: "care-turn", Text: api.SilentReminder}}
			if err := s.saveLocked(); err != nil {
				t.Fatal(err)
			}
			restored, err := loadBinding(s.path)
			if err != nil {
				t.Fatal(err)
			}
			s.state = restored
			if got := s.BackgroundResult("care"); got.Visible != tc.visible || !got.Complete {
				t.Fatal("incorrect retained interruption", got)
			}
		})
	}
}
