package caelis

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func continuityFixture(t *testing.T, onCreate func(), extra ...http.HandlerFunc) (*Session, *atomic.Int32, api.ToolInvocation) {
	t.Helper()
	var creates atomic.Int32
	var profile wire.SessionProfile
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/sessions/main/configuration":
			writeFixture(w, wire.SessionConfiguration{SessionId: "main", Revision: "1", Profile: wire.SessionProfile{Execution: "workspace-write", Model: "original-model"}})
		case "/sessions":
			creates.Add(1)
			var in wire.CreateSessionRequest
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			profile = *in.Profile
			if profile.Reviewer != nil || profile.Permissions == nil || value(profile.Permissions.ApprovalMode) != "auto-review" {
				t.Error("renewal replaced Runtime Guardian ownership")
			}
			if onCreate != nil {
				onCreate()
			}
			writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "committed", SessionId: pointer("next")})
		case "/sessions/next/profile":
			writeFixture(w, wire.SessionBinding{SessionId: "next", PrincipalId: "owner", Profile: profile})
		case "/sessions/next/reviewer-state":
			writeFixture(w, wire.SessionReviewerState{SessionId: "next", ApprovalMode: "auto-review", Reviewer: &wire.SessionReviewer{Kind: "guardian", Model: "runtime/luna"}, Status: "ready"})
		case "/sessions/next/state":
			writeFixture(w, wire.SessionState{SessionId: "next"})
		default:
			if len(extra) != 0 {
				extra[0](w, r)
				return
			}
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	call := api.ToolInvocation{Provider: "caelis", CallID: "original", Session: "main", Turn: "turn"}
	s.state.Calls[call.CallID] = callRecord{Call: wire.SessionToolCall{Id: call.CallID, SessionId: call.Session, TurnId: call.Turn}, Receipt: &wire.SessionToolCallResult{Outcome: "succeeded", TurnComplete: pointer(true)}}
	v := s.state.Views[call.Session]
	v.CommandCaughtUp = true
	v.State.Run.Status, v.State.Run.TurnId = pointer("completed"), pointer(call.Turn)
	v.Turns = map[string]string{call.Turn: "completed"}
	s.state.Context.Injected = true
	return s, &creates, call
}

func TestGrantRevocationSharesRenewalAdmissionAndKeepsOriginalAuthority(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var revoked atomic.Bool
	s, _, call := continuityFixture(t, func() { close(entered); <-release }, func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/sessions/next/background-grants":
			var in wire.CreateSessionBackgroundGrantRequest
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			if in.AuthorizationOperationId != "original-user-operation" || in.Source != "original-source" {
				t.Error("renewal invented authorization")
			}
			writeFixture(w, wire.CommandResult{OperationId: in.OperationId, Outcome: "committed", BackgroundGrant: &wire.SessionBackgroundGrant{Id: "rebound-grant", SessionId: "next", Source: in.Source, AuthorizationOperationId: in.AuthorizationOperationId}})
		case "/sessions/next/background-grants/rebound-grant/revoke":
			var in wire.RevokeSessionBackgroundGrantRequest
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			revoked.Store(true)
			writeFixture(w, wire.CommandResult{OperationId: in.OperationId, Outcome: "committed", BackgroundGrant: &wire.SessionBackgroundGrant{Id: "rebound-grant", SessionId: "next", Revoked: true}})
		default:
			t.Errorf("revocation used stale target: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	s.state.Grants["original-schedule"] = grantRecord{Fingerprint: "original", Grant: wire.SessionBackgroundGrant{Id: "old-grant", SessionId: "main", Source: "original-source", AuthorizationOperationId: "original-user-operation"}}
	renewed := make(chan error, 1)
	go func() { _, err := s.RenewAfterTool(t.Context(), call); renewed <- err }()
	<-entered
	finished := make(chan error, 1)
	go func() { finished <- s.RevokeBackground(t.Context(), "original-schedule") }()
	close(release)
	if err := <-renewed; err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	grant := s.state.Grants["original-schedule"].Grant
	if !revoked.Load() || !grant.Revoked || grant.Id != "rebound-grant" || grant.AuthorizationOperationId != "original-user-operation" {
		t.Fatal("revocation was resurrected or rebound to new authority", grant)
	}
}

func TestRenewalDeferralRetainsSourceAndUnusedNativeReceipt(t *testing.T) {
	for _, duringCreate := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-create", true: "during-create"}[duringCreate], func(t *testing.T) {
			var allowed atomic.Bool
			allowed.Store(duringCreate)
			s, creates, call := continuityFixture(t, func() { allowed.Store(false) })
			ctx := api.WithRenewalGuard(t.Context(), func() error {
				if !allowed.Load() {
					return api.ErrConversationRenewalDeferred
				}
				return nil
			})
			if _, err := s.RenewAfterTool(ctx, call); !errors.Is(err, api.ErrConversationRenewalDeferred) {
				t.Fatal(err)
			}
			if s.state.Session.SessionId != call.Session || !s.state.Context.Injected || !s.Snapshot().CanSend {
				t.Fatal("lost usable source binding")
			}
			if !duringCreate {
				if creates.Load() != 0 {
					t.Fatal("created despite initial refusal")
				}
				return
			}
			op := "tool-renew-" + digest([]byte(call.CallID))
			restored := New(Options{Directory: filepath.Dir(s.path)})
			if j := restored.state.Operations[op]; j.Resource != "next" || !j.RenewalDeferred || j.Outcome != "committed" {
				t.Fatal("original native receipt lost", j)
			}
			allowed.Store(true)
			if _, err := s.RenewAfterTool(ctx, call); !errors.Is(err, api.ErrConversationRenewalDeferred) || creates.Load() != 1 {
				t.Fatal("adopted or replayed superseded creation", err)
			}
		})
	}
}

