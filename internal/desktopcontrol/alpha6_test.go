package desktopcontrol

import (
	"encoding/json"
	"strings"
	"testing"

	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/host"
	"github.com/caelis-labs/desktop-world/protocol"
)

func TestSemanticStateContractsPreserveFalse(t *testing.T) {
	var schema json.RawMessage
	for _, d := range Definitions() {
		if d.Name == Prefix+"act" {
			schema = d.InputSchema
		}
	}
	for _, op := range []string{"set_expanded", "set_selected", "set_checked"} {
		field := strings.TrimPrefix(op, "set_")
		for _, value := range []string{"true", "false"} {
			raw := json.RawMessage(`{"requestId":"semantic-state","steps":[{"id":"state","op":"` + op + `","target":{"ref":"control"},"` + op + `":{"` + field + `":` + value + `},"completion":"verify"}]}`)
			if _, err := tc.Decode(schema, raw); err != nil {
				t.Fatal(op, value, err)
			}
			var plan dw.Plan
			var in map[string]any
			json.Unmarshal(raw, &in)
			delete(in, "requestId")
			b, _ := json.Marshal(in)
			if err := protocol.Decode(b, &plan); err != nil {
				t.Fatal(err)
			}
			b, _ = protocol.Marshal(plan)
			if !strings.Contains(string(b), `"`+field+`":`+value) {
				t.Fatal("desired state lost", string(b))
			}
		}
		for _, arm := range []string{`{}`, `{"` + field + `":null}`, `{"checked":false,"extra":true}`} {
			raw := json.RawMessage(`{"requestId":"semantic-state","steps":[{"id":"state","op":"` + op + `","target":{"ref":"control"},"` + op + `":` + arm + `}]}`)
			if _, err := tc.Decode(schema, raw); err == nil {
				t.Fatal("accepted invalid desired state", string(raw))
			}
		}
	}
	if _, err := tc.Decode(schema, json.RawMessage(`{"requestId":"semantic-scroll","steps":[{"id":"reveal","op":"scroll_into_view","target":{"ref":"row"},"completion":"verify"}]}`)); err != nil {
		t.Fatal(err)
	}
}

func TestCooperativeLocalPreflightRejectsBeforeRegistration(t *testing.T) {
	c, f, ctx := fixtureController(t)
	for _, suffix := range []string{
		`{"id":"text","op":"keyboard.type_text","target":{"ref":"field"},"type_text":{"text":"` + strings.Repeat("😀", 129) + `"}}`,
		`{"id":"drag","op":"pointer.drag","target":{"ref":"field"},"drag":{"to":{"ref":"field"},"duration_ms":501}}`,
		`{"id":"point","op":"pointer.click","target":{"point":{"x":1,"y":2,"frame":"screen"}},"click":{"button":"left","count":1}}`,
	} {
		raw := json.RawMessage(`{"requestId":"correctable-plan","args":{"steps":[{"id":"prefix","op":"invoke","target":{"ref":"button"}},` + suffix + `]}}`)
		out := c.CallTool(ctx, Prefix+"act", raw)
		if !out.IsError || out.StructuredContent["outcome"] != "rejected" || f.calls != 1 {
			t.Fatal("prefix dispatched", out, f.calls)
		}
		if _, recorded := c.requests["correctable-plan"]; recorded {
			t.Fatal("local preflight reserved the request ID")
		}
	}
	// Only this unrecorded local rejection permits corrected arguments under the same ID.
	raw := json.RawMessage(`{"requestId":"correctable-plan","args":{"steps":[{"id":"text","op":"keyboard.type_text","target":{"ref":"field"},"type_text":{"text":"` + strings.Repeat("😀", 128) + `"}}]}}`)
	if out := c.CallTool(ctx, Prefix+"act", raw); out.IsError || f.calls != 2 {
		t.Fatal(out, f.calls)
	}
}

