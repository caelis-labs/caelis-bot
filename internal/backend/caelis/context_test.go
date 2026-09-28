package caelis

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/notebook"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
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
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/application/sessions/main/configuration":
			writeFixture(w, wire.ApplicationConfiguration{SessionId: "main", Revision: "1", Profile: wire.ApplicationProfile{Model: "retained-model", Execution: "workspace-write"}})
		case "/application/sessions":
			creates.Add(1)
			var in wire.CreateApplicationSessionRequest
			json.NewDecoder(r.Body).Decode(&in)
			if in.Profile.Model != "retained-model" {
				t.Error("model changed")
			}
			writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "committed", SessionId: pointer("next")})
		case "/application/sessions/next":
			writeFixture(w, wire.ApplicationBinding{SessionId: "next", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Profile: wire.ApplicationProfile{Execution: "workspace-write"}})
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
