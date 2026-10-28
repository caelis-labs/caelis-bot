package caelis

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func TestLiveUsageSeparatesContextGaugeFromBilling(t *testing.T) {
	now := time.Now().Round(0)
	v := &view{State: wire.SessionState{SessionId: "resident"}, Seen: map[string]bool{}}
	update := json.RawMessage(`{"sessionUpdate":"usage_update","used":"80000","size":"100000"}`)
	e := wire.Envelope{EventId: pointer("gauge"), SessionId: pointer("resident"), TurnId: pointer("turn"), Scope: pointer("main"), OccurredAt: &now, Update: &update, UsageSemantics: wire.UsageSemanticsContextGauge}
	applyLiveUsage(v, e, now)
	if v.Usage.Used != 80000 || !v.Usage.ModelAt.IsZero() || v.UsageEvidence != "awaiting_provider_usage" {
		t.Fatal("gauge asserted model activity", v.Usage)
	}
	provider := e
	provider.EventId, provider.UsageSemantics = pointer("provider"), wire.UsageSemanticsProviderUsage
	billed := json.RawMessage(`{"sessionUpdate":"usage_update","used":"9000000","size":"100000","cost":{"total":0.012,"currency":"USD"}}`)
	provider.Update = &billed
	applyLiveUsage(v, provider, now)
	if v.Usage.Used != 80000 || !v.Usage.ModelAt.Equal(now) || v.ModelTurn != v.UsageTurn || v.UsageEvidence != "live_gauge_and_provider" || !v.Usage.LastProviderCostKnown || v.Usage.LastProviderCost != 0.012 || v.Usage.LastProviderCostCurrency != "USD" {
		t.Fatal(v.Usage)
	}
	for _, kind := range []string{"duplicate", "worker", "child", "old", "missing-time", "unknown-semantics"} {
		t.Run(kind, func(t *testing.T) {
			bad := provider
			later := now.Add(time.Minute)
			bad.OccurredAt = &later
			switch kind {
			case "duplicate":
				v.Seen["provider"] = true
				defer delete(v.Seen, "provider")
			case "worker":
				bad.SessionId = pointer("worker")
			case "child":
				bad.ParentTool = &wire.ParentToolRelation{}
			case "old":
				old := now.Add(-time.Hour)
				bad.OccurredAt = &old
			case "missing-time":
				bad.OccurredAt = nil
			case "unknown-semantics":
				bad.UsageSemantics = ""
			}
			applyLiveUsage(v, bad, later)
			if !v.Usage.ModelAt.Equal(now) {
				t.Fatal("untrusted freshness", kind)
			}
		})
	}
	bytes, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var restored view
	if err = json.Unmarshal(bytes, &restored); err != nil {
		t.Fatal(err)
	}
	if !restored.Usage.ModelAt.IsZero() || restored.Usage.Window != 0 || restored.UsageEvidence != "" {
		t.Fatal("warmth persisted across restart")
	}
}

func TestLiveCacheCountersPreservePresenceAndTTL(t *testing.T) {
	now := time.Now().Round(0)
	v := &view{State: wire.SessionState{SessionId: "resident"}, Seen: map[string]bool{}}
	update := json.RawMessage(`{"sessionUpdate":"usage_update","used":"123","size":"1000","_meta":{"caelis":{"usage":{"prompt_tokens":"100","completion_tokens":"23","cached_input_tokens":"0","cached_input_tokens_reported":true,"cache_write_tokens":"70","cache_write_tokens_reported":true,"cache_write_5m_tokens":"50","cache_write_5m_tokens_reported":true,"cache_write_1h_tokens":"20","cache_write_1h_tokens_reported":true}}}}`)
	e := wire.Envelope{EventId: pointer("billing"), SessionId: pointer("resident"), TurnId: pointer("turn"), OccurredAt: &now, Update: &update, UsageSemantics: wire.UsageSemanticsProviderUsage}
	applyLiveUsage(v, e, now)
	u := v.Usage
	if u.InputTokens != 100 || u.OutputTokens != 23 || u.CacheReadTokens != 0 || !u.CacheReadTokensReported || u.CacheWriteTokens != 70 || !u.CacheWriteTokensReported || u.CacheWrite5mTokens != 50 || !u.CacheWrite5mTokensReported || u.CacheWrite1hTokens != 20 || !u.CacheWrite1hTokensReported || u.Used != 0 {
		t.Fatal(u)
	}
	// Background accounting never establishes a foreground turn or its warmth.
	e.TurnId = nil
	later := now.Add(time.Second)
	e.OccurredAt = &later
	applyLiveUsage(v, e, later)
	if v.Usage != u || v.ModelTurn != "turn" {
		t.Fatal("background usage replaced resident gauge")
	}
	// A later response with missing counters clears old presence; zero is not
	// inferred from the struct's zero value or the usage update's total.
	e.TurnId = pointer("turn")
	e.EventId = pointer("missing")
	empty := json.RawMessage(`{"sessionUpdate":"usage_update","used":"0","size":"0"}`)
	e.Update = &empty
	applyLiveUsage(v, e, later)
	if v.Usage.CacheReadTokensReported || v.Usage.CacheWriteTokensReported || v.Usage.CacheWrite5mTokensReported || v.Usage.CacheWrite1hTokensReported || v.Usage.InputTokens != 0 {
		t.Fatal(v.Usage)
	}
}
