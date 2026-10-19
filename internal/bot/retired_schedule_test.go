package bot

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRetiredCareToolsCannotRegisterOrExecute(t *testing.T) {
	r, f, _ := fixture(t)
	for _, defs := range []string{string(encoded(r.Definitions())), string(encoded(r.LegacyDefinitions())), string(encoded(toolSpecs()))} {
		if strings.Contains(defs, `"name":"bot_care`) {
			t.Fatal("retired tool advertised")
		}
	}
	for _, name := range []string{"bot_care", "bot_care_read"} {
		for _, call := range []func() string{
			func() string {
				return string(encoded(r.CallTool(t.Context(), name, json.RawMessage(`{"operation":"save","id":"forged"}`))))
			},
			func() string {
				return string(encoded(r.callLegacyTool(t.Context(), name, json.RawMessage(`{"operation":"configure","policy":{}}`))))
			},
			func() string {
				return string(encoded((historicalLegacyTools{r}).CallTool(t.Context(), name, json.RawMessage(`{"operation":"remove","id":"existing"}`))))
			},
		} {
			if out := call(); !strings.Contains(out, "retired") {
				t.Fatal(out)
			}
		}
	}
	if len(f.submissions) != 0 || len(r.events.Snapshot().Plans) != 0 {
		t.Fatal("retired call had effects")
	}
	for _, purpose := range []string{"task", "care"} {
		raw := strings.Replace(nativeRequest, `"type":"event"`, `"type":"event","purpose":"`+purpose+`"`, 1)
		if !r.CallTool(t.Context(), "bot_schedule_update", json.RawMessage(raw)).IsError {
			t.Fatal("retired purpose accepted")
		}
	}
}
func TestUnifiedScheduleBudgetNeedsAuthorization(t *testing.T) {
	r, f, _ := fixture(t)
	g := &scheduleGrantEngine{fakeEngine: f, deny: true}
	r.engine = g
	raw := json.RawMessage(`{"request":{"type":"configure","policy":{"maximumInterruptionsPer24Hours":12,"minimumGapSeconds":60}}}`)
	if !r.CallTool(t.Context(), "bot_schedule_update", raw).IsError || r.events.Snapshot().Policy.MaximumInterruptionsPer24Hours != 8 {
		t.Fatal("policy changed without authority")
	}
	g.deny = false
	if r.CallTool(t.Context(), "bot_schedule_update", raw).IsError || g.authorized != "schedule-policy:limits" || r.events.Snapshot().Policy.MaximumInterruptionsPer24Hours != 12 {
		t.Fatal("policy update did not use schedule grant")
	}
}