func TestRenewalKeepsPendingWorkerInteractionsAndOperations(t *testing.T) {
	s, creates, call := continuityFixture(t, nil)
	workerView := &view{State: wire.SessionState{SessionId: "worker", Approval: wire.ApprovalState{Active: &wire.ActiveApproval{RequestId: "original-worker-approval"}}}}
	s.state.Views["worker"] = workerView
	s.state.Workers["original-task"] = worker{Binding: wire.SessionBinding{SessionId: "worker"}, Task: api.Task{ID: "original-task"}}
	s.state.Operations["original-worker-operation"] = journal{Path: "/sessions/worker/prompt", Outcome: "unknown", TurnID: "original-worker-turn", Digest: "original-bytes"}
	if sid, err := s.RenewAfterTool(t.Context(), call); err != nil || sid != "next" || creates.Load() != 1 {
		t.Fatal("unrelated durable work blocked handoff", sid, err)
	}
	if s.state.Views["worker"] != workerView || workerView.State.Approval.Active.RequestId != "original-worker-approval" || s.state.Operations["original-worker-operation"].Digest != "original-bytes" || s.state.Operations["original-worker-operation"].Outcome != "unknown" || !s.Snapshot().CanSend {
		t.Fatal("renewal answered, replayed or rebound original Worker records")
	}
}

func TestRenewalStillBlocksUnresolvedResidentAndUnownedEffects(t *testing.T) {
	for _, path := range []string{"/sessions/main/prompt", "/plugins/example"} {
		t.Run(path, func(t *testing.T) {
			s, creates, call := continuityFixture(t, nil)
			s.state.Operations["original-effect"] = journal{Path: path, Outcome: "unknown"}
			if _, err := s.RenewAfterTool(t.Context(), call); err == nil || creates.Load() != 0 || s.state.Session.SessionId != "main" {
				t.Fatal("unresolved effect lost its admission fence", err)
			}
		})
	}
}

func TestUnknownToolCreationKeepsOldChatWithoutReplayingOrAdoptingLateResult(t *testing.T) {
	var creates atomic.Int32
	var known atomic.Bool
	call := api.ToolInvocation{Provider: "caelis", CallID: "original", Session: "main", Turn: "turn"}
	op := "tool-renew-" + digest([]byte(call.CallID))
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/api/control/v1") {
		case "/sessions/main/configuration":
			writeFixture(w, wire.SessionConfiguration{SessionId: "main", Revision: "1", Profile: wire.SessionProfile{Execution: "workspace-write"}})
		case "/sessions":
			creates.Add(1)
			drop(w)
		case "/operations/" + op:
			if known.Load() {
				writeFixture(w, wire.CommandResult{OperationId: op, Outcome: "committed", SessionId: pointer("unused-original-destination")})
			} else {
				writeFixture(w, wire.CommandResult{OperationId: op, Outcome: "unknown"})
			}
		case "/sessions/main/prompt":
			var in wire.PromptRequest
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			if value(in.OperationId) != "user-correction" {
				t.Error("changed original user input identity")
			}
			writeFixture(w, wire.CommandResult{OperationId: value(in.OperationId), Outcome: "accepted", Target: &wire.CommandTarget{TurnId: pointer("correction-turn")}})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	s.state.Calls[call.CallID] = callRecord{Call: wire.SessionToolCall{Id: call.CallID, SessionId: call.Session, TurnId: call.Turn}, Receipt: &wire.SessionToolCallResult{Outcome: "succeeded", TurnComplete: pointer(true)}}
	v := s.state.Views[call.Session]
	v.CommandCaughtUp = true
	v.State.Run.Status, v.State.Run.TurnId = pointer("completed"), pointer(call.Turn)
	v.Turns = map[string]string{call.Turn: "completed"}
	s.state.Context.Injected = true
	if _, err := s.RenewAfterTool(t.Context(), call); !errors.Is(err, api.ErrConversationRenewalUnknown) || creates.Load() != 1 || !s.Snapshot().CanSend {
		t.Fatal("unknown empty creation disabled the source", err, creates.Load(), s.Snapshot().CanSend)
	}
	restored := New(Options{Directory: filepath.Dir(s.path)})
	if j := restored.state.Operations[op]; j.Outcome != "unknown" || !j.RenewalDeferred || restored.state.Session.SessionId != "main" {
		t.Fatal("lost original unknown receipt", j)
	}
	other := call
	other.CallID = "different-dream"
	record := s.state.Calls[call.CallID]
	record.Call.Id = other.CallID
	s.state.Calls[other.CallID] = record
	if _, err := s.RenewAfterTool(t.Context(), other); err == nil || creates.Load() != 1 {
		t.Fatal("new ID bypassed unresolved original creation", err)
	}
	if receipt, err := s.Submit(t.Context(), api.Submission{ID: "user-correction", Text: "Cancel the original goal."}, nil); err != nil || receipt.Outcome != "accepted" {
		t.Fatal("old conversation cannot accept correction", receipt, err)
	}
	known.Store(true)
	if err := s.recoverOperation(t.Context(), op); err != nil {
		t.Fatal(err)
	}
	if j := s.state.Operations[op]; j.Outcome != "committed" || j.Resource != "unused-original-destination" || !j.RenewalDeferred || creates.Load() != 1 || s.state.Session.SessionId != "main" {
		t.Fatal("late original receipt was lost, replayed or adopted", j)
	}
}
