package plugins

import (
	"context"
	"encoding/json"
	"fmt"

	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventCollectorCallsOnceAndPinsConnection(t *testing.T) {
	var calls atomic.Int32
	m := configuredProbeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		method, id := probeMCPMethod(t, w, r)
		if method == "tools/call" {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"structuredContent":{"unreadCount":23}}}`, id)
		}
	})
	binding := m.EventBindings()["fixture/docs"]
	if binding == "" {
		t.Fatal("missing binding")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := m.CollectEvent(ctx, "fixture/docs", binding, "count", map[string]any{})
	if err != nil || string(result) != `{"unreadCount":23}` || calls.Load() != 1 {
		t.Fatal(string(result), err, calls.Load())
	}
	if _, err = m.CollectEvent(ctx, "fixture/docs", "old-binding", "count", nil); err == nil || calls.Load() != 1 {
		t.Fatal("changed binding executed")
	}
	if _, err = m.Mutate(t.Context(), "fixture", "disable", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = m.CollectEvent(ctx, "fixture/docs", binding, "count", nil); err == nil || calls.Load() != 1 {
		t.Fatal("disabled connection executed")
	}
}
func TestEventCollectorRejectsErrorAndNonJSONWithoutReplay(t *testing.T) {
	for _, result := range []string{`{"isError":true,"content":[{"type":"text","text":"private diagnostic"}]}`, `{"content":[{"type":"text","text":"not JSON"}]}`} {
		t.Run(result, func(t *testing.T) {
			var calls atomic.Int32
			m := configuredProbeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				method, id := probeMCPMethod(t, w, r)
				if method == "tools/call" {
					calls.Add(1)
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": json.RawMessage(result)})
				}
			})
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if _, err := m.CollectEvent(ctx, "fixture/docs", m.EventBindings()["fixture/docs"], "read", nil); err == nil || calls.Load() != 1 {
				t.Fatal("invalid output accepted or retried", err, calls.Load())
			}
		})
	}
}

func TestLostToolResponseIsUnconfirmed(t *testing.T) {
	var calls atomic.Int32
	m := configuredProbeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		method, _ := probeMCPMethod(t, w, r)
		if method == "tools/call" {
			calls.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: error\ndata: {}\n\n")
		}
	})
	_, err := m.CollectEvent(t.Context(), "fixture/docs", m.EventBindings()["fixture/docs"], "read", nil)
	var confirmed interface{ ConfirmedFailure() bool }
	if err == nil || errors.As(err, &confirmed) && confirmed.ConfirmedFailure() || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
}
