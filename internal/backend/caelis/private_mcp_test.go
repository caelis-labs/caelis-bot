package caelis

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func fixtureResidentClaim(s *Session) callbackClaim {
	return callbackClaim{client: s.client, generation: s.generation, call: wire.SessionToolCall{
		SessionId: "main", PrincipalId: "owner", TurnId: "t", ItemId: "item", Id: "receipt", CallId: "native-call",
		State: "claimed", Name: privateTasksPrefix + "bot_delegate", Arguments: json.RawMessage(`{"prompt":"fixture"}`),
	}}
}

func TestResidentMCPRequiresOriginalNativeClaim(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) {
		t.Error("claim validation requested unrelated Runtime state")
	})
	s.state.Session.Profile.Permissions = &wire.SessionPermissions{ApprovalMode: pointer("auto-review")}
	claim := fixtureResidentClaim(s)
	count := 0
	adapter := legacyTaskCallbackTools{Session: s, host: &acceptanceTools{call: func(ctx context.Context, name string, args json.RawMessage) api.ToolResult {
		count++
		if name != "bot_delegate" || s.WorkAdmission(ctx) != nil {
			t.Error("original claim did not reach the scoped Bot service")
		}
		return api.ToolResult{}
	}}}
	withClaim := func(c callbackClaim) context.Context { return context.WithValue(t.Context(), callbackClaimKey{}, c) }
	for _, tc := range []struct {
		label string
		edit  func(*callbackClaim)
	}{
		{"session", func(c *callbackClaim) { c.call.SessionId = "worker" }},
		{"principal", func(c *callbackClaim) { c.call.PrincipalId = "other" }},
		{"generation", func(c *callbackClaim) { c.generation++ }},
		{"claim status", func(c *callbackClaim) { c.call.State = "pending" }},
		{"tool scope", func(c *callbackClaim) { c.call.Name = "bot_gesture" }},
	} {
		invalid := claim
		tc.edit(&invalid)
		if out := adapter.CallTool(withClaim(invalid), claim.call.Name, claim.call.Arguments.(json.RawMessage)); !out.IsError {
			t.Fatal("missing native binding accepted", tc.label)
		}
	}
	if out := adapter.CallTool(t.Context(), claim.call.Name, claim.call.Arguments.(json.RawMessage)); !out.IsError {
		t.Fatal("missing claim accepted")
	}
	if out := adapter.CallTool(withClaim(claim), claim.call.Name, json.RawMessage(`{"prompt":"different"}`)); !out.IsError {
		t.Fatal("approval extended to changed arguments")
	}
	if out := adapter.CallTool(withClaim(claim), claim.call.Name, claim.call.Arguments.(json.RawMessage)); out.IsError || count != 1 {
		t.Fatal("valid native claim was not forwarded exactly once")
	}
	s.generation++
	if s.WorkAdmission(context.WithValue(t.Context(), legacyTaskInvocationKey{}, claim)) == nil {
		t.Fatal("revoked session claim still prepares work")
	}
}

func TestManualResidentMCPRequiresOriginalCommittedDecision(t *testing.T) {
	for _, option := range []string{"allow", "reject", "cancel"} {
		t.Run(option, func(t *testing.T) {
			approval := testApproval()
			name := privateTasksPrefix + "bot_delegate"
			approval.Subject = &wire.ApprovalSubject{Tool: wire.ApprovalField{State: wire.ApprovalFieldStateKnown, Text: &name}}
			approval.Permission["tool_call"] = map[string]any{"id": "native-call", "raw_input": map[string]any{"prompt": "fixture"}}
			approval.Permission["options"] = []map[string]any{
				{"id": "allow", "kind": "allow_once"},
				{"id": "reject", "kind": "reject_once"},
				{"id": "cancel", "kind": "cancel"},
			}
			head := wire.SessionState{SessionId: "main", Approval: wire.ApprovalState{Active: approval}}
			id := approvalID("instance", "main", approval)
			op := "approval-" + id
			var posts, reads atomic.Int32
			var committed, wrongSession atomic.Bool
			var bindingPath string
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "GET" && r.URL.Path == "/api/control/v1/sessions/main/state":
					writeFixture(w, head)
				case r.Method == "POST" && r.URL.Path == "/api/control/v1/sessions/main/approvals/approval/resolve":
					posts.Add(1)
					b, err := loadBinding(bindingPath)
					j := b.Operations[op]
					if err != nil || j.Outcome != "unknown" || (j.ManualMCP != nil) != (option == "allow") {
						t.Error("approval evidence was not journaled before POST", err)
					}
					if option == "allow" {
						drop(w)
					} else {
						writeFixture(w, wire.CommandResult{OperationId: op, Outcome: "committed"})
					}
				case r.Method == "GET" && r.URL.Path == "/api/control/v1/operations/"+op:
					reads.Add(1)
					outcome := wire.Outcome("accepted")
					if committed.Load() {
						outcome = "committed"
					}
					session := "main"
					if wrongSession.Load() {
						session = "other"
					}
					writeFixture(w, wire.CommandResult{OperationId: op, Outcome: outcome, SessionId: &session})
				default:
					t.Error("unexpected approval operation", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			})
			bindingPath = s.path
			s.state.Views["main"].State = head
			d := api.Decision{ID: id, Choice: option}
			err := s.Decide(t.Context(), d)
			claim := fixtureResidentClaim(s)
			if option != "allow" {
				if err != nil || s.admitLegacyTaskCallback(t.Context(), claim) || reads.Load() != 0 {
					t.Fatal("negative decision authorized a call", err)
				}
				return
			}
			if err == nil {
				t.Fatal("lost Allow response was reported as committed")
			}
			_ = s.Decide(t.Context(), d)
			brief, cancel := context.WithTimeout(t.Context(), 180*time.Millisecond)
			if s.admitLegacyTaskCallback(brief, claim) {
				t.Fatal("accepted receipt authorized before commit")
			}
			cancel()
			committed.Store(true)
			wrongSession.Store(true)
			if err := s.recoverOperation(t.Context(), op); err == nil {
				t.Fatal("a committed receipt with a mismatched Session authorized the original call")
			}
			wrongSession.Store(false)
			if err := s.recoverOperation(t.Context(), op); err != nil {
				t.Fatal(err)
			}
			if posts.Load() != 1 || reads.Load() < 2 || !s.admitLegacyTaskCallback(t.Context(), claim) {
				t.Fatal("original committed Allow did not resume its claim", posts.Load(), reads.Load())
			}
			claim.call.CallId = "different"
			if s.admitLegacyTaskCallback(t.Context(), claim) {
				t.Fatal("original approval authorized another native call")
			}
		})
	}
}

func TestPrivateMCPRecoveryCannotSpendRevokedPreparation(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("revoked preparation made a Core request", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})
	pending := worker{Native: true, Task: api.Task{ID: "task", Outcome: "unknown"}, Start: &workerStart{PrivateMCP: true}}
	s.state.Workers[pending.Task.ID] = pending
	task, err := s.advanceWorker(t.Context(), pending)
	if err == nil || task.Outcome != "rejected" || s.state.Workers[task.ID].Start != nil || len(s.state.Operations) != 0 {
		t.Fatal("undispatched private preparation retained write authority", task.Outcome, err)
	}
}
