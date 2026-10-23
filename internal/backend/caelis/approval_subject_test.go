package caelis

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func subjectField(state wire.ApprovalFieldState, text string) wire.ApprovalField {
	return wire.ApprovalField{State: state, Text: pointer(text)}
}

func subjectFixture() *wire.ApprovalSubject {
	return &wire.ApprovalSubject{
		Kind: "file", Title: pointer(subjectField(wire.ApprovalFieldStateKnown, "Use the protected fixture")),
		Description: pointer(subjectField(wire.ApprovalFieldStateKnown, "The original native owner needs a decision.")),
		Tool:        subjectField(wire.ApprovalFieldStateKnown, "FixtureTool"),
		Operation:   subjectField(wire.ApprovalFieldStateKnown, "Write fixture output"),
		Effect:      subjectField(wire.ApprovalFieldStateTruncated, "Bounded write description…"),
		Scope:       subjectField(wire.ApprovalFieldStateUnavailable, "unavailable-sentinel"),
		Targets: []wire.ApprovalField{
			subjectField(wire.ApprovalFieldStateKnown, "fixture/output"),
			subjectField(wire.ApprovalFieldStateTruncated, "fixture/more…"),
			subjectField(wire.ApprovalFieldStateOpaque, "opaque-sentinel"),
			{State: wire.ApprovalFieldStateKnown, Text: pointer("sensitive-sentinel"), Sensitive: pointer(true)},
		},
		Grants: []wire.ApprovalGrant{
			{OptionId: "native-allow", Scope: subjectField(wire.ApprovalFieldStateKnown, "this operation")},
			{OptionId: "native-reject", Scope: wire.ApprovalField{State: wire.ApprovalFieldStateOpaque, Sensitive: pointer(true), Text: pointer("grant-sentinel")}},
		},
		TargetsTruncated: pointer(true), GrantsTruncated: pointer(true), SensitiveInput: pointer(true), OpaqueInput: pointer(true), Reported: pointer(false),
	}
}

func subjectSnapshot(t *testing.T, a *wire.ActiveApproval, worker bool) api.Approval {
	t.Helper()
	s := New(Options{Directory: filepath.Join(t.TempDir(), "private")})
	s.connected = true
	s.state.InstanceID = "instance"
	s.state.Session = wire.SessionBinding{PrincipalId: "owner", SessionId: "main"}
	s.state.Views["main"] = &view{State: wire.SessionState{SessionId: "main"}, Seen: map[string]bool{}}
	sid := "main"
	if worker {
		sid = "work"
		addServiceWorker(s, "task", sid, nativeWorkHead("later-turn"))
	}
	s.state.Views[sid].State.Approval.Active = a
	out := s.Snapshot()
	if len(out.Approvals) != 1 {
		t.Fatal("original native request was not projected", out.Approvals)
	}
	return out.Approvals[0]
}

