package caelis

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestIdleStreamFailureWakesOriginalOwnerForReplacement(t *testing.T) {
	settings := setupFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("observer replayed a mutation")
		}
		http.Error(w, "old listener draining", http.StatusServiceUnavailable)
	})
	d, token, err := Discover(settings)
	if err != nil {
		t.Fatal(err)
	}
	c, err := newClient(d.Endpoint, token)
	if err != nil {
		t.Fatal(err)
	}
	s := New(Options{Directory: t.TempDir(), Settings: settings})
	s.ctx, s.cancel = context.WithCancel(t.Context())
	defer s.Close(context.Background())
	s.connected = true
	s.client = c
	s.state.InstanceID = "old-instance"
	s.state.Session = wire.SessionBinding{SessionId: "original-session"}
	s.mu.Lock()
	s.ensureStreamLocked("original-session")
	s.mu.Unlock()
	select {
	case <-s.wake:
	case <-time.After(3 * time.Second):
		t.Fatal("idle observer did not wake recovery")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connected || !s.needsRefreshLocked() || s.state.InstanceID != "old-instance" || s.state.Session.SessionId != "original-session" {
		t.Fatal("observer accepted new authority instead of handing off authenticated recovery")
	}
}
