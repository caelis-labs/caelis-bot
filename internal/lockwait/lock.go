// Package lockwait bounds admission waiting without leaving orphan goroutines
// that might acquire a mutation lock after its caller has timed out.
package lockwait

import (
	"context"
	"sync"
	"time"
)

func Lock(ctx context.Context, mu *sync.Mutex) error    { return wait(ctx, mu.TryLock) }
func RLock(ctx context.Context, mu *sync.RWMutex) error { return wait(ctx, mu.TryRLock) }
func wait(ctx context.Context, acquire func() bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if acquire() {
			return nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
