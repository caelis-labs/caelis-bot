package desktopcontrol

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/host"
	"github.com/caelis-labs/desktop-world/protocol"
)

// Direct published-host comparison, on a new page and process, bypasses Bot's
// compact adapter. It never retries a failed or unknown semantic write.
func TestPackagedHelperDirectHostBrowserDiagnostic(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_DIRECT_TITLE")
	oncePath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_DIRECT_ONCE")
	evidencePath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_DIRECT_EVIDENCE")
	if helper == "" || title == "" || oncePath == "" || evidencePath == "" {
		t.Skip("requires published helper, exact isolated direct-host page, once marker and evidence path")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	h, err := host.Start(ctx, host.Options{Executable: helper, AssetsDir: t.TempDir(), InputMode: dw.InputModeCooperative, InputPolicy: dw.InputShared})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if h.Hello.Protocol != "desktop-world/helper-v0.1" || h.Hello.InputMode != dw.InputModeCooperative || h.Hello.Environment.InputMode != dw.InputModeCooperative {
		t.Fatal("direct helper hello/input mode mismatch")
	}
	turn := "rc2-direct-browser-diagnostic"
	if err := h.BeginTurn(ctx, turn); err != nil {
		t.Fatal(err)
	}
	defer h.EndTurn(context.Background(), turn)
	callRead := func(id, op string, args map[string]any, out any) {
		t.Helper()
		reply, err := h.Call(ctx, turn, id, op, args)
		if err != nil || reply.Error != nil || protocol.Decode(reply.Result, out) != nil {
			t.Fatalf("direct %s read failed before input: transport=%v fault=%v", op, err, reply.Error)
		}
	}
	var inventory dw.Observation
	callRead("direct-inventory", "observe", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}, &inventory)
	var window, app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title {
			if window != "" {
				t.Fatal("direct diagnostic window ambiguous")
			}
			window, app = o.Ref, o.App
		}
	}
	if window == "" || app == "" {
		t.Fatal("direct diagnostic window unavailable")
	}
	if err := h.Grant(ctx, turn, app); err != nil {
		t.Fatal("direct diagnostic grant refused", err)
	}
	grants, err := h.Grants(ctx, turn)
	if err != nil || len(grants.Grants) != 1 || grants.Grants[0].State != "active" {
		t.Fatal("direct diagnostic grant not active", err)
	}
	var outline dw.Observation
	callRead("direct-window", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{window}}, "projection": "outline", "fields": []string{"name", "role"}, "budget": map[string]any{"max_depth": 14, "max_visited_nodes": 512, "max_results": 256, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}}, &outline)
	var document dw.Ref
	for _, o := range outline.Objects {
		if o.Role == "document" && o.Name.Value != nil && *o.Name.Value == title {
			document = o.Ref
		}
	}
	if document == "" {
		t.Fatal("direct diagnostic document unavailable")
	}
	var observed dw.Observation
	callRead("direct-checkbox", "observe", map[string]any{"scope": map[string]any{"refs": []dw.Ref{document}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities"}, "match": map[string]any{"within": document, "role": "checkbox", "name_equals": "Direct host checkbox"}, "budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000, "max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000}}, &observed)
	if !observed.Coverage.Complete || len(observed.Objects) != 1 {
		t.Fatalf("direct checkbox not proven unique: complete=%t count=%d", observed.Coverage.Complete, len(observed.Objects))
	}
	target := observed.Objects[0]
	checked := target.States["checked"]
	if checked.Status != dw.FactKnown || checked.Value == nil || *checked.Value {
		t.Fatal("direct checkbox not known false before action")
	}
	var support string
	for _, cap := range target.Capabilities {
		if cap.Name == "set_checked" {
			support = cap.Support + "/" + cap.Availability
		}
	}
	if support != "supported/available" {
		t.Fatal("direct checkbox action not advertised", support)
	}
	marker, err := os.OpenFile(oncePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("direct browser input already attempted or marker unavailable", err)
	}
	if _, err := marker.WriteString(title + "\n"); err != nil {
		marker.Close()
		t.Fatal("direct marker write failed", err)
	}
	if err := marker.Close(); err != nil {
		t.Fatal("direct marker close failed", err)
	}
	reply, transportErr := h.Call(ctx, turn, "direct-set-once", "act", map[string]any{"steps": []map[string]any{{"id": "set", "op": "set_checked", "target": map[string]any{"ref": target.Ref}, "set_checked": map[string]any{"checked": true}, "completion": "verify"}}})
	var receipt dw.Receipt
	decodeErr := protocol.Decode(reply.Result, &receipt)
	recovered, reconcileErr := h.Reconcile(ctx, turn, "direct-set-once") // exact original only
	var recoveredReceipt dw.Receipt
	if reconcileErr == nil {
		_ = protocol.Decode(recovered.Result, &recoveredReceipt)
	}
	evidence := map[string]any{"helper_protocol": h.Hello.Protocol, "hello_input_mode": h.Hello.InputMode, "environment_input_mode": h.Hello.Environment.InputMode, "input_policy": h.Hello.InputPolicy, "grant_state": grants.Grants[0].State, "target_role": target.Role, "target_name": *target.Name.Value, "checked_before": false, "set_checked_capability": support, "transport_error": transportErr != nil, "reply_fault": reply.Error, "receipt": receipt, "reconcile_transport_error": reconcileErr != nil, "reconciled_run_same": receipt.RunID != "" && receipt.RunID == recoveredReceipt.RunID, "reconciled_outcome_same": receipt.Outcome == recoveredReceipt.Outcome}
	b, _ := json.MarshalIndent(evidence, "", "  ")
	if err := os.WriteFile(evidencePath, b, 0600); err != nil {
		t.Fatal("direct original receipt evidence write failed", err)
	}
	if transportErr != nil || decodeErr != nil || reconcileErr != nil || len(receipt.Steps) != 1 || receipt.RunID != recoveredReceipt.RunID {
		t.Fatalf("direct receipt incomplete; never replay: call=%v decode=%v reconcile=%v steps=%d", transportErr, decodeErr, reconcileErr, len(receipt.Steps))
	}
	t.Logf("direct host original outcome=%s delivery=%s verification=%s fault=%v; same-run reconcile; no replay", receipt.Outcome, receipt.Steps[0].Delivery, receipt.Steps[0].Verification, receipt.Fault)
}
