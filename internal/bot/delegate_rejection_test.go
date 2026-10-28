package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type rejectionCompactTasks struct {
	compactTasks
	task api.Task
	err  error
}

func (f *rejectionCompactTasks) StartTask(_ context.Context, in api.TaskStart) (api.Task, error) {
	f.start = in
	f.starts++
	return f.task, f.err
}
func (f *rejectionCompactTasks) SendTask(_ context.Context, in api.TaskMessage) (api.Task, error) {
	f.sent = in
	f.sends++
	return f.task, f.err
}

func TestDelegateRejectionReasons(t *testing.T) {
	for _, reason := range []string{"updater_paused", "capacity_full", "work_admission_failed", "source_not_authorized", "workspace_invalid", "native_conflict", "native_rejected", ""} {
		for _, continuation := range []bool{false, true} {
			t.Run(reason+map[bool]string{false: "/create", true: "/continue"}[continuation], func(t *testing.T) {
				outcome := "rejected"
				wantCode := "submission_rejected"
				if reason == "native_conflict" {
					outcome = "conflicted"
					wantCode = "submission_conflicted"
				}
				// Display status may remain unknown while this invocation has a definite refusal.
				f := &rejectionCompactTasks{task: api.Task{ID: "original", Status: "unknown", Outcome: outcome}, err: &api.WorkMessageRejection{Code: reason, Cause: errors.New("native admission unavailable")}}
				out, invocation := invokeRejectionCompact(t, f, continuation)
				e := out.StructuredContent["error"].(map[string]string)
				data := out.StructuredContent["data"].(map[string]any)
				if !out.IsError || e["code"] != wantCode || e["reason"] != reason || e["detail"] != "native admission unavailable" || data["reason"] != reason || data["detail"] != e["detail"] {
					t.Fatalf("wrong rejection: %#v", out.StructuredContent)
				}
				if data["id"] != "original" || data["requestId"] != toolSubmissionID(invocation) || data["outcome"] != outcome || out.StructuredContent["outcome"] != outcome {
					t.Fatal("original receipt lost", data)
				}
				message := e["message"]
				temporary := reason == "updater_paused" || reason == "capacity_full"
				if temporary && (!strings.Contains(message, "Retry later") || !strings.Contains(message, "30 seconds") || strings.Contains(message, "definitively rejected")) {
					t.Fatal("temporary refusal guidance", message)
				}
				if strings.Contains(message, "directory") != (reason == "workspace_invalid") {
					t.Fatal("unrelated workspace correction", message)
				}
				if continuation {
					if f.sends != 1 || f.starts != 0 || f.sent.ID != "original" || f.sent.RequestID != toolSubmissionID(invocation) {
						t.Fatal("continuation changed or replayed", f)
					}
				} else if f.starts != 1 || f.sends != 0 || f.start.RequestID != toolSubmissionID(invocation) {
					t.Fatal("creation changed or replayed", f)
				}
			})
		}
	}
}

func invokeRejectionCompact(t *testing.T, f *rejectionCompactTasks, continuation bool) (api.ToolResult, api.ToolInvocation) {
	t.Helper()
	r, _, _ := fixture(t)
	if err := r.ConfigureTasks(f, nil); err != nil {
		t.Fatal(err)
	}
	invocation := api.ToolInvocation{Provider: "codex", Session: "resident", Turn: "turn", CallID: "refusal"}
	ctx := api.WithToolInvocation(t.Context(), invocation)
	raw := `{"title":"Review","prompt":"Original private intent"}`
	if continuation {
		raw = `{"task":"original","prompt":"Original private intent"}`
	}
	return r.CallTool(ctx, "bot_delegate", json.RawMessage(raw)), invocation
}

func TestDelegateRejectionUnknownReceiptRemainsUnconfirmed(t *testing.T) {
	for _, tc := range []struct {
		status, outcome string
		err             error
	}{
		{"unknown", "unknown", &api.WorkMessageRejection{Code: "capacity_full", Cause: errors.New("private cause")}},
		{"failed", "unknown", &api.WorkMessageRejection{Code: "workspace_invalid", Cause: errors.New("private cause")}},
		{"failed", "rejected", errors.New("untyped refusal")},
		{"failed", "conflicted", errors.New("untyped conflict")},
	} {
		f := &rejectionCompactTasks{task: api.Task{ID: "original", Status: tc.status, Outcome: tc.outcome}, err: tc.err}
		out, _ := invokeRejectionCompact(t, f, false)
		e := out.StructuredContent["error"].(map[string]string)
		data := out.StructuredContent["data"].(map[string]any)
		if !out.IsError || e["code"] != "unconfirmed" || e["reason"] != "" || e["detail"] != "" || data["id"] != "original" || data["outcome"] != tc.outcome {
			t.Fatal("uncertainty downgraded", out.StructuredContent)
		}
	}
}

