// Package sharedruntime coordinates bounded recovery of shared services. Starting
// a service never grants the caller ownership of its process or native work.
package sharedruntime

import (
	"context"
	"sync"
	"time"
)

const RetryInterval = 30 * time.Second

type attempt struct {
	done chan struct{}
	err  error
	next time.Time
}

type Coordinator struct {
	mu       sync.Mutex
	attempts map[string]*attempt
}

// Recover merges observers of the same service and caches the result through
// the cooldown, including successful starts whose handshake is not ready yet.
// Each caller still reconnects and reconciles its own original receipts.
func (c *Coordinator) Recover(ctx context.Context, key string, start func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if c.attempts == nil {
		c.attempts = map[string]*attempt{}
	}
	if previous := c.attempts[key]; previous != nil {
		select {
		case <-previous.done:
			if time.Now().Before(previous.next) {
				c.mu.Unlock()
				return previous.err
			}
		default:
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-previous.done:
				return previous.err
			}
		}
	}
	a := &attempt{done: make(chan struct{})}
	c.attempts[key] = a
	c.mu.Unlock()
	err := start(ctx)
	c.mu.Lock()
	a.err, a.next = err, time.Now().Add(RetryInterval)
	close(a.done)
	c.mu.Unlock()
	return err
}

var Shared Coordinator
