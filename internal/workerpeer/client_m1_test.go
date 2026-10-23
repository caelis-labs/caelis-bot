package workerpeer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestConnectionErrorsUseOnlyFixedCodes(t *testing.T) {
	for _, tc := range []struct {
		body string
		code int
		want string
	}{
		{"pairing_code_expired", 403, "pairing_code_expired"},
		{"pairing_code_invalid", 403, "pairing_code_invalid"},
		{"original_peer_proof_required", 403, "original_peer_proof_required"},
		{"peer_authorization_revoked", 401, "peer_authorization_revoked"},
		{"worker_sharing_disabled", 403, "worker_sharing_disabled"},
		{"worker_not_ready: private-native-path-and-secret", 409, "worker_not_ready"},
		{"worker_not_ready: worker_contract_incompatible", 409, "bot_worker_contract_incompatible"},
		{"private-native-path-and-secret", 500, "bot_peer_unreachable"},
	} {
		t.Run(tc.want+tc.body[:3], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, tc.body, tc.code) }))
			defer server.Close()
			_, err := Pair(t.Context(), server.URL, "fixture-code", "fixture-source")
			if err == nil || err.Error() != tc.want || strings.Contains(err.Error(), "private-") {
				t.Fatal("pair classification", err)
			}
			_, err = Call(t.Context(), Connection{Address: server.URL, Token: "fixture-proof"}, Request{Action: "info"})
			if err == nil || err.Error() != tc.want {
				t.Fatal("authenticated check classification", err)
			}
		})
	}
	for _, body := range []string{"worker_not_ready: private-native-path-and-secret", "private-native-path-and-secret", "worker_contract_incompatible"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(Response{Contract: api.WorkerContract, Peer: "fixture-peer", Error: body})
		}))
		_, err := Call(t.Context(), Connection{Address: server.URL, Peer: "fixture-peer", Token: "fixture-proof"}, Request{Action: "info"})
		server.Close()
		if err == nil || strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), server.URL) {
			t.Fatal("untrusted response text escaped fixed classification", err)
		}
	}
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closed.Close()
	_, err := Call(context.Background(), Connection{Address: closed.URL, Token: "must-not-leak-proof"}, Request{Action: "info"})
	if err == nil || err.Error() != "bot_peer_unreachable" {
		t.Fatal("network failure leaked transport details", err)
	}
}

func TestConnectionClassificationPreservesNativeOperationReceipts(t *testing.T) {
	for _, tc := range []struct {
		action, peer string
		rejected     bool
	}{
		{"send", "original-peer", true},
		{"decision", "original-peer", true},
		{"reconcile", "original-peer", false},
		{"send", "different-peer", true},
	} {
		t.Run(tc.action+tc.peer, func(t *testing.T) {
			q := api.WorkOperationQuery{TaskID: "task-original", RequestID: "original-operation", Kind: "send", Fingerprint: "original-intent"}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(Response{Contract: api.WorkerContract, Peer: tc.peer, Error: "original operation intent does not match", Rejected: tc.rejected, Operation: api.WorkOperation{WorkOperationQuery: q, Outcome: "unknown"}})
			}))
			defer server.Close()
			out, err := Call(t.Context(), Connection{Address: server.URL, Peer: "original-peer", Token: "fixture-proof"}, Request{Action: tc.action, Query: q})
			if err == nil || out.Operation.WorkOperationQuery != q || out.Operation.Outcome != "unknown" {
				t.Fatal("original receipt identity or uncertainty changed", out.Operation, err)
			}
			var message *api.WorkMessageRejection
			var decision *api.DecisionValidationError
			if tc.peer != "original-peer" {
				if errors.As(err, &message) || errors.As(err, &decision) || err.Error() != "bot_peer_identity_changed" {
					t.Fatal("unverified peer supplied authoritative rejection", err)
				}
			} else if tc.action == "send" && !errors.As(err, &message) || tc.action == "decision" && !errors.As(err, &decision) {
				t.Fatal("native refusal lost typed semantics", err)
			} else if err.Error() != "original operation intent does not match" {
				t.Fatal("task rejection was changed into connection evidence", err)
			}
		})
	}
}
