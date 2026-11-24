package bot

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func TestResidentIdleWaitsForMutationAndTimedWake(t *testing.T) {
	r, native := systemFixture(t)
	initial := make(chan struct{})
	sent := make(chan struct{}, 1)
	native.beforeReturn = func() { sent <- struct{}{} }
	var checks atomic.Int32
	r.now = func() time.Time {
		if checks.Add(1) == 1 {
			close(initial)
		}
		return time.Now()
	}
	r.Start(native)
	defer r.Close()
	select {
	case <-initial:
	case <-time.After(5 * time.Second):
		t.Fatal("resident did not start")
	}
	// Long enough for the former unconditional one-second tick to run.
	<-time.After(1100 * time.Millisecond)
	if n := checks.Load(); n != 1 {
		t.Fatalf("idle resident kept checking the clock: %d calls", n)
	}
	at := time.Now().Add(200 * time.Millisecond)
	if _, err := r.Upsert(Schedule{ID: "timed", Label: "Timed", Prompt: "Synthetic", At: at.Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sent:
		if time.Now().Before(at) {
			t.Fatal("resident woke before the deadline")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("idle resident missed the new schedule")
	}
	native.mu.Lock()
	defer native.mu.Unlock()
	if len(native.inputs) != 1 || native.inputs[0].SystemSource != api.SystemReminder {
		t.Fatalf("wrong scheduled native input: %+v", native.inputs)
	}
}