func TestDelegateRejectionNilErrorNativeOutcomes(t *testing.T) {
	for _, outcome := range []string{"conflicted", "rejected"} {
		f := &rejectionCompactTasks{task: api.Task{ID: "original", Status: "failed", Outcome: outcome}}
		out, _ := invokeRejectionCompact(t, f, false)
		e := out.StructuredContent["error"].(map[string]string)
		code, reason := "submission_rejected", "native_rejected"
		if outcome == "conflicted" {
			code, reason = "submission_conflicted", "native_conflict"
		}
		if !out.IsError || e["code"] != code || e["reason"] != reason || out.StructuredContent["outcome"] != outcome {
			t.Fatal("native outcome collapsed", out.StructuredContent)
		}
	}
}

func TestDelegateRejectionSensitiveCauseAndUTF8(t *testing.T) {
	for _, tc := range []struct{ cause, want string }{
		{"native admission unavailable", "native admission unavailable"},
		{"native admission unavailable\nprivate conversation", "native admission unavailable"},
		{"native admission unavailable; body: full private prompt", "native admission unavailable; [private details omitted]"},
		{"native admission unavailable; prompt=private intent", "native admission unavailable; [private details omitted]"},
		{"native admission unavailable; payload: secret", "native admission unavailable; [private details omitted]"},
		{"native admission unavailable; Authorization: Bearer private-secret", "native admission unavailable; [private details omitted]"},
		{"native admission unavailable; password='private secret'", "native admission unavailable; [private details omitted]"},
		{"native admission unavailable; api_key=private-secret", "native admission unavailable; [private details omitted]"},
		{"native admission unavailable; token private-secret", "native admission unavailable; [private details omitted]"},
		{"native admission unavailable; response: full private content", "native admission unavailable; [private details omitted]"},
		{"native admission unavailable; {\"private\":\"content\"}", "native admission unavailable; [private details omitted]"},
		{"native admission unavailable\xff", "native admission unavailable"},
		{"native admission unavailable https://user:password@private.example/path?token=private-secret", "native admission unavailable [URL redacted]"},
		{"native admission unavailable sk-privateToken ghp_privateToken", "native admission unavailable [credential redacted] [credential redacted]"},
		{"native\x00 admission\u202e unavailable", "native admission unavailable"},
	} {
		f := &rejectionCompactTasks{task: api.Task{ID: "original", Status: "failed", Outcome: "rejected"}, err: &api.WorkMessageRejection{Code: "native_rejected", Cause: errors.New(tc.cause)}}
		out, _ := invokeRejectionCompact(t, f, false)
		e := out.StructuredContent["error"].(map[string]string)
		if e["detail"] != tc.want || out.StructuredContent["data"].(map[string]any)["detail"] != tc.want {
			t.Fatalf("unsafe or discarded cause: got %q want %q", e["detail"], tc.want)
		}
	}
	cause := "native admission unavailable " + strings.Repeat("界", 300) + " body: private content"
	detail := api.WorkRejectionDetail(errors.New(cause))
	if !utf8.ValidString(detail) || len(detail) > 512 || !strings.HasSuffix(detail, "…") || !strings.HasPrefix(detail, "native admission unavailable ") || strings.Contains(detail, "private content") {
		t.Fatal("unsafe UTF-8 truncation", detail)
	}
}

type privateDelegationCause struct{}

func (*privateDelegationCause) Error() string { return "native error -32602" }
func (*privateDelegationCause) WorkRejectionDetail() string {
	return "native admission unavailable; token=private-secret"
}

func TestDelegateUsesNativeCauseDiagnosticProjection(t *testing.T) {
	f := &rejectionCompactTasks{task: api.Task{ID: "original", Outcome: "rejected"}, err: &api.WorkMessageRejection{Code: "native_rejected", Cause: &privateDelegationCause{}}}
	out, _ := invokeRejectionCompact(t, f, false)
	e := out.StructuredContent["error"].(map[string]string)
	if e["reason"] != "native_rejected" || e["detail"] != "native admission unavailable; [private details omitted]" {
		t.Fatal(out.StructuredContent)
	}
}

func TestUnclassifiedAdmissionDoesNotRecommendTimedRetry(t *testing.T) {
	f := &rejectionCompactTasks{task: api.Task{Outcome: "rejected"}, err: &api.WorkMessageRejection{Code: "work_admission_failed", Cause: errors.New("unclassified admission")}}
	out, _ := invokeRejectionCompact(t, f, false)
	message := out.StructuredContent["error"].(map[string]string)["message"]
	if strings.Contains(message, "30 seconds") || strings.Contains(message, "temporarily") || strings.Contains(message, "Retry later") {
		t.Fatal("unclassified admission was asserted temporary", message)
	}
}
