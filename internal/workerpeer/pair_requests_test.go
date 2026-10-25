package workerpeer

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPairingRequestNeedsExplicitLocalDecisionAndSurvivesRestart(t *testing.T) {
	var opened atomic.Int32
	factory := func(context.Context, string, string, string) (Owner, string, *api.WorkExecutionSettings, error) {
		opened.Add(1)
		return newFixtureOwner(), "codex", nil, nil
	}
	s, err := Open(t.TempDir(), factory)
	if err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(s)
	defer h.Close()
	id, proof := rand.Text(), rand.Text()+rand.Text()
	first, err := RequestPairing(t.Context(), h.URL, id, "source-a", "Machine A", proof, s.Status().Peer)
	if err != nil || first.Status != "pending" {
		t.Fatal(first, err)
	}
	if opened.Load() != 0 || len(s.Status().Sources) != 0 || len(s.Status().Requests) != 1 {
		t.Fatal("request opened native work or granted access")
	}
	view := s.Status().Requests[0]
	if view.Name != "Machine A" || len(view.Fingerprint) != 64 || view.Address == "" {
		t.Fatal("missing safe identifying metadata", view)
	}
	c := Connection{Address: h.URL, Peer: first.Peer, Token: proof}
	if _, err = Call(t.Context(), c, Request{Action: "info"}); err == nil {
		t.Fatal("pending request became authorization")
	}
	same, err := RequestPairing(t.Context(), h.URL, id, "source-a", "Machine A", proof, first.Peer)
	if err != nil || same.Status != "pending" || s.Status().Requests[0].ExpiresAt != view.ExpiresAt {
		t.Fatal("retry extended or replaced original request", same, err)
	}
	if _, err = RequestPairing(t.Context(), h.URL, id, "source-a", "Changed", proof, first.Peer); err == nil {
		t.Fatal("same ID changed intent")
	}
	if _, err = RequestPairing(t.Context(), h.URL, id, "source-b", "Machine A", proof, first.Peer); err == nil {
		t.Fatal("request crossed sources")
	}
	unknown, err := ReadPairing(t.Context(), h.URL, rand.Text(), "source-a", proof)
	if err != nil || unknown.Status != "unknown" || len(s.Status().Requests) != 1 {
		t.Fatal("read created request", unknown, err)
	}
	bytes, err := os.ReadFile(filepath.Join(s.root, "peers.json"))
	if err != nil || strings.Contains(string(bytes), proof) {
		t.Fatal("registry stored raw bearer proof", err)
	}
	restarted, err := Open(s.root, factory)
	if err != nil {
		t.Fatal(err)
	}
	h.Close()
	h = httptest.NewServer(restarted)
	defer h.Close()
	c.Address = h.URL
	if err = restarted.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: id, Allow: true}); err != nil {
		t.Fatal(err)
	}
	if opened.Load() != 0 {
		t.Fatal("local approval created an empty execution owner")
	}
	receipt, err := ReadPairing(t.Context(), h.URL, id, "source-a", proof)
	if err != nil || receipt.Status != "approved" {
		t.Fatal(receipt, err)
	}
	if _, err = Call(t.Context(), c, Request{Action: "info"}); err != nil {
		t.Fatal("approved source cannot connect", err)
	}
	if err = restarted.Revoke("source-a"); err != nil {
		t.Fatal(err)
	}
	if err = restarted.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: id, Allow: true}); err != nil {
		t.Fatal(err)
	}
	receipt, err = ReadPairing(t.Context(), h.URL, id, "source-a", proof)
	if err != nil || receipt.Status != "revoked" {
		t.Fatal("duplicate decision reactivated revoked grant", receipt, err)
	}
}
func TestPairingClosedIntentCannotReviveAtCapacityAndExpiryDoesNotApprove(t *testing.T) {
	s, h, _ := peerFixture(t)
	proof := rand.Text() + rand.Text()
	id := rand.Text()
	if _, err := RequestPairing(t.Context(), h.URL, id, "source", "Fixture", proof, s.Status().Peer); err != nil {
		t.Fatal(err)
	}
	if err := s.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: id, Allow: false}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	for i := 0; i < 127; i++ {
		s.state.Requests[fmt.Sprint("closed-", i)] = pairingRequest{Source: fmt.Sprint("source-", i), Hash: tokenHash(proof), Name: "Fixture", ExpiresAt: 1, Status: "rejected"}
	}
	s.mu.Unlock()
	if _, err := RequestPairing(t.Context(), h.URL, "new", "new-source", "Fixture", proof, s.Status().Peer); err == nil {
		t.Fatal("capacity not bounded")
	}
	r, err := RequestPairing(t.Context(), h.URL, id, "source", "Fixture", proof, s.Status().Peer)
	if err != nil || r.Status != "rejected" {
		t.Fatal("closed ID resurrected", r, err)
	}
	if _, err = RequestPairing(t.Context(), h.URL, id, "source", "changed", proof, s.Status().Peer); err == nil {
		t.Fatal("closed ID forgot digest")
	}
	s.mu.Lock()
	v := s.state.Requests[id]
	v.Status = "pending"
	v.ExpiresAt = time.Now().Add(-time.Second).Unix()
	s.state.Requests[id] = v
	s.mu.Unlock()
	if err = s.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: id, Allow: true}); err == nil {
		t.Fatal("expired request approved")
	}
	if len(s.Status().Sources) != 0 || len(s.Status().Requests) != 0 {
		t.Fatal("expiry granted or displayed live request")
	}
	r, err = ReadPairing(t.Context(), h.URL, id, "source", proof)
	if err != nil || r.Status != "expired" {
		t.Fatal(r, err)
	}
}
func TestPairingApprovalFailureIsLocalAndDoesNotCreateGrant(t *testing.T) {
	s, h, _ := peerFixture(t)
	s.ConfigureReadiness(func(ctx context.Context) error {
		return &api.SetupBlockedError{Code: "worker_effort_unsupported", Message: "private path must not escape"}
	})
	r, err := RequestPairing(t.Context(), h.URL, "intent", "source", "Fixture", rand.Text()+rand.Text(), s.Status().Peer)
	if err != nil || r.Status != "pending" {
		t.Fatal(r, err)
	}
	err = s.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: "intent", Allow: true})
	if api.WorkerReadinessCode(err) != "worker_effort_unsupported" || len(s.Status().Sources) != 0 {
		t.Fatal("failed readiness granted source", err)
	}
	if err = s.DecidePairing(t.Context(), api.WorkerPairingDecision{ID: "intent", Allow: false}); err != nil {
		t.Fatal("reject depended on runtime readiness", err)
	}
}
func TestPairingClientOnlyCompleteExpectedRefusalIsTerminal(t *testing.T) {
	for _, code := range []int{400, 409, 500, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "pairing_request_conflict", code) }))
			defer h.Close()
			_, err := RequestPairing(t.Context(), h.URL, "intent", "source", "Fixture", rand.Text()+rand.Text(), "fixture-target")
			var rejected *api.ActionRejectedError
			if errors.As(err, &rejected) != (code < 500) {
				t.Fatal("ambiguous status became rejection", code, err)
			}
		})
	}
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(409)
		_, _ = w.Write([]byte("pairing_request_conflict"))
	}))
	defer h.Close()
	if _, err := RequestPairing(t.Context(), h.URL, "intent", "source", "Fixture", rand.Text()+rand.Text(), "fixture-target"); err == nil {
		t.Fatal("truncated response accepted")
	} else {
		var rejected *api.ActionRejectedError
		if errors.As(err, &rejected) {
			t.Fatal("truncated body became rejection")
		}
	}
}
func TestPairingBrowserOriginCannotRequestOrApprove(t *testing.T) {
	s, _, _ := peerFixture(t)
	body, _ := json.Marshal(PairingRequest{Contract: api.WorkerContract, ID: "intent", Source: "source", Name: "Fixture"})
	r := httptest.NewRequest("POST", Prefix+"pair", strings.NewReader(string(body)))
	r.Header.Set("Origin", "http://localhost")
	r.Header.Set("Authorization", "Bearer "+rand.Text()+rand.Text())
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 || len(s.Status().Requests) != 0 {
		t.Fatal("browser origin became pairing", w.Code)
	}
}
