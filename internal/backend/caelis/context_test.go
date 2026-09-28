package caelis

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
)

func TestContextLostReceiptRecoversWithoutResending(t *testing.T) {
	v, err := notebook.OpenVault(filepath.Join(t.TempDir(), "Notebook"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	handoff := filepath.Join(v.Path(), notebook.HandoffName)
	os.WriteFile(handoff, []byte("handoff fixture"), 0600)
	var posts atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			var in wire.ApplicationPromptRequest
			json.NewDecoder(r.Body).Decode(&in)
			if !strings.Contains(value(in.Input), "handoff fixture") || !strings.HasSuffix(value(in.Input), "new topic") {
				t.Error("missing context")
			}
			drop(w)
			return
		}
		writeFixture(w, wire.ApplicationOperation{OperationId: "first-user", Outcome: "accepted", Result: &wire.CommandResult{OperationId: "first-user", Outcome: "accepted", Target: &wire.CommandTarget{TurnId: pointer("turn")}}})
	})
	s.tools = &api.ToolConnection{PrepareContext: v.PrepareContext, ConsumeContext: v.ConsumeContext}
	in := api.Submission{ID: "first-user", Text: "new topic"}
	r, err := s.Submit(t.Context(), in, nil)
	if err != nil || r.Outcome != "unknown" {
		t.Fatal(r, err)
	}
	if _, err = os.Stat(handoff); err != nil {
		t.Fatal("consumed uncertain handoff")
	}
	restored := New(Options{Directory: filepath.Dir(s.path)})
	restored.client, restored.connected, restored.tools = s.client, true, s.tools
	if err = restored.recoverOperations(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(handoff); !os.IsNotExist(err) {
		t.Fatal("accepted handoff not consumed", err)
	}
	r, err = restored.Submit(t.Context(), in, nil)
	if err != nil || r.Outcome != "accepted" || posts.Load() != 1 {
		t.Fatal(r, err, posts.Load())
	}
	in.Text = "different"
	r, err = restored.Submit(t.Context(), in, nil)
	if err != nil || r.Outcome != "rejected" {
		t.Fatal("changed replay accepted", r, err)
	}
}

func TestDreamRenewalKeepsHistoryAndRebindsExistingGrant(t *testing.T) {
	var creates, grants atomic.Int32
	var created wire.ApplicationProfile
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/application/sessions/main/configuration":
			writeFixture(w, wire.ApplicationConfiguration{SessionId: "main", Revision: "1", Profile: wire.ApplicationProfile{Model: "retained-model", Execution: "workspace-write", Permissions: &wire.ApplicationPermissions{Mode: pointer("read-only"), ApprovalMode: pointer("manual")}}})
		case "/application/sessions":
			creates.Add(1)
			var in wire.CreateApplicationSessionRequest
			json.NewDecoder(r.Body).Decode(&in)
			if in.Profile.Model != "retained-model" || in.Profile.Reviewer == nil || in.Profile.Reviewer.Model != "retained-model" || value(in.Profile.Permissions.Mode) != "read-only" || value(in.Profile.Permissions.ApprovalMode) != "auto-review" {
				t.Error("handoff lost model, sandbox or Guardian assembly")
			}
			created = in.Profile
			writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "committed", SessionId: pointer("next")})
		case "/application/sessions/next":
			writeFixture(w, wire.ApplicationBinding{SessionId: "next", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Profile: created})
		case "/application/sessions/next/reviewer-state":
			writeFixture(w, wire.ApplicationReviewerState{SessionId: "next", ApprovalMode: "auto-review", Reviewer: created.Reviewer, Status: "ready"})
		case "/sessions/next/state":
			writeFixture(w, wire.SessionState{SessionId: "next"})
		case "/application/sessions/next/background-grants":
			grants.Add(1)
			var in wire.ApplicationBackgroundGrantRequest
			json.NewDecoder(r.Body).Decode(&in)
			if in.AuthorizationOperationId != "original-user" || in.Source != "schedule-source" {
				t.Error("new authority invented")
			}
			writeFixture(w, wire.ApplicationBackgroundGrant{Id: "new-grant", SessionId: "next", Source: in.Source, AuthorizationOperationId: in.AuthorizationOperationId})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	s.state.Operations["dream"] = journal{Path: "/application/sessions/main/prompt", Dream: true, Scheduled: true, Outcome: "accepted", TurnID: "turn"}
	v := s.state.Views["main"]
	v.CommandCaughtUp = true
	v.State.Run.Status = pointer("completed")
	v.State.Run.TurnId = pointer("turn")
	v.Turns = map[string]string{"turn": "completed"}
	v.Items = []api.Item{{ID: "old", Kind: "user", TurnKey: "user-turn", Text: "old topic"}, {ID: "recap", Kind: "assistant", TurnKey: "turn", Text: "All done."}}
	s.state.Grants["reminder"] = grantRecord{Fingerprint: "same", Grant: wire.ApplicationBackgroundGrant{Id: "old-grant", SessionId: "main", Source: "schedule-source", AuthorizationOperationId: "original-user"}}
	if err := s.RenewConversation(t.Context(), "dream", "main"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenewConversation(t.Context(), "dream", "main"); err != nil {
		t.Fatal(err)
	}
	if creates.Load() != 1 || grants.Load() != 1 || s.state.Grants["reminder"].Grant.Id != "new-grant" || len(s.Snapshot().Items) != 2 {
		t.Fatal("lost continuity")
	}
}