func TestApprovalSubjectPreservesPublicFactsForResidentAndWorker(t *testing.T) {
	for _, worker := range []bool{false, true} {
		t.Run(map[bool]string{false: "resident", true: "worker"}[worker], func(t *testing.T) {
			a := testApproval()
			a.Subject = subjectFixture()
			a.Permission["title"] = "private-title-sentinel"
			a.Permission["description"] = "private-description-sentinel"
			a.Permission["context"] = map[string]any{"title": "private-context-sentinel", "raw_subject": "private-evidence-sentinel", "native_request_id": "private-request-sentinel"}
			before, _ := json.Marshal(a)
			got := subjectSnapshot(t, a, worker)
			if got.Title != "Use the protected fixture" || got.Description != "The original native owner needs a decision." || got.Action != "Write fixture output" || got.Target != "fixture/output\nfixture/more…" {
				t.Fatalf("bounded native display facts were lost: %+v", got)
			}
			if got.NativeID != a.RequestId || got.ID != approvalID("instance", map[bool]string{false: "main", true: "work"}[worker], a) || got.Status != "pending" {
				t.Fatal("presentation changed original native identity", got)
			}
			if worker && (got.Owner != "task" || got.TaskID != "task") || !worker && got.Owner != "conversation" {
				t.Fatal("presentation changed request ownership", got)
			}
			wantChoices := []api.Choice{{ID: "native-allow", Label: "Allow once", Scope: "allow_once"}, {ID: "native-reject", Label: "Reject once", Scope: "reject_once"}}
			if !reflect.DeepEqual(got.Choices, wantChoices) || got.GuardianEligible() {
				t.Fatal("native choices or limited Agent permission changed", got)
			}
			var details struct {
				Subject *wire.ApprovalSubject `json:"subject"`
			}
			if json.Unmarshal([]byte(got.Details), &details) != nil || details.Subject == nil {
				t.Fatal("complete bounded subject is not available", got.Details)
			}
			subject := details.Subject
			if !value(subject.SensitiveInput) || !value(subject.OpaqueInput) || subject.Reported == nil || value(subject.Reported) || !value(subject.TargetsTruncated) || !value(subject.GrantsTruncated) || subject.Effect.State != wire.ApprovalFieldStateTruncated || subject.Scope.State != wire.ApprovalFieldStateUnavailable || len(subject.Grants) != 2 || subject.Grants[1].OptionId != "native-reject" || !value(subject.Grants[1].Scope.Sensitive) || subject.Targets[2].State != wire.ApprovalFieldStateOpaque || !value(subject.Targets[3].Sensitive) {
				t.Fatalf("native uncertainty or presence markers were lost: %+v", subject)
			}
			visible, _ := json.Marshal(got)
			for _, secret := range []string{"private-", "opaque-sentinel", "sensitive-sentinel", "grant-sentinel", "unavailable-sentinel", "printf fixture"} {
				if strings.Contains(string(visible), secret) {
					t.Fatalf("hidden native evidence leaked into presentation: %q", secret)
				}
			}
			after, _ := json.Marshal(a)
			if string(before) != string(after) {
				t.Fatal("display projection mutated authoritative native request")
			}
		})
	}
}

func TestApprovalReportedHiddenSubjectKeepsPresenceOnly(t *testing.T) {
	a := testApproval()
	a.Subject = subjectFixture()
	a.Subject.Reported = pointer(true)
	before, _ := json.Marshal(a)
	got := subjectSnapshot(t, a, true)
	if got.Title != "" || got.Description != "" || got.Action != "" || got.Target != "" || got.TitleKey == "" || got.GuardianEligible() || len(got.Choices) != 2 {
		t.Fatal("remote private evidence became a native display fact or grant", got)
	}
	var details struct {
		Subject *wire.ApprovalSubject `json:"subject"`
	}
	if json.Unmarshal([]byte(got.Details), &details) != nil || details.Subject == nil || !value(details.Subject.Reported) || !value(details.Subject.SensitiveInput) || !value(details.Subject.OpaqueInput) || details.Subject.Scope.State != wire.ApprovalFieldStateUnavailable || details.Subject.Title.State != wire.ApprovalFieldStateOpaque || details.Subject.Grants[0].OptionId != "native-allow" {
		t.Fatal("reported hidden subject lost uncertainty, presence or native option join", got.Details)
	}
	if strings.Contains(got.Details, `"text"`) || strings.Contains(got.Details, "printf fixture") {
		t.Fatal("reported marked input retained derived text", got.Details)
	}
	after, _ := json.Marshal(a)
	if string(before) != string(after) {
		t.Fatal("privacy projection rewrote the original native request")
	}
}

