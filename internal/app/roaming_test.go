package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

type fenceEngineFixture struct {
	*testEngine
	fences   atomic.Int32
	fenceErr error
}

func (f *fenceEngineFixture) FenceStop(context.Context) error { f.fences.Add(1); return f.fenceErr }
func TestManagedHardFenceIsIdempotentAndConfirmsOnlySuccessfulExit(t *testing.T) {
	for _, failing := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "unknown"}[failing], func(t *testing.T) {
			engine := &fenceEngineFixture{testEngine: newTestEngine()}
			if failing {
				engine.fenceErr = errors.New("native exit unconfirmed")
			}
			owner := &managedNodeOwner{app: &Application{engine: engine}, nativeFenced: make(chan struct{}), nativeFenceDone: make(chan struct{})}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					err := owner.hardFence(t.Context())
					if failing != (err != nil) {
						t.Error("wrong native stop proof", err)
					}
				})
			}
			wg.Wait()
			if engine.fences.Load() != 1 {
				t.Fatal("duplicate native termination", engine.fences.Load())
			}
			select {
			case <-owner.nativeFenceDone:
			default:
				t.Fatal("native completion not delivered")
			}
			select {
			case <-owner.nativeFenced:
				if failing {
					t.Fatal("failed native stop reported confirmed")
				}
			default:
				if !failing {
					t.Fatal("successful native stop lacked confirmation")
				}
			}
		})
	}
}
