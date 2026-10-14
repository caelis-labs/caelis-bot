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

func TestMainCreateReceiptTargetCannotActivateDifferentBinding(t *testing.T) {
	op := "create-original"
	profile := wire.ApplicationProfile{Execution: "workspace-write", Permissions: &wire.ApplicationPermissions{Mode: pointer("workspace-write"), ApprovalMode: pointer("auto-review")}}
	gets, posts := 0, 0
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			t.Fatal("original create was redispatched")
		}
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/application/sessions/A") {
			t.Fatalf("unexpected binding read: %s %s", r.Method, r.URL.Path)
		}
		gets++
		writeFixture(w, wire.ApplicationBinding{SessionId: "B", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Profile: profile})
	})
	s.state.Session = wire.ApplicationBinding{}
	s.state.CreateID = op
	s.state.Operations[op] = journal{Path: "/application/sessions", Outcome: "committed", Resource: "A", CreationAuthority: &creationAuthority{Permissions: profile.Permissions}}
	s.retainedWorkers = true
	if err := s.ensureSession(t.Context(), s.client); err == nil || gets != 1 || posts != 0 || s.state.Session.SessionId != "" || s.state.CreateID != op || s.state.Operations[op].Resource != "A" {
		t.Fatal("mismatched binding replaced original create target or receipt", err, gets, posts, s.state.Session, s.state.Operations[op])
	}
}

