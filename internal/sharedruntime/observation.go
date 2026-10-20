package sharedruntime

import "time"

// ObservationBudget limits an unresolved observation episode, not a Runtime's
// lifetime. It is transient and grants no authority to retry a mutation.
// Owners reset it on fresh native progress or an explicit user operation.
type ObservationBudget struct {
	attempts int
	next     time.Time
}

func (b *ObservationBudget) Take(now time.Time) bool {
	if b.attempts >= 3 || now.Before(b.next) {
		return false
	}
	b.next = now.Add(time.Second * time.Duration(2<<b.attempts))
	b.attempts++
	return true
}

func (b *ObservationBudget) Reset() { *b = ObservationBudget{} }
