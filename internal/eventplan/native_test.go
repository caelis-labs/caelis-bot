package eventplan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
)

func nativeArrangement(id string) NativeArrangement {
	return NativeArrangement{ID: id, Label: "Work break", Prompt: "Report the requested result", Sources: []string{"clock.minute"}, When: "true", TimeZone: "UTC"}
}

func assertRejectedUnchanged(t *testing.T, e *Engine, save func(func()) error) {
	t.Helper()
	before := e.Snapshot()
	raw, err := os.ReadFile(e.path)
	if err != nil {
		t.Fatal(err)
	}
	authorized := false
	if err := save(func() { authorized = true }); err == nil {
		t.Error("save accepted despite retained identity or capacity constraint")
	}
	after, err := os.ReadFile(e.path)
	if err != nil || authorized || !bytes.Equal(raw, after) || !reflect.DeepEqual(before, e.Snapshot()) {
		t.Error("rejected save changed authorization, memory or persisted state", err, authorized)
	}
}

func TestNativeRetainedEventIdentityAfterOfflineCleanup(t *testing.T) {
	e, now := setup(t, nil)
	n := nativeArrangement("work-break")
	p, err := e.SaveNative(n, *now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Remove(p.ID, *now); err != nil {
		t.Fatal(err)
	}
	tick(t, e, *now, noDelivery()) // Retire the cancelled Plan, then quit for eight days.
	if !e.Snapshot().Plans[0].Retired {
		t.Fatal("plan was not retired")
	}
	e.Close()
	*now = now.Add(8 * 24 * time.Hour)
	restored, err := Open(e.path, nil, *now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	s := restored.Snapshot()
	if len(s.Plans) != 0 || len(s.Events) != 2 || s.Events[1].Archived.IsZero() {
		t.Fatal("fixture did not reach retained Event / pruned Plan window", s)
	}
	assertRejectedUnchanged(t, restored, func(authorize func()) error {
		_, err := restored.SaveNative(n, *now, func(Plan) error { authorize(); return nil })
		return err
	})
	reopened, err := Open(e.path, nil, *now)
	if err != nil {
		t.Fatal("save poisoned the ledger on next restart:", err)
	}
	reopened.Close()
}

func TestNativeCapacityIncludesSharedEventPlansAndUnresolvedCancellation(t *testing.T) {
	for _, receipt := range []string{"shared-event", "accepted", "unknown"} {
		t.Run(receipt, func(t *testing.T) {
			e, now := setup(t, nil)
			count := MaxActive
			if receipt != "shared-event" {
				count--
			}
			for i := range count {
				if _, err := e.Save(Plan{ID: fmt.Sprintf("shared-%d", i), Event: "mail", Kind: "after", Seconds: 3600}, *now); err != nil {
					t.Fatal(err)
				}
			}
			if receipt != "shared-event" {
				p, err := e.SaveNative(nativeArrangement("unsettled"), *now, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err = e.ReceiveNative(t.Context(), []scheduledata.Event{{Source: "clock.minute", At: *now, Data: map[string]any{}}}, *now); err != nil {
					t.Fatal(err)
				}
				d := noDelivery()
				d.Ready = true
				d.Submit = func(context.Context, string, string, string) (string, error) { return receipt, nil }
				tick(t, e, *now, d)
				if err = e.Remove(p.ID, *now); err != nil {
					t.Fatal(err)
				}
				if !e.pending(p.ID) {
					t.Fatal("cancelled receipt was lost")
				}
				// An identical retry at capacity is a read of the same cancelled
				// arrangement, never a new authorization or a reopened schedule.
				retry, err := e.SaveNative(nativeArrangement("unsettled"), *now, func(Plan) error {
					t.Fatal("identical retry reauthorized")
					return nil
				})
				if err != nil || retry.ID != p.ID || retry.Status != "cancelled" {
					t.Fatal("identical retry lost original identity", retry, err)
				}
			}
			// Both APIs must use the same occupied-Plan count, before authorization.
			assertRejectedUnchanged(t, e, func(authorize func()) error {
				_, err := e.Save(Plan{ID: "extra-timed", Event: "mail", Kind: "after", Seconds: 3600}, *now, func() error { authorize(); return nil })
				return err
			})
			assertRejectedUnchanged(t, e, func(authorize func()) error {
				_, err := e.SaveNative(nativeArrangement("extra-native"), *now, func(Plan) error { authorize(); return nil })
				return err
			})
			restored, err := Open(e.path, nil, *now)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restored.Close)
			if !reflect.DeepEqual(e.Snapshot(), restored.Snapshot()) {
				t.Fatal("failed admission changed restart state")
			}
		})
	}
}

func TestNativeSaveFailurePreservesPersistedLedger(t *testing.T) {
	for _, failure := range []string{"authorization", "commit"} {
		t.Run(failure, func(t *testing.T) {
			e, now := setup(t, nil)
			before := e.Snapshot()
			raw, err := os.ReadFile(e.path)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "commit" {
				e.write = func(string, any) error { return errors.New("synthetic commit failure") }
			}
			_, err = e.SaveNative(nativeArrangement("failed"), *now, func(Plan) error {
				if failure == "authorization" {
					return errors.New("synthetic denied registration")
				}
				return nil
			})
			if err == nil {
				t.Fatal("failure hidden")
			}
			after, err := os.ReadFile(e.path)
			if err != nil || !bytes.Equal(raw, after) {
				t.Fatal("failed registration changed disk", err)
			}
			if failure == "authorization" && !reflect.DeepEqual(before, e.Snapshot()) {
				t.Fatal("denied registration changed memory")
			}
			if failure == "commit" && e.usable() == nil {
				t.Fatal("failed commit did not stop execution")
			}
			restored, err := Open(e.path, nil, *now)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restored.Close)
			if !reflect.DeepEqual(before, restored.Snapshot()) {
				t.Fatal("failed registration changed restart state")
			}
		})
	}
}

func TestNativeCapacityAlsoBoundsEventDefinitions(t *testing.T) {
	for _, archived := range []bool{false, true} {
		t.Run(fmt.Sprint("archived=", archived), func(t *testing.T) {
			e, now := setup(t, nil)
			limit := MaxActive
			if archived {
				limit += MaxHistory
				if err := e.ArchiveEvent("mail", *now); err != nil {
					t.Fatal(err)
				}
			}
			for i := 1; i < limit; i++ {
				v, err := e.Register(Event{ID: fmt.Sprintf("reserved-%d", i), Kind: "wake", Label: "Reserved definition", OutputSchema: json.RawMessage(`{"type":"object"}`)}, *now)
				if err != nil {
					t.Fatal(err)
				}
				if archived {
					if err = e.ArchiveEvent(v.ID, *now); err != nil {
						t.Fatal(err)
					}
				}
			}
			assertRejectedUnchanged(t, e, func(authorize func()) error {
				_, err := e.SaveNative(nativeArrangement("extra-native"), *now, func(Plan) error { authorize(); return nil })
				return err
			})
		})
	}
}