func TestWindowContentContractRejectsDesktopMappingArguments(t *testing.T) {
	var schema json.RawMessage
	for _, d := range Definitions() {
		if d.Name == Prefix+"inspect" {
			schema = d.InputSchema
		}
	}
	for _, raw := range []string{
		`{"request":{"type":"image","kind":"window_content"}}`,
		`{"request":{"type":"image","kind":"window_content","target":"capture","include_cursor":true}}`,
		`{"request":{"type":"image","kind":"window_content","target":"capture","region":{}}}`,
		`{"request":{"type":"image","kind":"window_content","target":"capture","max_pixel_width":1001}}`,
	} {
		if _, err := tc.Decode(schema, json.RawMessage(raw)); err == nil {
			t.Fatal("invalid capture passed", raw)
		}
	}
	for _, raw := range []string{
		`{"request":{"type":"image","kind":"window_content","target":"capture","include_cursor":false,"max_pixel_width":1000}}`,
		`{"request":{"type":"outline","scope":{"refs":["app"]},"projection":"capture_windows","fields":["name","role"]}}`,
	} {
		if _, err := tc.Decode(schema, json.RawMessage(raw)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestObservationDefaultsStaySmallAndExplicitFieldsSurvive(t *testing.T) {
	c, f, ctx := fixtureController(t)
	if f.last["budget"].(map[string]any)["max_results"] != 32 {
		t.Fatal("unbounded default")
	}
	if out := c.CallTool(ctx, Prefix+"observe", json.RawMessage(`{"requestId":"explicit-fields","args":{"scope":{"refs":["app-1"]},"fields":["states","capabilities"],"budget":{"max_results":2,"max_output_bytes":2000}}}`)); out.IsError {
		t.Fatal(out)
	}
	if f.last["fields"].([]any)[0] != "states" || f.last["budget"].(map[string]any)["max_results"] != float64(2) {
		t.Fatal("host expanded explicit query")
	}
}

func TestOversizeReceiptKeepsPartialStepAndRestorationAfterTurn(t *testing.T) {
	c, f, ctx := fixtureController(t)
	large := strings.Repeat("x", 40000)
	b, _ := protocol.Marshal(dw.Receipt{RunID: "partial-run", Outcome: "partial", SeatHealth: "fenced", Input: &dw.InputReport{Mode: "cooperative", ForegroundMS: 950, Restoration: "failed"}, Steps: []dw.StepResult{
		{ID: "delivered", Channel: "foreground_transaction", State: "dispatched", Delivery: dw.DeliveryComplete, Verification: dw.Verification("timeout"), Evidence: []dw.Predicate{{Property: "value", EqualsString: &large}}},
		{ID: "suffix", State: "skipped", Delivery: dw.DeliveryNone},
	}})
	f.reply = host.Reply{Result: b}
	c.CallTool(ctx, Prefix+"act", json.RawMessage(`{"requestId":"partial-original","args":{"steps":[]}}`))
	c.EndTurn("turn-one")
	before := f.calls
	out := c.ReadRun(t.Context(), "partial-run")
	encoded, _ := json.Marshal(out)
	if !out.IsError || len(encoded) > 32<<10 || f.calls != before {
		t.Fatal("recovery replayed or overflowed", out)
	}
	if out.StructuredContent["input"].(map[string]any)["restoration"] != "failed" || out.StructuredContent["steps"].([]map[string]any)[1]["state"] != "skipped" {
		t.Fatal("partial evidence lost", out)
	}
}

func TestDeliveredPlanWithFailedRestorationStaysUnknownAfterTurn(t *testing.T) {
	c, f, ctx := fixtureController(t)
	fault := dw.NewFault("input_restoration_failed", "foreground cleanup could not be confirmed", "never_automatically")
	b, err := protocol.Marshal(dw.Receipt{
		RunID: "cleanup-run", State: "terminal", Outcome: "unknown", SeatHealth: "fenced", Fault: fault,
		Input: &dw.InputReport{Mode: "cooperative", ForegroundMS: 397, Restoration: "failed"},
		Steps: []dw.StepResult{{ID: "submit", Channel: "semantic", State: "dispatched", Delivery: dw.DeliveryComplete, Verification: dw.Verification("not_requested")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.reply = host.Reply{Result: b, Error: fault}
	first := c.CallTool(ctx, Prefix+"act", json.RawMessage(`{"requestId":"cleanup-original","args":{"steps":[{"id":"submit","op":"invoke","target":{"ref":"button"}}]}}`))
	c.EndTurn("turn-one")
	before := f.calls
	got := c.ReadRun(t.Context(), "cleanup-run")
	a, _ := json.Marshal(first)
	z, _ := json.Marshal(got)
	if !first.IsError || !got.IsError || string(a) != string(z) || f.calls != before {
		t.Fatal("cleanup failure lost its original receipt or replayed input", first, got)
	}
	var receipt dw.Receipt
	raw, _ := json.Marshal(got.StructuredContent["result"])
	if err := protocol.Decode(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome != "unknown" || receipt.SeatHealth != "fenced" || receipt.Input == nil || receipt.Input.Restoration != "failed" || len(receipt.Steps) != 1 || receipt.Steps[0].Delivery != dw.DeliveryComplete || receipt.Fault == nil || receipt.Fault.RetryClass != "never_automatically" {
		t.Fatal("delivered input was misrepresented as no effect", receipt)
	}
}
