package workerpeer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

type refusingPeerOwner struct {
	*fixtureOwner
	code  string
	cause error
	start api.WorkStart
	sent  api.TaskMessage
}

func (f *refusingPeerOwner) refusal(id string) (api.Task, error) {
	outcome := "rejected"
	if f.code == "native_conflict" {
		outcome = "conflicted"
	}
	cause := f.cause
	if cause == nil {
		cause = errors.New("native admission unavailable")
	}
	return api.Task{ID: id, Status: "failed", Outcome: outcome}, fmt.Errorf("native boundary: %w", &api.WorkMessageRejection{Code: f.code, Cause: cause})
}
func (f *refusingPeerOwner) StartWork(_ context.Context, in api.WorkStart) (api.Task, error) {
	f.start = in
	f.starts++
	return f.refusal(in.ID)
}
func (f *refusingPeerOwner) SendWork(_ context.Context, in api.TaskMessage) (api.Task, error) {
	f.sent = in
	f.sends++
	return f.refusal(in.ID)
}
func TestPeerStartSendRejectionRoundTrip(t *testing.T) {
	for _, code := range []string{"updater_paused", "capacity_full", "work_admission_failed", "workspace_invalid", "native_conflict", "native_rejected", ""} {
		t.Run(code, func(t *testing.T) {
			owner := &refusingPeerOwner{fixtureOwner: newFixtureOwner(), code: code}
			s, err := Open(t.TempDir(), func(context.Context, string, string, string) (Owner, string, *api.WorkExecutionSettings, error) {
				return owner, "codex", nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(context.Background())
			h := httptest.NewServer(s)
			defer h.Close()
			c := pairFixture(t, s, h, "source-a")
			for _, action := range []string{"start", "send"} {
				in := Request{Action: action, Start: api.WorkStart{ID: "original-task", TaskStart: api.TaskStart{RequestID: "original-start", Prompt: "Original intent"}}, Message: api.TaskMessage{ID: "original-task", RequestID: "original-send", Prompt: "Original continuation"}}
				out, err := Call(t.Context(), c, in)
				var rejected *api.WorkMessageRejection
				if !errors.As(err, &rejected) || rejected.Code != code || rejected.Cause.Error() != "native boundary: native admission unavailable" || !out.Rejected || out.RejectionCode != code || out.Task.ID != "original-task" {
					t.Fatalf("%s rejection lost: response=%#v err=%v", action, out, err)
				}
				outcome := "rejected"
				if code == "native_conflict" {
					outcome = "conflicted"
				}
				if out.Task.Outcome != outcome {
					t.Fatal("native outcome changed", out.Task)
				}
			}
			if owner.starts != 1 || owner.sends != 1 || owner.start.RequestID != "original-start" || owner.sent.RequestID != "original-send" || owner.sent.ID != "original-task" {
				t.Fatal("original calls changed or replayed", owner)
			}
		})
	}
}

func TestPeerRejectionCompatibilityAndIdentity(t *testing.T) {
	for _, action := range []string{"start", "send"} {
		for _, tc := range []struct {
			name       string
			rejected   bool
			contract   int
			peer, code string
			typed      bool
			cause      string
		}{
			{"old-no-code", true, api.WorkerContract, "original-peer", "", true, "native refused"},
			{"unknown-effect", false, api.WorkerContract, "original-peer", "native_rejected", false, "native refused"},
			{"peer-mismatch", true, api.WorkerContract, "replacement-peer", "native_rejected", false, "bot_peer_identity_changed"},
			{"contract-mismatch", true, api.WorkerContract + 1, "original-peer", "native_rejected", false, "bot_worker_contract_incompatible"},
		} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				raw, err := json.Marshal(Response{Contract: tc.contract, Peer: tc.peer, Rejected: tc.rejected, RejectionCode: tc.code, Error: "native refused", Task: api.Task{ID: "original-task", Outcome: "unknown"}})
				if err != nil {
					t.Fatal(err)
				}
				if tc.code == "" && bytes.Contains(raw, []byte("rejectionCode")) {
					t.Fatal("empty additive field was emitted")
				}
				h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(raw) }))
				defer h.Close()
				out, err := Call(t.Context(), Connection{Address: h.URL, Peer: "original-peer", Token: "fixture-proof"}, Request{Action: action})
				var rejected *api.WorkMessageRejection
				if errors.As(err, &rejected) != tc.typed || err == nil || err.Error() != tc.cause || out.Task.ID != "original-task" || out.Task.Outcome != "unknown" {
					t.Fatalf("identity/compatibility changed: out=%#v err=%v", out, err)
				}
				if tc.typed && (rejected.Code != "" || rejected.Cause.Error() != tc.cause) {
					t.Fatal("old peer acquired a reason", rejected)
				}
			})
		}
	}
}

// The native error deliberately keeps private detail out of Error().
type privateNativeRefusal struct{}

func (*privateNativeRefusal) Error() string { return "native error -32602" }
func (*privateNativeRefusal) WorkRejectionDetail() string {
	return "native admission unavailable; token=private-secret"
}

func TestPeerRefusalPreservesPrivateDiagnosticProjection(t *testing.T) {
	owner := &refusingPeerOwner{fixtureOwner: newFixtureOwner(), code: "native_rejected", cause: &privateNativeRefusal{}}
	s, err := Open(t.TempDir(), func(context.Context, string, string, string) (Owner, string, *api.WorkExecutionSettings, error) {
		return owner, "codex", nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	h := httptest.NewServer(s)
	defer h.Close()
	c := pairFixture(t, s, h, "source-a")
	out, err := Call(t.Context(), c, Request{Action: "start", Start: api.WorkStart{ID: "original-task", TaskStart: api.TaskStart{RequestID: "original-start", Prompt: "Original intent"}}})
	var refusal *api.WorkMessageRejection
	want := "native admission unavailable; [private details omitted]"
	if !errors.As(err, &refusal) || refusal.Code != "native_rejected" || out.RejectionDetail != want || api.WorkRejectionDetail(refusal.Cause) != want || err.Error() != "native boundary: native error -32602" {
		t.Fatalf("lost safe original detail: response=%#v err=%v", out, err)
	}
}
