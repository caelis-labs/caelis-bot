package eventplan

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/localstate"
)

func TestExpiredWakeBudgetWritesOnceAndIdleDoesNoWork(t *testing.T) {
	now := time.Now()
	path := filepath.Join(t.TempDir(), "events.json")
	before := now.Add(-24*time.Hour + time.Second)
	saved := State{
		Version: 1, Policy: DefaultPolicy(),
		WakeAttempts: []time.Time{before}, WakeReservations: []time.Time{before},
	}
	if err := localstate.Write(path, saved); err != nil {
		t.Fatal(err)
	}
	e, err := Open(path, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	writes := 0
	e.write = func(path string, state any) error {
		writes++
		return localstate.Write(path, state)
	}
	d := Delivery{
		Ready: func() bool { t.Fatal("idle tick queried resident admission"); return false },
	}
	expiry := now.Add(time.Second)
	if next := e.NextCheck(now); !next.Equal(expiry) {
		t.Fatalf("cleanup deadline = %v, want %v", next, expiry)
	}
	if err := e.Tick(t.Context(), expiry, d); err != nil {
		t.Fatal(err)
	}
	if writes != 1 || len(e.state.WakeAttempts) != 0 || len(e.state.WakeReservations) != 0 {
		t.Fatalf("cleanup did not commit exactly once: writes=%d", writes)
	}
	idle := expiry.Add(time.Second)
	if allocs := testing.AllocsPerRun(60, func() {
		if err := e.Tick(t.Context(), idle, d); err != nil {
			t.Fatal(err)
		}
		if next := e.NextCheck(idle); !next.IsZero() {
			t.Fatalf("idle engine armed another check: %v", next)
		}
	}); allocs != 0 || writes != 1 {
		t.Fatalf("idle work: allocs=%g writes=%d", allocs, writes-1)
	}
}
