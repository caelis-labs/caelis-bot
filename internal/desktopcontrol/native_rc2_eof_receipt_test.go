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

// A distinct disposable app and one-shot marker keep this EOF experiment
// separate from every earlier in-flight request. No input is replayed.
func TestPackagedHelperNativeRC2EOFReceipt(t *testing.T) {
	helper, title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD"), os.Getenv("CAELIS_BOT_TEST_DESKTOP_EOF_TITLE")
	logPath, once, resultPath := os.Getenv("CAELIS_BOT_TEST_DESKTOP_EOF_LOG"), os.Getenv("CAELIS_BOT_TEST_DESKTOP_EOF_ONCE"), os.Getenv("CAELIS_BOT_TEST_DESKTOP_EOF_RESULT")
	if helper == "" || title == "" || logPath == "" || once == "" || resultPath == "" {
		t.Skip("requires fresh isolated EOF fixture, packaged helper, once marker and result path")
	}
	events, err := fixtureEvents(logPath)
	if err != nil || !events["ready:"+title] || events["menu_count:1"] {
		t.Fatal("EOF fixture not fresh")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	h, err := host.Start(ctx, host.Options{Executable: helper, AssetsDir: t.TempDir(), InputMode: dw.InputModeCooperative, InputPolicy: dw.InputShared})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	turn := "rc2-eof-receipt"
	if err := h.BeginTurn(ctx, turn); err != nil {
		t.Fatal(err)
	}
	read := func(id string, args map[string]any) dw.Observation {
		t.Helper()
		reply, err := h.Call(ctx, turn, id, "observe", args)
		var out dw.Observation
		if err != nil || reply.Error != nil || protocol.Decode(reply.Result, &out) != nil {
			t.Fatalf("EOF observation failed: %v %v", err, reply.Error)
		}
		return out
	}
	inventory := read("inventory", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}})
	var app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title {
			if app != "" {
				t.Fatal("ambiguous EOF fixture")
			}
			app = o.App
		}
	}
	if app == "" {
		t.Fatal("EOF fixture absent")
	}
	if err := h.Grant(ctx, turn, app); err != nil {
		t.Fatal(err)
	}
	target := read("menu-item", map[string]any{"scope": map[string]any{"refs": []dw.Ref{app}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities"}, "match": map[string]any{"within": app, "name_equals": "Increment Fixture Counter"}, "budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000, "max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000}})
	if !target.Coverage.Complete || len(target.Objects) != 1 {
		t.Fatal("EOF menu target not unique")
	}
	item := target.Objects[0]
	enabled := item.States["enabled"]
	if item.Role != "menu_item" || enabled.Status != dw.FactKnown || enabled.Value == nil || !*enabled.Value {
		t.Fatal("EOF enabled=true baseline absent")
	}
	f, err := os.OpenFile(once, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("EOF request already attempted", err)
	}
	_, writeErr := f.WriteString(title + "\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("EOF marker failed", writeErr, closeErr)
	}
	type result struct {
		reply host.Reply
		err   error
	}
	done := make(chan result, 1)
	const id = "wait-before-owner-eof"
	go func() {
		reply, err := h.Call(ctx, turn, id, "act", map[string]any{"steps": []map[string]any{
			{"id": "await-disabled", "op": "wait", "timeout_ms": 5000, "after": []map[string]any{{"target": map[string]any{"ref": item.Ref}, "property": "enabled", "equals_bool": false}}},
			{"id": "would-invoke", "op": "invoke", "target": map[string]any{"ref": item.Ref}, "completion": "dispatch"},
		}})
		done <- result{reply, err}
	}()
	time.Sleep(300 * time.Millisecond)
	select {
	case completed := <-done:
		_ = h.CloseWithError()
		t.Fatalf("wait ended before EOF; no in-flight lifecycle proof: call=%v helper=%v", completed.err, completed.reply.Error)
	default:
	}
	closeErr = h.CloseWithError()
	got := <-done
	var receipt dw.Receipt
	if got.err == nil && len(got.reply.Result) != 0 {
		if err := protocol.Decode(got.reply.Result, &receipt); err != nil {
			t.Fatal("EOF original receipt invalid", err)
		}
	}
	reconcileCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	recovered, reconcileErr := h.Reconcile(reconcileCtx, turn, id)
	stop()
	var original dw.Receipt
	if reconcileErr == nil && len(recovered.Result) != 0 {
		if err := protocol.Decode(recovered.Result, &original); err != nil {
			t.Fatal("EOF reconciled receipt invalid", err)
		}
	}
	time.Sleep(1200 * time.Millisecond)
	events, err = fixtureEvents(logPath)
	if err != nil || events["menu_count:1"] {
		t.Fatal("late effect after owner EOF", err)
	}
	if receipt.RunID == "" || receipt.RunID != original.RunID || receipt.Outcome != "stopped" || original.Outcome != "stopped" || len(receipt.Steps) != 2 || receipt.Steps[1].State != "skipped" {
		t.Fatal("original EOF receipt did not retain the stopped, no-invoke result")
	}
	proof := map[string]any{"originalRequestId": id, "helperExitClean": closeErr == nil, "originalCallError": errString(got.err), "originalReplyError": got.reply.Error, "originalReceipt": receipt, "reconcileError": errString(reconcileErr), "reconciledReplyError": recovered.Error, "reconciledReceipt": original, "sameRun": receipt.RunID != "" && receipt.RunID == original.RunID, "appEffectCount": 0}
	b, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		t.Fatal("could not encode EOF evidence", err)
	}
	if err := os.WriteFile(resultPath, b, 0600); err != nil {
		t.Fatal("could not retain EOF evidence", err)
	}
	t.Logf("EOF helper_exit_clean=%t original_outcome=%s reconcile_outcome=%s same_run=%t app_effect_count=0", closeErr == nil, receipt.Outcome, original.Outcome, proof["sameRun"])
	if closeErr != nil {
		t.Fatal("helper EOF did not exit cleanly", closeErr)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
