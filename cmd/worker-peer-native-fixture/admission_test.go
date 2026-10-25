//go:build workerpeere2e

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Unit coverage of the controlled resident, not a replacement product server.
// The application E2E separately consumes the actual App's scoped MCP endpoint.
func TestFixtureAdmissionQueriesOnlyOriginalReceipt(t *testing.T) {
	for _, mode := range []string{"accepted", "rejected", "unknown-cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, err := openNative(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			var replies, calls bytes.Buffer
			outcomes := []string{"unknown", "accepted"}
			if mode == "rejected" {
				outcomes = []string{"rejected"}
			}
			if mode == "unknown-cancelled" {
				outcomes = []string{"unknown"}
			}
			for n, outcome := range outcomes {
				id := "fixture-call-turn-original-admission-" + string(rune('0'+n))
				_ = json.NewEncoder(&replies).Encode(map[string]any{"id": id, "result": map[string]any{"structuredContent": map[string]any{"ok": true, "data": map[string]any{"outcome": outcome}}}})
			}
			c := &botMCP{encode: json.NewEncoder(&calls), output: json.NewDecoder(&replies)}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			if mode == "unknown-cancelled" {
				cancel()
				ctx, cancel = context.WithTimeout(t.Context(), 10*time.Millisecond)
			}
			defer cancel()
			err = s.reconcileFixtureAdmission(ctx, c, "thread", "turn", "original-task", "original-operation")
			if (err == nil) != (mode == "accepted") {
				t.Fatal("unknown/refusal became acceptance", mode, err)
			}
			dec := json.NewDecoder(&calls)
			count := 0
			for dec.More() {
				var v struct {
					Params struct {
						Name      string
						Arguments struct{ Request map[string]any }
					}
				}
				if err = dec.Decode(&v); err != nil {
					t.Fatal(err)
				}
				count++
				q := v.Params.Arguments.Request
				if v.Params.Name != "bot_tasks" || q["type"] != "reconcile" || q["task"] != "original-task" || q["requestId"] != "original-operation" {
					t.Fatal("original unknown replayed or rerouted", v)
				}
			}
			if count != len(outcomes) {
				t.Fatal("unexpected calls", count)
			}
		})
	}
}
func TestFixtureFailedDelegateRetainsOriginalIdentity(t *testing.T) {
	s, err := openNative(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reply := `{"id":"fixture-call-turn-delegate","result":{"isError":true,"structuredContent":{"ok":false,"error":{"code":"unconfirmed"},"data":{"id":"original-task","requestId":"original-operation"}}}}`
	var calls bytes.Buffer
	c := &botMCP{encode: json.NewEncoder(&calls), output: json.NewDecoder(strings.NewReader(reply))}
	result, err := s.callBot(t.Context(), c, "thread", "turn", "delegate", "bot_delegate", map[string]any{"prompt": "controlled"})
	if err == nil || result["data"].(map[string]any)["requestId"] != "original-operation" {
		t.Fatal("original uncertain receipt discarded", result, err)
	}
}
