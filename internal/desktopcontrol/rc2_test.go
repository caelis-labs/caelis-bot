package desktopcontrol

import (
	"encoding/json"
	"strings"
	"testing"

	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/protocol"
)

func TestRC2DynamicAuthorizationKeepsNativeReviewAndTurnBoundary(t *testing.T) {
	var auth, inspect json.RawMessage
	for _, d := range Definitions() {
		switch d.Name {
		case Prefix + "authorize":
			auth = d.InputSchema
		case Prefix + "inspect":
			inspect = d.InputSchema
		}
	}
	for _, raw := range []string{
		`{"application":"app-1","name":"Fixture","purpose":"test"}`,
		`{"operation":"declare","name":"Future Fixture","purpose":"test"}`,
		`{"operation":"declare","windowTitle":"Future Window","purpose":"test"}`,
		`{"operation":"revoke","grantId":"grant-pending","purpose":"test"}`,
		`{"operation":"revoke","application":"app-1","purpose":"test"}`,
	} {
		if _, err := tc.Decode(auth, json.RawMessage(raw)); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{
		`{"operation":"declare","name":"Future","windowTitle":"Window","purpose":"test"}`,
		`{"operation":"declare","name":"Future","application":"app-1","purpose":"test"}`,
		`{"operation":"revoke","grantId":"grant-pending","application":"app-1","purpose":"test"}`,
		`{"operation":"grant","application":"app-1","name":"Fixture","purpose":"test"}`,
		`{"operation":"declare","name":"Future","purpose":""}`,
	} {
		if _, err := tc.Decode(auth, json.RawMessage(raw)); err == nil {
			t.Fatal("accepted ambiguous authorization", raw)
		}
	}
	if _, err := tc.Decode(inspect, json.RawMessage(`{"request":{"type":"grants"}}`)); err != nil {
		t.Fatal(err)
	}
	c, f, ctx := fixtureController(t)
	declared := c.CallTool(ctx, Prefix+"authorize", json.RawMessage(`{"operation":"declare","name":"Future Fixture","purpose":"test"}`))
	if declared.IsError || !strings.Contains(declared.Content[0]["text"], "pending") || f.grants != 0 {
		t.Fatal("declaration was treated as an active observed grant", declared)
	}
	if state := c.GrantStatus(ctx); state.IsError || !strings.Contains(state.Content[0]["text"], "pending") {
		t.Fatal("current turn grant status unavailable", state)
	}
	if revoked := c.CallTool(ctx, Prefix+"authorize", json.RawMessage(`{"operation":"revoke","grantId":"grant-pending","purpose":"test"}`)); revoked.IsError || !strings.Contains(revoked.Content[0]["text"], "revoked") {
		t.Fatal("revocation not reflected", revoked)
	}
	if refused := c.CallTool(ctx, Prefix+"authorize", json.RawMessage(`{"operation":"grant","application":"app-1","name":"Fixture","purpose":"test"}`)); !refused.IsError || f.grants != 0 {
		t.Fatal("undocumented grant form bypassed review", refused)
	}
	c.EndTurn("turn-one")
	if status := c.GrantStatus(ctx); !status.IsError {
		t.Fatal("ended turn retained status authority", status)
	}
	if late := c.CallTool(ctx, Prefix+"authorize", json.RawMessage(`{"operation":"declare","name":"Late","purpose":"test"}`)); !late.IsError {
		t.Fatal("ended turn accepted declaration", late)
	}
}

func TestRC2BindFocusIsOneValidatedPlanStep(t *testing.T) {
	var schema json.RawMessage
	for _, d := range Definitions() {
		if d.Name == Prefix+"act" {
			schema = d.InputSchema
		}
	}
	raw := json.RawMessage(`{"requestId":"focus-plan-001","steps":[{"id":"focus","op":"focus","target":{"ref":"window"}},{"id":"bind","op":"bind_focus","bind_focus":{"name":"input","within":"window"}},{"id":"press","op":"keyboard.press","target":{"bound":"input"},"press":{"key":"O","modifiers":["primary"]}}]}`)
	if _, err := tc.Decode(schema, raw); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	delete(body, "requestId")
	b, _ := json.Marshal(body)
	var plan dw.Plan
	if err := protocol.Decode(b, &plan); err != nil {
		t.Fatal(err)
	}
	plan.Epoch, plan.RequestID = "epoch", "epoch:focus-plan-001"
	if err := plan.Validate(); err != nil || len(plan.Steps) != 3 || plan.Steps[1].BindFocus.Within != "window" {
		t.Fatal("bind_focus did not reach the native plan intact", err)
	}
	for _, bad := range []string{
		`{"requestId":"focus-plan-001","steps":[{"id":"bind","op":"bind_focus","target":{"ref":"window"},"bind_focus":{"name":"input","within":"window"}}]}`,
		`{"requestId":"focus-plan-001","steps":[{"id":"bind","op":"bind_focus","bind_focus":{"name":"input"}}]}`,
	} {
		if _, err := tc.Decode(schema, json.RawMessage(bad)); err == nil {
			t.Fatal("invalid focus binding accepted", bad)
		}
	}
}
