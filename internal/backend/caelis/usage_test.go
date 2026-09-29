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
	if v.Usage.Used != 80000 || !v.Usage.ModelAt.IsZero() {
		t.Fatal("gauge asserted model activity", v.Usage)
	}
	provider := e
	provider.EventId, provider.UsageSemantics = pointer("provider"), wire.UsageSemanticsProviderUsage
	billed := json.RawMessage(`{"sessionUpdate":"usage_update","used":"9000000","size":"100000"}`)
	provider.Update = &billed
	applyLiveUsage(v, provider, now)
	if v.Usage.Used != 80000 || !v.Usage.ModelAt.Equal(now) || v.ModelTurn != v.UsageTurn {
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
	if !restored.Usage.ModelAt.IsZero() || restored.Usage.Window != 0 {
		t.Fatal("warmth persisted across restart")
	}
}
