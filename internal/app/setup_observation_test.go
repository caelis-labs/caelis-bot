package app

import (
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
	"time"
)

func TestSavedSetupOverviewDoesNotWaitForRuntimeInspection(t *testing.T) {
	a, _ := fixtureApp(t, newTestEngine(), Host{})
	s := a.setup
	s.mu.Lock()
	defer s.mu.Unlock()
	done := make(chan api.SetupOverview, 1)
	go func() { done <- s.Overview() }()
	select {
	case got := <-done:
		if got.Active == "" {
			t.Fatal("saved runtime identity missing")
		}
	case <-time.After(time.Second):
		t.Fatal("saved preferences blocked on Runtime inspection")
	}
}
