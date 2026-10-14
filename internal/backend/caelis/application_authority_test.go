package caelis

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestApplicationCreateJournalPinsAuthorityAfterReceipt(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/application/sessions") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeFixture(w, wire.CommandResult{OperationId: "create-pinned", Outcome: "committed", SessionId: pointer("new")})
	})
	profile := wire.ApplicationProfile{Execution: "workspace-write", Permissions: &wire.ApplicationPermissions{Mode: pointer("danger-full-access"), ApprovalMode: pointer("auto-review")}, Reviewer: &wire.ApplicationReviewer{Kind: "guardian", Model: "review-model"}}
	if _, err := s.command(t.Context(), "create-pinned", "/application/sessions", wire.CreateApplicationSessionRequest{OperationId: pointer("create-pinned"), Profile: profile}); err != nil {
		t.Fatal(err)
	}
	j := s.state.Operations["create-pinned"]
	if len(j.Body) != 0 || j.CreationAuthority == nil || !sameCreationAuthority(j, profile) {
		t.Fatalf("committed create lost its fixed authority: %+v", j)
	}
	stored, err := privateRead(s.path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var restored binding
	if err := json.Unmarshal(stored, &restored); err != nil || !sameCreationAuthority(restored.Operations["create-pinned"], profile) {
		t.Fatal("durable create lost authority", err)
	}
}

func TestNewAndRenewedBindingsRejectAuthorityMismatchWithoutCutover(t *testing.T) {
	profile := wire.ApplicationProfile{Execution: "workspace-write", Permissions: &wire.ApplicationPermissions{Mode: pointer("danger-full-access"), ApprovalMode: pointer("auto-review")}, Reviewer: &wire.ApplicationReviewer{Kind: "guardian", Model: "review-model"}}
	for _, mismatch := range []string{"sandbox", "approval", "reviewer", "missing-request"} {
		t.Run(mismatch, func(t *testing.T) {
			returned := clone(profile)
			switch mismatch {
			case "sandbox":
				returned.Permissions.Mode = pointer("workspace-write")
			case "approval":
				returned.Permissions.ApprovalMode = pointer("manual")
			case "reviewer":
				returned.Reviewer = &wire.ApplicationReviewer{Kind: "guardian", Model: "other"}
			}
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/application/sessions/new") {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				writeFixture(w, wire.ApplicationBinding{SessionId: "new", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Profile: returned})
			})
			s.state.Session = wire.ApplicationBinding{}
			s.state.CreateID = "create-pinned"
			s.state.Operations["create-pinned"] = journal{Path: "/application/sessions", Outcome: "committed", Resource: "new", CreationAuthority: &creationAuthority{Permissions: profile.Permissions, Reviewer: profile.Reviewer}}
			if mismatch == "missing-request" {
				s.state.Operations["create-pinned"] = journal{Path: "/application/sessions", Outcome: "committed", Resource: "new"}
			}
			if err := s.ensureSession(t.Context(), s.client); err == nil || s.state.Session.SessionId != "" || s.state.Operations["create-pinned"].Resource != "new" {
				t.Fatal("new binding mismatch activated or lost its receipt", err)
			}
		})
	}
	for _, mismatch := range []string{"sandbox", "approval", "reviewer"} {
		t.Run("renew-"+mismatch, func(t *testing.T) {
			returned := clone(profile)
			switch mismatch {
			case "sandbox":
				returned.Permissions.Mode = pointer("workspace-write")
			case "approval":
				returned.Permissions.ApprovalMode = pointer("manual")
			case "reviewer":
				returned.Reviewer = &wire.ApplicationReviewer{Kind: "guardian", Model: "other"}
			}
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/application/sessions/new") {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				writeFixture(w, wire.ApplicationBinding{SessionId: "new", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Profile: returned})
			})
			s.state.Operations["dream"] = journal{Dream: true, Outcome: "accepted", TurnID: "turn"}
			s.state.Operations["dream-renew-"+digest([]byte("dream"))] = journal{Path: "/application/sessions", Outcome: "committed", Resource: "new", CreationAuthority: &creationAuthority{Permissions: profile.Permissions, Reviewer: profile.Reviewer}}
			v := s.state.Views["main"]
			v.CommandCaughtUp = true
			v.State.Run.Status = pointer("completed")
			v.State.Run.TurnId = pointer("turn")
			v.Turns = map[string]string{"turn": "completed"}
			if err := s.RenewConversation(t.Context(), "dream", "main"); err == nil || s.state.Session.SessionId != "main" || s.state.RenewedBy != "" {
				t.Fatal("renewed binding mismatch replaced existing session", err)
			}
		})
	}
}

func TestExistingBindingRecoveryKeepsOriginalPermissions(t *testing.T) {
	old := wire.ApplicationProfile{Execution: "workspace-write", Permissions: &wire.ApplicationPermissions{Mode: pointer("workspace-write"), ApprovalMode: pointer("manual")}}
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/application/sessions/main") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeFixture(w, wire.ApplicationBinding{SessionId: "main", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Profile: old})
	})
	s.state.Session.Profile = old
	s.SetFutureExecutionMode("full-access")
	s.retainedWorkers = true // Skip unrelated configuration reads after binding recovery.
	if err := s.ensureSession(t.Context(), s.client); err != nil || value(s.state.Session.Profile.Permissions.Mode) != "workspace-write" || value(s.state.Session.Profile.Permissions.ApprovalMode) != "manual" {
		t.Fatal("existing binding inherited future full access", err, s.state.Session.Profile)
	}
}

func TestFullAccessBindingCannotActivateBeforeCoreExecutionApprovalContract(t *testing.T) {
	profile := wire.ApplicationProfile{Execution: "workspace-write", Permissions: &wire.ApplicationPermissions{Mode: pointer("danger-full-access"), ApprovalMode: pointer("auto-review")}, Reviewer: &wire.ApplicationReviewer{Kind: "guardian", Model: "review-model"}}
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/application/sessions/new") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeFixture(w, wire.ApplicationBinding{SessionId: "new", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Profile: profile})
	})
	s.state.Session = wire.ApplicationBinding{}
	s.state.CreateID = "create-pinned"
	s.state.Operations["create-pinned"] = journal{Path: "/application/sessions", Outcome: "committed", Resource: "new", CreationAuthority: &creationAuthority{Permissions: profile.Permissions, Reviewer: profile.Reviewer}}
	if err := s.ensureSession(t.Context(), s.client); err == nil || s.state.Session.SessionId != "" {
		t.Fatal("unavailable full access binding was activated", err)
	}
	if len(s.ExecutionOptions().ApprovalModes) != 1 || s.ExecutionOptions().ApprovalModes[0].ID != "auto" {
		t.Fatal("unsupported Caelis full access was offered")
	}
}

func sameCreationAuthority(j journal, profile wire.ApplicationProfile) bool {
	return j.CreationAuthority != nil && j.CreationAuthority.Permissions != nil && profile.Permissions != nil &&
		value(j.CreationAuthority.Permissions.Mode) == value(profile.Permissions.Mode) &&
		value(j.CreationAuthority.Permissions.ApprovalMode) == value(profile.Permissions.ApprovalMode) &&
		j.CreationAuthority.Reviewer != nil && profile.Reviewer != nil && *j.CreationAuthority.Reviewer == *profile.Reviewer
}
