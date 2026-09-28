package caelis

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestGuardianCapabilityFailsBeforeMutation(t *testing.T) {
	s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/initialize") {
			t.Fatal("unexpected mutation")
		}
		caps := slices.DeleteFunc(slices.Clone(required), func(c string) bool { return c == "application-guardian-review-v1" })
		writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: caps})
	})
	if _, err := initialize(t.Context(), s.client); !errors.Is(err, errBotIncompatible) {
		t.Fatal("old Host accepted", err)
	}
}

func TestCallbackPolicyRequiresReviewUnlessExplicitlyDirect(t *testing.T) {
	s := New(Options{Directory: t.TempDir()})
	if err := s.ConfigureBotTools(&api.ToolConnection{Host: &acceptanceTools{defs: fixtureDefinitions("string")}, ApprovedTools: []string{"FixtureLookup"}}); err != nil {
		t.Fatal(err)
	}
	for _, d := range s.profile.Tools {
		want := "required"
		if d.Name == "FixtureLookup" {
			want = "direct"
		}
		if value(d.ApprovalPolicy) != want {
			t.Fatal(d.Name, d.ApprovalPolicy)
		}
	}
}

func TestReviewerReadinessAndLegacyManualAreCreationBound(t *testing.T) {
	for _, status := range []string{"ready", "unavailable", "manual", "mismatch"} {
		t.Run(status, func(t *testing.T) {
			reviewer := &wire.ApplicationReviewer{Kind: "guardian", Model: "explicit/model"}
			b := wire.ApplicationBinding{SessionId: "main", Profile: wire.ApplicationProfile{Permissions: &wire.ApplicationPermissions{ApprovalMode: pointer("auto-review")}, Reviewer: reviewer}}
			response := wire.ApplicationReviewerState{SessionId: "main", ApprovalMode: "auto-review", Reviewer: reviewer, Status: status}
			if status == "manual" {
				b.Profile.Reviewer = nil
				b.Profile.Permissions.ApprovalMode = pointer("manual")
				response.Reviewer = nil
				response.ApprovalMode = "manual"
			}
			if status == "mismatch" {
				response.Status = "ready"
				response.SessionId = "other"
			}
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/main/reviewer-state") {
					t.Fatal("reviewer read mutated configuration")
				}
				writeFixture(w, response)
			})
			err := s.checkReviewer(t.Context(), b)
			if (err == nil) != (status == "ready" || status == "manual") {
				t.Fatal(status, err)
			}
		})
	}
}

func TestReviewFactsPreserveIdentityAndNeverGrantManualAuthority(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("review fact dispatched mutation") })
	s.state.Session.Profile.Permissions = &wire.ApplicationPermissions{ApprovalMode: pointer("auto-review")}
	v := s.state.Views["main"]
	v.State.Run = wire.RunState{Active: pointer(true), TurnId: pointer("turn")}
	v.State.Approval.Active = testApproval()
	if len(s.Snapshot().Approvals) != 0 {
		t.Fatal("automatic approval exposed before progress")
	}
	if err := s.Decide(t.Context(), api.Decision{ID: approvalID(s.state.InstanceID, "main", v.State.Approval.Active), Choice: "native-allow"}); err == nil {
		t.Fatal("automatic review manually resolved")
	}
	e := wire.Envelope{Kind: "caelis/approval_review", SessionId: pointer("main"), TurnId: pointer("turn"), ApprovalRequestId: pointer("approval"), ApprovalReview: &wire.ApprovalReview{ItemId: pointer("item"), ToolCallId: pointer("reused-provider-id"), ToolName: pointer("FixtureLookup"), RawInput: wire.JSONObject{"key": "one"}, Status: pointer("in_progress")}}
	applyEnvelope(v, e)
	if len(s.Snapshot().Reviews) != 1 || s.Snapshot().Reviews[0].Status != "inProgress" || !value(v.State.Run.Active) {
		t.Fatal("review changed lifecycle")
	}
	e.Delivery = wire.Delivery{Mode: wire.DeliveryModeMirror}
	e.ApprovalReview.Status = pointer("denied")
	e.ApprovalReview.Text = pointer("Not authorized")
	applyEnvelope(v, e)
	e.ApprovalReview.Status = pointer("in_progress")
	applyEnvelope(v, e)
	if got := s.Snapshot().Reviews; len(got) != 1 || got[0].Status != "denied" || !strings.Contains(got[0].Action, `"key": "one"`) {
		t.Fatal("decision regressed", got)
	}
	e.ApprovalRequestId = pointer("second")
	e.ApprovalReview.ItemId = pointer("second-item")
	e.ApprovalReview.Status = pointer("failed")
	applyEnvelope(v, e)
	e.ApprovalRequestId = pointer("third")
	e.ApprovalReview.Status = pointer("timed_out")
	applyEnvelope(v, e)
	if len(s.Snapshot().Reviews) != 3 {
		t.Fatal("provider call ID merged distinct invocations")
	}
	encoded, _ := json.Marshal(v)
	var restored view
	_ = json.Unmarshal(encoded, &restored)
	if len(restored.Reviews) != 1 || len(restored.LiveReviews) != 0 {
		t.Fatal("transient failure became durable decision")
	}
	e.SessionId = pointer("foreign")
	applyEnvelope(v, e)
	if len(s.Snapshot().Reviews) != 3 {
		t.Fatal("foreign review accepted")
	}
}

func TestNewProfileAndNaturalHandoffPinReviewerWithoutRelaxingSandbox(t *testing.T) {
	s := New(Options{Directory: t.TempDir()})
	profile := wire.ApplicationProfile{Model: "initial-model", Permissions: &wire.ApplicationPermissions{Mode: pointer("read-only"), ApprovalMode: pointer("manual")}}
	s.configureReviewer(&profile)
	if profile.Reviewer.Model != "initial-model" || value(profile.Permissions.Mode) != "read-only" || value(profile.Permissions.ApprovalMode) != "auto-review" {
		t.Fatal("wrong creation assembly")
	}
	profile.Model = "new-main-model"
	s.configureReviewer(&profile)
	if profile.Reviewer.Model != "initial-model" {
		t.Fatal("hot model selection replaced pinned reviewer")
	}
	s.requireApproval = true
	manual := wire.ApplicationProfile{Model: "test-model", Permissions: &wire.ApplicationPermissions{ApprovalMode: pointer("manual")}}
	s.configureReviewer(&manual)
	if manual.Reviewer != nil || value(manual.Permissions.ApprovalMode) != "manual" {
		t.Fatal("explicit manual fixture changed")
	}
}
