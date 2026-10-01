package app

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
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

func TestBootstrapRetirementPreservesFacadeAndRejectsOriginalRestart(t *testing.T) {
	e := newTestEngine()
	a, _ := fixtureApp(t, e, Host{})
	facade := a.Backend
	if err := a.PreparePersonal(); err != nil {
		t.Fatal(err)
	}
	if err := a.PrepareUpdate(); err != nil {
		t.Fatal(err)
	}
	if err := a.retireRoamingSource(t.Context()); err != nil {
		t.Fatal(err)
	}
	if a.Backend != facade || a.closed || !a.sourceRetired || a.personal != nil || a.notebook != nil {
		t.Fatal("source retirement destroyed stable facade or retained writers")
	}
	if err := a.Start(); err == nil {
		t.Fatal("retired native source restarted")
	}
	if _, err := a.Backend.Submit(t.Context(), api.Submission{ID: "after-source-retirement"}); err == nil {
		t.Fatal("retired facade admission reopened before observer swap")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if !a.closed {
		t.Fatal("final application close was consumed during retirement")
	}
}

func TestBootstrapPreflightRefusalPreservesOriginalLocalLifetime(t *testing.T) {
	for _, node := range []string{"", "actual-node"} {
		e := newTestEngine()
		a, _ := fixtureApp(t, e, Host{})
		if _, _, _, err := a.PrepareRoamingBootstrap(t.Context(), node); !errors.Is(err, ErrNodeRoamingPreflight) {
			t.Fatal("no-effect refusal became unknown", err)
		}
		if a.closed || a.sourceRetired || e.closed != 0 {
			t.Fatal("preflight refusal retired original local runtime")
		}
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
