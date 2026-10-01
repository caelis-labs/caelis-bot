package notebooksync

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func fixture(t *testing.T, fail string) (*Controller, *[]string) {
	t.Helper()
	trace := []string{}
	call := func(name string) error {
		trace = append(trace, name)
		if name == fail {
			return errors.New(name + " unavailable")
		}
		return nil
	}
	h := Hooks{
		SourceActive: func(context.Context) error { return call("active") }, StandbyStopped: func(context.Context, string) error { return call("standby") },
		StopSource: func(context.Context) error { return call("stop") }, SourceStopped: func(context.Context) error { return call("stopped-proof") },
		StartFresh: func(context.Context, string) error { return call("start-fresh") }, Transfer: func(_ context.Context, _ string, final bool) error {
			if final {
				return call("final-sync")
			}
			return call("periodic-sync")
		},
		Save: func(State) error { return nil },
	}
	c, err := New(State{SourceNodeID: "source", Targets: []Status{{NodeID: "backup"}}}, h)
	if err != nil {
		t.Fatal(err)
	}
	return c, &trace
}
func TestSwitchStopFinalSyncNewSessionOrder(t *testing.T) {
	c, trace := fixture(t, "")
	if err := c.Switch(t.Context(), "backup"); err != nil {
		t.Fatal(err)
	}
	want := []string{"active", "standby", "stop", "stopped-proof", "standby", "final-sync", "stopped-proof", "start-fresh"}
	if !reflect.DeepEqual(*trace, want) {
		t.Fatalf("order: %v", *trace)
	}
	if c.State().Targets[0].Phase != "switched" || c.State().Targets[0].LastSuccess == "" {
		t.Fatal("switch status missing")
	}
	if err := c.Sync(t.Context(), "backup"); err == nil {
		t.Fatal("retired source backed up after switch")
	}
}
func TestUncertainStopSyncOrStartNeverLaunchesAnotherBot(t *testing.T) {
	for _, fail := range []string{"stop", "stopped-proof", "final-sync", "start-fresh"} {
		t.Run(fail, func(t *testing.T) {
			c, trace := fixture(t, fail)
			if err := c.Switch(t.Context(), "backup"); err == nil {
				t.Fatal("failure hidden")
			}
			if fail != "start-fresh" {
				for _, v := range *trace {
					if v == "start-fresh" {
						t.Fatal("target started without safe final sync")
					}
				}
			}
			length := len(*trace)
			if err := c.Switch(t.Context(), "backup"); err == nil || len(*trace) != length {
				t.Fatal("uncertain action repeated")
			}
			restored, err := New(c.State(), c.hooks)
			if err != nil {
				t.Fatal(err)
			}
			if err = restored.Switch(t.Context(), "backup"); err == nil {
				t.Fatal("restart repeated uncertain switch")
			}
		})
	}
}
func TestSyncFailureKeepsLastSuccessAndReportsError(t *testing.T) {
	c, _ := fixture(t, "")
	if err := c.Sync(t.Context(), "backup"); err != nil {
		t.Fatal(err)
	}
	first := c.State().Targets[0].LastSuccess
	c.hooks.Transfer = func(context.Context, string, bool) error { return errors.New("rsync exit 23") }
	if err := c.Sync(t.Context(), "backup"); err == nil {
		t.Fatal("sync failure hidden")
	}
	s := c.State().Targets[0]
	if s.LastSuccess != first || s.Error != "rsync exit 23" {
		t.Fatalf("false freshness: %+v", s)
	}
}
func TestDurableIntentFailureStopsBeforeDispatch(t *testing.T) {
	c, trace := fixture(t, "")
	c.hooks.Save = func(State) error { return errors.New("disk unavailable") }
	if err := c.Switch(t.Context(), "backup"); err == nil {
		t.Fatal("durability error hidden")
	}
	if !reflect.DeepEqual(*trace, []string{"active", "standby"}) {
		t.Fatal("side effects before durable intent")
	}
}

func TestCancelledPeriodicWaitDoesNotBlockStoppedSourceCleanup(t *testing.T) {
	c, _ := fixture(t, "")
	if err := c.acquire(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer c.release()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan error, 1)
	go func() { done <- c.Sync(ctx, "backup") }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled timer blocked behind switch operation")
	}
}
