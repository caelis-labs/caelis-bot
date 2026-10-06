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

// The first step waits for an impossible state on a fresh fixture. A later
// invoke would leave an app-owned log entry, so cancellation/EOF can be checked
// independently without replaying an unknown input.
func TestPackagedHelperNativeRC2Lifecycle(t *testing.T) {
	helper, title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD"), os.Getenv("CAELIS_BOT_TEST_DESKTOP_LIFECYCLE_TITLE")
	logPath, markerDir := os.Getenv("CAELIS_BOT_TEST_DESKTOP_LIFECYCLE_LOG"), os.Getenv("CAELIS_BOT_TEST_DESKTOP_LIFECYCLE_MARKERS")
	if helper == "" || title == "" || logPath == "" || markerDir == "" {
		t.Skip("requires fresh isolated lifecycle fixture, helper and once-marker directory")
	}
	events, err := fixtureEvents(logPath)
	if err != nil || !events["ready:"+title] || events["menu_count:1"] {
		t.Fatal("lifecycle fixture not fresh")
	}
	mark := func(name string) {
		t.Helper()
		f, err := os.OpenFile(markerDir+"/"+name+"-attempted", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal("lifecycle scenario already attempted", name, err)
		}
		_, writeErr := f.WriteString(title + "\n")
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal("lifecycle marker failed", writeErr, closeErr)
		}
	}
	noEffect := func() {
		t.Helper()
		got, err := fixtureEvents(logPath)
		if err != nil || got["menu_count:1"] {
			t.Fatal("late menu effect after lifecycle stop", err)
		}
	}
	start := func(turn string) (*host.Client, dw.Ref) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		h, err := host.Start(ctx, host.Options{Executable: helper, AssetsDir: t.TempDir(), InputMode: dw.InputModeCooperative, InputPolicy: dw.InputShared})
		if err != nil {
			t.Fatal(err)
		}
		if h.Hello.Protocol != "desktop-world/helper-v0.1" {
			h.Close()
			t.Fatal("helper protocol mismatch")
		}
		if err := h.BeginTurn(ctx, turn); err != nil {
			h.Close()
			t.Fatal(err)
		}
		read := func(id string, args map[string]any) dw.Observation {
			t.Helper()
			reply, err := h.Call(ctx, turn, id, "observe", args)
			var out dw.Observation
			if err != nil || reply.Error != nil || protocol.Decode(reply.Result, &out) != nil {
				h.Close()
				t.Fatalf("lifecycle observation failed: %v %v", err, reply.Error)
			}
			return out
		}
		inventory := read("inventory", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}})
		var app dw.Ref
		for _, o := range inventory.Objects {
			if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title {
				if app != "" {
					h.Close()
					t.Fatal("lifecycle fixture ambiguous")
				}
				app = o.App
			}
		}
		if app == "" {
			h.Close()
			t.Fatal("lifecycle fixture absent")
		}
		if err := h.Grant(ctx, turn, app); err != nil {
			h.Close()
			t.Fatal(err)
		}
		observed := read("menu-item", map[string]any{"scope": map[string]any{"refs": []dw.Ref{app}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities"}, "match": map[string]any{"within": app, "name_equals": "Increment Fixture Counter"}, "budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000, "max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000}})
		if !observed.Coverage.Complete || len(observed.Objects) != 1 {
			h.Close()
			t.Fatal("lifecycle menu item not unique")
		}
		item := observed.Objects[0]
		enabled := item.States["enabled"]
		if item.Role != "menu_item" || enabled.Status != dw.FactKnown || enabled.Value == nil || !*enabled.Value {
			h.Close()
			t.Fatal("menu item enabled=true baseline absent")
		}
		return h, item.Ref
	}
	plan := func(ref dw.Ref) map[string]any {
		return map[string]any{"steps": []map[string]any{
			{"id": "await-disabled", "op": "wait", "after": []map[string]any{{"target": map[string]any{"ref": ref}, "property": "enabled", "equals_bool": false}}},
			{"id": "would-invoke", "op": "invoke", "target": map[string]any{"ref": ref}, "completion": "dispatch"},
		}}
	}
	// Cancel a real in-flight helper request. The host stops the turn, then the
	// same original ID is queried; no second action ID is sent.
	{
		turn := "rc2-lifecycle-cancel"
		h, item := start(turn)
		mark("cancel")
		ctx, cancel := context.WithCancel(t.Context())
		type result struct {
			reply host.Reply
			err   error
		}
		done := make(chan result, 1)
		go func() {
			reply, err := h.Call(ctx, turn, "wait-then-invoke", "act", plan(item))
			done <- result{reply, err}
		}()
		time.Sleep(300 * time.Millisecond)
		cancel()
		got := <-done
		if got.err == nil {
			h.Close()
			t.Fatal("cancelled wait unexpectedly completed")
		}
		reconcileCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		recovered, reconcileErr := h.Reconcile(reconcileCtx, turn, "wait-then-invoke")
		stop()
		var receipt dw.Receipt
		if reconcileErr == nil {
			_ = protocol.Decode(recovered.Result, &receipt)
		}
		t.Logf("cancel original transport=%v reconcile_error=%v outcome=%s steps=%d", got.err, reconcileErr, receipt.Outcome, len(receipt.Steps))
		h.Close()
		time.Sleep(1200 * time.Millisecond)
		noEffect()
	}
	// EOF of the trusted owner pipe must terminate this helper. The original
	// reply may become unavailable; we retain its ID and never send it again.
	{
		turn := "rc2-lifecycle-eof"
		h, item := start(turn)
		mark("eof")
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := h.Call(ctx, turn, "wait-then-invoke", "act", plan(item)); done <- err }()
		time.Sleep(300 * time.Millisecond)
		closeErr := h.CloseWithError()
		callErr := <-done
		t.Logf("EOF helper_close=%v original_call=%v", closeErr, callErr)
		if closeErr != nil {
			t.Fatal("helper EOF did not exit cleanly", closeErr)
		}
		time.Sleep(1200 * time.Millisecond)
		noEffect()
	}
	// A new helper sees the still-running fixture, but owns a new session. It
	// performs only observations and does not replay either stopped plan.
	{
		h, _ := start("rc2-lifecycle-restart")
		mark("restart")
		if _, err := h.Reconcile(context.Background(), "rc2-lifecycle-restart", "wait-then-invoke"); err == nil {
			h.Close()
			t.Fatal("new helper inherited old request record")
		}
		h.Close()
		noEffect()
	}
	// Preserve a small local machine-readable outcome without private paths.
	b, _ := json.Marshal(map[string]any{"cancel": "original queried; no app effect", "eof": "helper exited; no app effect", "restart": "new session did not inherit request; no app effect"})
	_ = os.WriteFile(markerDir+"/lifecycle-result.json", b, 0600)
	t.Log("published helper cancellation, EOF exit and fresh-session restart preserved no-effect log")
}