func TestDreamRenewalRejectedAndUnknownRecovery(t *testing.T) {
	for _, outcome := range []string{"rejected", "conflicted", "http-rejected", "unknown", "recovered-rejected"} {
		t.Run(outcome, func(t *testing.T) {
			var creates atomic.Int32
			op := "dream-renew-" + digest([]byte("dream"))
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
				case "/application/sessions/main/configuration":
					writeFixture(w, wire.ApplicationConfiguration{SessionId: "main", Revision: "1", Profile: wire.ApplicationProfile{Execution: "workspace-write"}})
				case "/application/sessions":
					creates.Add(1)
					if outcome == "unknown" || outcome == "recovered-rejected" {
						drop(w)
					} else if outcome == "http-rejected" {
						http.Error(w, "synthetic rejection", http.StatusBadRequest)
					} else {
						writeFixture(w, wire.CommandResult{OperationId: op, Outcome: wire.Outcome(outcome)})
					}
				case "/application/operations/" + op:
					recovered := wire.Outcome("unknown")
					if outcome == "recovered-rejected" {
						recovered = "rejected"
					}
					writeFixture(w, wire.ApplicationOperation{OperationId: op, Outcome: recovered, Result: &wire.CommandResult{OperationId: op, Outcome: recovered}})
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			})
			s.state.Operations["dream"] = journal{Path: "/application/sessions/main/prompt", Dream: true, Outcome: "accepted", TurnID: "turn"}
			v := s.state.Views["main"]
			v.CommandCaughtUp = true
			v.State.Run.Status, v.State.Run.TurnId = pointer("completed"), pointer("turn")
			v.Turns = map[string]string{"turn": "completed"}
			for i := range 3 {
				err := s.RenewConversation(t.Context(), "dream", "main")
				known := outcome != "unknown" && (outcome != "recovered-rejected" || i > 0)
				if err == nil || errors.Is(err, api.ErrConversationRenewalRejected) != known {
					t.Fatalf("attempt %d: rejection=%v err=%v", i, known, err)
				}
				if s.state.Session.SessionId != "main" || creates.Load() != 1 || s.Snapshot().CanSend != known {
					t.Fatal("lost source binding, repeated creation, or incorrect send gate", creates.Load(), s.Snapshot().CanSend)
				}
				// Both a known rejection and an unknown receipt survive process restart.
				restored := New(Options{Directory: filepath.Dir(s.path)})
				restored.client, restored.connected = s.client, true
				restored.state.Views["main"].CommandCaughtUp = true
				s = restored
			}
		})
	}
}

func TestConversationObservedOnlyAfterNativeCatchup(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	v := s.state.Views["main"]
	v.State.Run.Status, v.State.Run.TurnId = pointer("completed"), pointer("dream-turn")
	if s.ConversationState().Observed {
		t.Fatal("unrestored projection was authoritative")
	}
	v.CommandCaughtUp = true
	v.State.Run.Active, v.State.Run.Status = pointer(true), pointer("running")
	if got := s.ConversationState(); !got.Observed || got.Idle {
		t.Fatal("observed activity confused with idle", got)
	}
}

func TestDreamRenewalRejectionRequiresDurableReceipt(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) { t.Error("repeated native create") })
	s.state.Operations["dream"] = journal{Dream: true, Outcome: "accepted", TurnID: "turn"}
	s.state.Operations["dream-renew-"+digest([]byte("dream"))] = journal{Path: "/application/sessions", Outcome: "rejected"}
	v := s.state.Views["main"]
	v.CommandCaughtUp = true
	v.State.Run.Status, v.State.Run.TurnId = pointer("completed"), pointer("turn")
	s.path = filepath.Join(t.TempDir(), "blocked")
	if err := os.Mkdir(s.path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.RenewConversation(t.Context(), "dream", "main"); err == nil || errors.Is(err, api.ErrConversationRenewalRejected) {
		t.Fatal("persistence failure permitted fallback", err)
	}
}
