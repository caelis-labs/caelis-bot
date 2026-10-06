package lockwait

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCancellationNeverLeavesAnOrphanLockOwner(t *testing.T) {
	var mu sync.Mutex
	mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if Lock(ctx, &mu) == nil {
		t.Fatal("acquired blocked control lock")
	}
	mu.Unlock()
	if !mu.TryLock() {
		t.Fatal("cancelled waiter retained lock")
	}
	mu.Unlock()
}
