package codex

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRemoteIngressFenceRefusesBeforeNativeReceipt(t *testing.T) {
	s := NewSession(SessionOptions{StateFile: filepath.Join(t.TempDir(), "binding.json")})
	defer s.Close(t.Context())
	fence := s.RecoveryState().Fence
	s.mu.Lock()
	s.reconnectActive = true
	s.state.Connection = "ready" // cleanup reconciliation can expose ready first
	s.state.CanSend = true
	s.mu.Unlock()
	in := api.Submission{ID: "telegram:123:1", Text: "queued", NativeIngressFence: fence}
	if _, err := s.Submit(t.Context(), in, nil); !errors.Is(err, api.ErrRecoveryPending) {
		t.Fatal("ready snapshot admitted input before automatic recovery completed", err)
	}
	s.mu.Lock()
	s.reconnectActive = false
	s.epoch++
	s.mu.Unlock()
	if _, err := s.Submit(t.Context(), in, nil); !errors.Is(err, api.ErrRecoveryPending) {
		t.Fatal("stale native generation admitted queued input", err)
	}
	if s.binding.Pending != nil || s.state.LastReceipt.ID != "" {
		t.Fatal("pre-dispatch refusal wrote a native receipt")
	}
}
