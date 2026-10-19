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
		s.applyScheduledEnvelope(v, e, inputScope{})
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
		s.applyScheduledEnvelope(replacement, e, inputScope{})
	}
	s.state.Views["main"] = replacement
	got = s.Snapshot()
	if len(got.Items) != 0 || !got.Quiet {
		t.Fatal("replacement leaked", got)
	}
	s.applyScheduledEnvelope(replacement, wire.Envelope{EventId: pointer("human"), TurnId: pointer("human-turn"), InputOperationId: pointer("human-operation"), Update: &user}, inputScope{})
	got = s.Snapshot()
	if len(got.Items) != 1 || got.Items[0].Kind != "user" {
		t.Fatal("human hidden", got)
	}
}

func TestBackgroundResultRequiresCaughtUpHistoryAndSurvivesReload(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
	s.state.Operations["care-fixture"] = journal{Scheduled: true, Outcome: "accepted", TurnID: "care-turn", Path: "/sessions/main/prompt"}
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
	s.state.Operations["care-fixture"] = journal{Scheduled: true, Outcome: "accepted", TurnID: "care-turn", Path: "/sessions/main/prompt"}
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
			s.state.Session.Profile.Permissions = &wire.SessionPermissions{ApprovalMode: pointer("auto-review")}
			s.state.Operations["care"] = journal{Scheduled: true, Outcome: "accepted", TurnID: "care-turn", Path: "/sessions/main/prompt"}
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

func TestScheduledOriginPersistsBeforeNativeDispatch(t *testing.T) {
	var session *Session
	session = fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		persisted, err := loadBinding(session.path)
		if err != nil {
			t.Fatal(err)
		}
		j := persisted.Operations["scheduled-input"]
		if len(j.OriginRequests) != 1 || j.OriginRequests[0] != "original-phone-input" || j.OriginOwner == "" {
			t.Fatal("source was not durable before dispatch")
		}
		writeFixture(w, wire.CommandResult{OperationId: "scheduled-input", Outcome: "accepted", Resource: &wire.CommandResource{Ref: pointer("turn")}})
	})
	in := api.Submission{ID: "scheduled-input", Scheduled: true, OriginRequests: []string{"original-phone-input"}}
	_, err := session.command(t.Context(), in.ID, "/sessions/main/prompt", wire.PromptRequest{OperationId: pointer(in.ID), Input: pointer("fixture"), SourceKind: pointer("authorized_background")}, in)
	if err != nil {
		t.Fatal(err)
	}
	j := session.state.Operations[in.ID]
	j.TurnID = "scheduled-turn"
	session.state.Operations[in.ID] = j
	session.state.Views["main"].Turns = map[string]string{"scheduled-turn": "completed"}
	if err := session.saveLocked(); err != nil {
		t.Fatal(err)
	}
	session.state, err = loadBinding(session.path)
	if err != nil {
		t.Fatal(err)
	}
	got := session.Snapshot()
	if len(got.ScheduledSources) != 1 || got.ScheduledSources[0].RequestID != "original-phone-input" || got.ScheduledSources[0].RuntimeOwner != got.RuntimeOwner || got.ScheduledSources[0].Phase != "completed" {
		t.Fatal(got.ScheduledSources)
	}
	raw, _ := json.Marshal(got)
	var visible map[string]any
	_ = json.Unmarshal(raw, &visible)
	if _, ok := visible["ScheduledSources"]; ok {
		t.Fatal("native provenance escaped")
	}
}