func TestNewAndRenewedBindingsRejectAuthorityMismatchWithoutCutover(t *testing.T) {
	profile := wire.ApplicationProfile{Execution: "workspace-write", Permissions: &wire.ApplicationPermissions{Mode: pointer("danger-full-access"), ApprovalMode: pointer("auto-review"), ExecutionApproval: pointer("never")}, Reviewer: &wire.ApplicationReviewer{Kind: "guardian", Model: "review-model"}}
	for _, mismatch := range []string{"sandbox", "approval", "execution-approval", "reviewer", "missing-request"} {
		t.Run(mismatch, func(t *testing.T) {
			returned := clone(profile)
			switch mismatch {
			case "sandbox":
				returned.Permissions.Mode = pointer("workspace-write")
			case "approval":
				returned.Permissions.ApprovalMode = pointer("manual")
			case "execution-approval":
				returned.Permissions.ExecutionApproval = nil
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
	for _, mismatch := range []string{"sandbox", "approval", "execution-approval", "reviewer"} {
		t.Run("renew-"+mismatch, func(t *testing.T) {
			returned := clone(profile)
			switch mismatch {
			case "sandbox":
				returned.Permissions.Mode = pointer("workspace-write")
			case "approval":
				returned.Permissions.ApprovalMode = pointer("manual")
			case "execution-approval":
				returned.Permissions.ExecutionApproval = nil
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

func TestLegacyFullBindingResumesUnderOriginalAuthority(t *testing.T) {
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
	s.retainedWorkers = true
	if err := s.ensureSession(t.Context(), s.client); err != nil || s.state.Session.SessionId != "new" || s.state.Session.Profile.Permissions.ExecutionApproval != nil {
		t.Fatal("old binding was replaced by future full-access contract", err, s.state.Session)
	}
	if len(s.ExecutionOptions().ApprovalModes) != 1 || s.ExecutionOptions().ApprovalModes[0].ID != "auto" {
		t.Fatal("unsupported Caelis full access was offered")
	}
}

func sameCreationAuthority(j journal, profile wire.ApplicationProfile) bool {
	return j.CreationAuthority != nil && j.CreationAuthority.Permissions != nil && profile.Permissions != nil &&
		value(j.CreationAuthority.Permissions.Mode) == value(profile.Permissions.Mode) &&
		value(j.CreationAuthority.Permissions.ApprovalMode) == value(profile.Permissions.ApprovalMode) &&
		value(j.CreationAuthority.Permissions.ExecutionApproval) == value(profile.Permissions.ExecutionApproval) &&
		j.CreationAuthority.Reviewer != nil && profile.Reviewer != nil && *j.CreationAuthority.Reviewer == *profile.Reviewer
}

func TestLegacyCreationJournalRecoversOnlyOriginalAuthority(t *testing.T) {
	for _, outcome := range []string{"unknown", "accepted", "committed"} {
		t.Run(outcome, func(t *testing.T) {
			op := "legacy-create"
			profile := wire.ApplicationProfile{Execution: "workspace-write", Model: "old-model", Permissions: &wire.ApplicationPermissions{Mode: pointer("workspace-write"), ApprovalMode: pointer("auto-review")}, Reviewer: &wire.ApplicationReviewer{Kind: "guardian", Model: "old-reviewer"}}
			body, err := json.Marshal(wire.CreateApplicationSessionRequest{OperationId: &op, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			j := journal{Path: "/application/sessions", Body: body, Digest: digest(body), Outcome: outcome, Resource: "created"}
			recoverCreationAuthority(op, &j)
			if !sameCreationAuthority(j, profile) {
				t.Fatal("old body did not restore immutable authority")
			}
			binding := wire.ApplicationBinding{SessionId: "created", Profile: profile}
			if err := verifyCreatedAuthority(op, j, binding); err != nil {
				t.Fatal(err)
			}
			j.CreationAuthority = nil
			j.Body = nil // Committed/accepted old journals cleared Body before upgrade.
			if err := verifyCreatedAuthority(op, j, binding); err != nil {
				t.Fatal("digest-anchored old binding not recoverable", err)
			}
			binding.Profile.Permissions.Mode = pointer("danger-full-access")
			if err := verifyCreatedAuthority(op, j, binding); err == nil {
				t.Fatal("future or changed authority inferred from old receipt")
			}
		})
	}
}

func TestLegacyCreationJournalUnknownEvidenceStaysOriginal(t *testing.T) {
	op := "legacy-create"
	profile := wire.ApplicationProfile{Execution: "workspace-write", Model: "old-model", Permissions: &wire.ApplicationPermissions{Mode: pointer("workspace-write"), ApprovalMode: pointer("manual")}}
	body, _ := json.Marshal(wire.CreateApplicationSessionRequest{OperationId: &op, Profile: profile})
	for _, j := range []journal{
		{Path: "/application/sessions", Outcome: "unknown", Resource: "created"},
		{Path: "/application/sessions", Outcome: "accepted", Resource: "created", Digest: "wrong"},
		{Path: "/application/sessions", Outcome: "accepted", Resource: "different", Digest: digest(body)},
	} {
		if err := verifyCreatedAuthority(op, j, wire.ApplicationBinding{SessionId: "created", Profile: profile}); err == nil {
			t.Fatal("unproven legacy authority activated", j)
		}
	}
}

func TestNewCaelisPermissionContractsRequireCapabilityAndSeparateGrants(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("read-only capability test dispatched request") })
	s.client.schema = true
	s.info.Capabilities = []string{"application-execution-approval-never-v1", "worker-creation-permissions-v1"}
	if s.checkFutureBotMode("full-access") == nil || s.CheckFutureWorkExecutionMode("full-access") == nil {
		t.Fatal("missing independent Host grants accepted")
	}
	s.state.Connection.AllowUnreviewedApplicationExecution = pointer(true)
	if err := s.checkFutureBotMode("full-access"); err != nil {
		t.Fatal(err)
	}
	if s.CheckFutureWorkExecutionMode("full-access") == nil {
		t.Fatal("Bot grant leaked into Worker")
	}
	s.state.Connection.AllowWorkerFullAccess = pointer(true)
	if err := s.CheckFutureWorkExecutionMode("full-access"); err != nil {
		t.Fatal(err)
	}
	s.execution.ApprovalMode = "full-access"
	profile := wire.ApplicationProfile{Execution: "workspace-write", Model: "model"}
	s.configureReviewer(&profile)
	if value(profile.Permissions.Mode) != "danger-full-access" || value(profile.Permissions.ApprovalMode) != "auto-review" || value(profile.Permissions.ExecutionApproval) != "never" {
		t.Fatal("full access profile lacks fixed contract", profile.Permissions)
	}
	s.client.schema = false
	if s.checkFutureBotMode("full-access") == nil || s.CheckFutureWorkExecutionMode("auto") == nil {
		t.Fatal("unnegotiated Host accepted new permissions")
	}
}

func TestLegacyCreateReceiptRecoveryKeepsOriginalOperation(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		body          bool
	}{
		{"pending-body", "unknown", true}, {"pending-cleared", "unknown", false},
		{"accepted-cleared", "accepted", false}, {"committed-cleared", "committed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := "create-original"
			profile := wire.ApplicationProfile{Execution: "workspace-write", Model: "historical", Permissions: &wire.ApplicationPermissions{Mode: pointer("workspace-write"), ApprovalMode: pointer("manual")}}
			raw, _ := json.Marshal(wire.CreateApplicationSessionRequest{OperationId: &op, Profile: profile})
			posts := 0
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					t.Fatal("legacy operation was redispatched")
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/application/operations/"+op):
					writeFixture(w, wire.ApplicationOperation{OperationId: op, Outcome: "accepted", Result: &wire.CommandResult{OperationId: op, Outcome: "accepted", SessionId: pointer("historical-session")}})
				case strings.HasSuffix(r.URL.Path, "/application/sessions/historical-session"):
					writeFixture(w, wire.ApplicationBinding{SessionId: "historical-session", ApplicationId: "app", ConnectionId: "client", PrincipalId: "owner", Profile: profile})
				default:
					t.Fatalf("unexpected recovery route %s", r.URL.Path)
				}
			})
			s.retainedWorkers = true
			s.state.Session = wire.ApplicationBinding{}
			s.state.CreateID = op
			j := journal{Path: "/application/sessions", Outcome: tc.outcome, Resource: "historical-session", Digest: digest(raw)}
			if tc.body {
				j.Body = raw
				j.Resource = ""
			}
			s.state.Operations[op] = j
			if err := s.ensureSession(t.Context(), s.client); err != nil {
				t.Fatal(err)
			}
			saved := s.state.Operations[op]
			if posts != 0 || s.state.Session.SessionId != "historical-session" || value(s.state.Session.Profile.Permissions.ApprovalMode) != "manual" || saved.Resource != "historical-session" {
				t.Fatal("legacy authority or receipt changed", posts, saved, s.state.Session)
			}
			if tc.body && (saved.CreationAuthority == nil || len(saved.Body) != 0) {
				t.Fatal("pending Body was cleared without durable authority", saved)
			}
		})
	}
}
