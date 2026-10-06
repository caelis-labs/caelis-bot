package caelis

import (
	"errors"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestRemoteIngressFenceRefusesChangedNativeGeneration(t *testing.T) {
	s := New(Options{Directory: t.TempDir()})
	in := api.Submission{ID: "telegram:123:1", Text: "queued", NativeIngressFence: s.RecoveryState().Fence}
	s.mu.Lock()
	s.recoveryGeneration++
	s.mu.Unlock()
	if _, err := s.Submit(t.Context(), in, nil); !errors.Is(err, api.ErrRecoveryPending) {
		t.Fatal("stale Caelis connection admitted queued input", err)
	}
	if len(s.state.Operations) != 0 {
		t.Fatal("pre-dispatch refusal wrote a native operation")
	}
}
