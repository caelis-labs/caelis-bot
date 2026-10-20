package sharedruntime

import (
	"testing"
	"time"
)

func TestObservationBudgetIsFiniteAndExplicitlyReset(t *testing.T) {
	var b ObservationBudget
	now := time.Unix(0, 0)
	calls := 0
	for second := range 100 {
		if b.Take(now.Add(time.Duration(second) * time.Second)) {
			calls++
		}
	}
	if calls != 3 {
		t.Fatal("unbounded observation", calls)
	}
	b.Reset()
	if !b.Take(now.Add(time.Hour)) {
		t.Fatal("explicit retry unavailable")
	}
	if b.Take(now.Add(time.Hour)) {
		t.Fatal("backoff ignored")
	}
}
