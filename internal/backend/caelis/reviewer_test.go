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
	for _, requiredCapability := range []string{"session-guardian-review-v1", "runtime-agent-team-v1"} {
		t.Run(requiredCapability, func(t *testing.T) {
			s := fixtureSession(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/initialize") {
					t.Fatal("unexpected mutation")
				}
				caps := slices.DeleteFunc(slices.Clone(required), func(c string) bool { return c == requiredCapability })
				writeFixture(w, wire.ServerInfo{ProtocolVersion: 1, ApiVersion: "v1", EnvelopeVersion: "caelis.control.envelope/v1", StoreId: pointer("store"), InstanceId: pointer("instance"), Capabilities: caps})
			})
			if _, err := initialize(t.Context(), s.client); !errors.Is(err, errBotIncompatible) {
				t.Fatal("old Host accepted", err)
			}
		})
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

func TestReviewerReadinessUsesRuntimeTeamAndPreservesManualPolicy(t *testing.T) {
	for _, status := range []string{"ready", "unavailable", "manual", "mismatch"} {
		t.Run(status, func(t *testing.T) {
			reviewer := &wire.SessionReviewer{Kind: "guardian", Model: "explicit/model"}
			b := wire.SessionBinding{SessionId: "main", Profile: wire.SessionProfile{Permissions: &wire.SessionPermissions{ApprovalMode: pointer("auto-review")}, Reviewer: reviewer}}
			response := wire.SessionReviewerState{SessionId: "main", ApprovalMode: "auto-review", Reviewer: &wire.SessionReviewer{Kind: "guardian", Model: "team/luna"}, Status: status}
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
			before, _ := json.Marshal(b)
			err := s.checkReviewer(t.Context(), b)
			after, _ := json.Marshal(b)
			if string(before) != string(after) {
				t.Fatal("review readiness rewrote original binding")
			}
			if (err == nil) != (status == "ready" || status == "manual") {
				t.Fatal(status, err)
			}
		})
	}
}

func TestReviewFactsPreserveIdentityAndNeverGrantManualAuthority(t *testing.T) {
	s := fixtureSession(t, func(http.ResponseWriter, *http.Request) { t.Fatal("review fact dispatched mutation") })
	s.state.Session.Profile.Permissions = &wire.SessionPermissions{ApprovalMode: pointer("auto-review")}
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

func TestNewProfileAndHandoffUseRuntimeTeamWithoutRelaxingSandbox(t *testing.T) {
	s := New(Options{Directory: t.TempDir()})
	profile := wire.SessionProfile{Model: "initial-model", Permissions: &wire.SessionPermissions{Mode: pointer("read-only"), ApprovalMode: pointer("manual")}}
	s.configureApproval(&profile)
	if profile.Reviewer != nil || value(profile.Permissions.Mode) != "read-only" || value(profile.Permissions.ApprovalMode) != "auto-review" {
		t.Fatal("wrong creation assembly")
	}
	profile.Model = "new-main-model"
	profile.Reviewer = &wire.SessionReviewer{Kind: "guardian", Model: "legacy-mimo"}
	s.configureApproval(&profile)
	if profile.Reviewer != nil {
		t.Fatal("new profile retained an independent reviewer")
	}
	s.requireApproval = true
	manual := wire.SessionProfile{Model: "test-model", Permissions: &wire.SessionPermissions{ApprovalMode: pointer("manual")}}
	s.configureApproval(&manual)
	if manual.Reviewer != nil || value(manual.Permissions.ApprovalMode) != "manual" {
		t.Fatal("explicit manual fixture changed")
	}
}