func TestApprovalPermissionTitleDescriptionAndContext(t *testing.T) {
	tests := []struct {
		name               string
		permission         wire.JSONObject
		subject            *wire.ApprovalSubject
		title, description string
	}{
		{name: "independent permission", permission: wire.JSONObject{"title": "Native permission", "description": "Native explanation"}, title: "Native permission", description: "Native explanation"},
		{name: "durable context", permission: wire.JSONObject{"context": map[string]any{"title": "Context permission", "description": "Context explanation", "raw_subject": "private-context-sentinel"}}, title: "Context permission", description: "Context explanation"},
		{name: "unavailable is not fallback", permission: wire.JSONObject{"title": "private-title-sentinel"}, subject: &wire.ApprovalSubject{Title: pointer(wire.ApprovalField{State: wire.ApprovalFieldStateUnavailable})}},
		{name: "sensitive is not fallback", permission: wire.JSONObject{"title": "private-title-sentinel"}, subject: &wire.ApprovalSubject{SensitiveInput: pointer(true)}},
		{name: "opaque is not fallback", permission: wire.JSONObject{"context": map[string]any{"title": "private-title-sentinel"}}, subject: &wire.ApprovalSubject{OpaqueInput: pointer(true)}},
		{name: "field sensitivity", permission: wire.JSONObject{}, subject: &wire.ApprovalSubject{Title: pointer(wire.ApprovalField{State: wire.ApprovalFieldStateKnown, Text: pointer("private-title-sentinel"), Sensitive: pointer(true)})}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := testApproval()
			a.Permission, a.Subject = tc.permission, tc.subject
			got := subjectSnapshot(t, a, true)
			if got.Title != tc.title || got.Description != tc.description || got.Details == "" || got.GuardianEligible() {
				t.Fatalf("public title/context was lost or changed eligibility: %+v", got)
			}
			visible, _ := json.Marshal(got)
			if strings.Contains(string(visible), "private-") {
				t.Fatal("private native context became presentation", string(visible))
			}
		})
	}
}

func TestApprovalLegacyToolDisplayAndSensitivePresence(t *testing.T) {
	a := testApproval()
	got := subjectSnapshot(t, a, true)
	if got.Title != "Verify fixture" || !strings.Contains(got.Details, "printf fixture") || got.GuardianEligible() {
		t.Fatal("known unmarked legacy tool changed display or Agent permission", got)
	}
	a.Permission["tool_call"] = map[string]any{"title": "private-title-sentinel", "raw_input": map[string]any{"sensitive": true, "value": "private-input-sentinel"}}
	got = subjectSnapshot(t, a, true)
	visible, _ := json.Marshal(got)
	if strings.Contains(string(visible), "private-") || !strings.Contains(got.Details, "sensitive_input") || got.GuardianEligible() || len(got.Choices) != 2 {
		t.Fatal("explicit sensitive legacy input was exposed or original options changed", got)
	}
}

func TestWorkApprovalSubjectDecisionUsesOriginalNativeReceipt(t *testing.T) {
	a := testApproval()
	a.Subject = subjectFixture()
	head := nativeWorkHead("later-turn")
	head.Approval.Active = a
	var posts atomic.Int32
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			writeFixture(w, head)
			return
		}
		posts.Add(1)
		var req wire.ResolveApprovalRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Target != *a.Target || req.ApprovalRequestId != a.RequestId || value(req.OptionId) != "native-allow" || !req.Approved || value(req.OperationId) != "work-decision-"+digest([]byte("original-decision")) {
			t.Error("Subject presentation changed original request, target, option or operation")
		}
		writeFixture(w, wire.CommandResult{OperationId: value(req.OperationId), Outcome: "committed"})
	})
	addServiceWorker(s, "task", "work", head)
	out, err := s.ObserveWork(t.Context(), "task")
	if err != nil || len(out.Approvals) != 1 || out.Approvals[0].Title != "Use the protected fixture" || out.Approvals[0].NativeID != a.RequestId || out.Approvals[0].TaskID != "task" {
		t.Fatal("scoped observation lost native Subject", out, err)
	}
	in := api.WorkDecision{TaskID: "task", RequestID: "original-decision", Decision: api.Decision{ID: out.Approvals[0].ID, Choice: "native-allow"}}
	if err = s.DecideWork(t.Context(), in); err != nil || posts.Load() != 1 {
		t.Fatal("original native decision failed", err, posts.Load())
	}
	if err = s.DecideWork(t.Context(), in); err != nil || posts.Load() != 1 {
		t.Fatal("original receipt repeated a decision", err, posts.Load())
	}
	receipt, err := s.ReadWorkOperation(t.Context(), api.WorkOperationQuery{TaskID: in.TaskID, RequestID: in.RequestID, Kind: "decision", Fingerprint: api.WorkFingerprint(in)})
	if err != nil || receipt.Outcome != "accepted" {
		t.Fatal("original native receipt was lost", receipt, err)
	}
}
