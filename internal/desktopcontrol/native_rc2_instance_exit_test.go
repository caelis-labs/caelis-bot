package desktopcontrol

import (
	"context"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/host"
	"github.com/caelis-labs/desktop-world/protocol"
)

func TestPackagedHelperNativeRC2InstanceExit(t *testing.T) {
	helper, title := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD"), os.Getenv("CAELIS_BOT_TEST_DESKTOP_EXIT_TITLE")
	logPath, once := os.Getenv("CAELIS_BOT_TEST_DESKTOP_EXIT_LOG"), os.Getenv("CAELIS_BOT_TEST_DESKTOP_EXIT_ONCE")
	pidText := os.Getenv("CAELIS_BOT_TEST_DESKTOP_EXIT_PID")
	if helper == "" || title == "" || logPath == "" || once == "" || pidText == "" {
		t.Skip("requires exact disposable fixture PID, log and once marker")
	}
	before, err := fixtureEvents(logPath)
	if err != nil || !before["ready:"+title] || before["menu_count:1"] {
		t.Fatal("instance-exit fixture not fresh")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	h, err := host.Start(ctx, host.Options{Executable: helper, AssetsDir: t.TempDir(), InputMode: dw.InputModeCooperative, InputPolicy: dw.InputShared})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	turn := "rc2-instance-exit"
	if err := h.BeginTurn(ctx, turn); err != nil {
		t.Fatal(err)
	}
	read := func(id string, args map[string]any) dw.Observation {
		t.Helper()
		reply, err := h.Call(ctx, turn, id, "observe", args)
		var out dw.Observation
		if err != nil || reply.Error != nil || protocol.Decode(reply.Result, &out) != nil {
			t.Fatalf("instance-exit observe failed: %v %v", err, reply.Error)
		}
		return out
	}
	inventory := read("exit-inventory", map[string]any{"scope": map[string]any{"desktop": true}, "projection": "summary", "fields": []string{"name", "role", "app"}, "budget": map[string]any{"max_results": 1024, "max_output_bytes": 1 << 20, "read_deadline_ms": 5000}})
	var app dw.Ref
	for _, o := range inventory.Objects {
		if o.Kind == dw.KindWindow && o.Name.Value != nil && *o.Name.Value == title {
			if app != "" {
				t.Fatal("instance-exit fixture ambiguous")
			}
			app = o.App
		}
	}
	if app == "" {
		t.Fatal("instance-exit fixture window missing")
	}
	if err := h.Grant(ctx, turn, app); err != nil {
		t.Fatal(err)
	}
	target := read("exit-target", map[string]any{"scope": map[string]any{"refs": []dw.Ref{app}}, "projection": "outline", "fields": []string{"name", "role", "states", "capabilities"}, "match": map[string]any{"within": app, "name_equals": "Increment Fixture Counter"}, "budget": map[string]any{"max_depth": 32, "max_visited_nodes": 10000, "max_results": 4, "max_output_bytes": 16384, "read_deadline_ms": 10000}})
	if !target.Coverage.Complete || len(target.Objects) != 1 {
		t.Fatal("instance-exit target not unique")
	}
	item := target.Objects[0]
	enabled := item.States["enabled"]
	if item.Role != "menu_item" || enabled.Status != dw.FactKnown || enabled.Value == nil || !*enabled.Value {
		t.Fatal("instance-exit enabled baseline absent")
	}
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 1 {
		t.Fatal("invalid disposable fixture PID")
	}
	marker, err := os.OpenFile(once, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("instance-exit scenario already attempted", err)
	}
	_, writeErr := marker.WriteString(title + "\n")
	closeErr := marker.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("instance-exit marker failed", writeErr, closeErr)
	}
	type result struct {
		reply host.Reply
		err   error
	}
	done := make(chan result, 1)
	go func() {
		reply, err := h.Call(ctx, turn, "wait-before-exit", "act", map[string]any{"steps": []map[string]any{
			{"id": "await-disabled", "op": "wait", "after": []map[string]any{{"target": map[string]any{"ref": item.Ref}, "property": "enabled", "equals_bool": false}}},
			{"id": "would-invoke", "op": "invoke", "target": map[string]any{"ref": item.Ref}, "completion": "dispatch"},
		}})
		done <- result{reply, err}
	}()
	time.Sleep(300 * time.Millisecond)
	process, err := os.FindProcess(pid)
	if err != nil || process.Signal(syscall.SIGTERM) != nil {
		t.Fatal("could not stop exact disposable fixture PID", err)
	}
	got := <-done
	var receipt dw.Receipt
	if got.err == nil {
		_ = protocol.Decode(got.reply.Result, &receipt)
	}
	reconcileCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	recovered, reconcileErr := h.Reconcile(reconcileCtx, turn, "wait-before-exit")
	stop()
	var original dw.Receipt
	if reconcileErr == nil {
		_ = protocol.Decode(recovered.Result, &original)
	}
	t.Logf("instance-exit call_error=%v outcome=%s reconcile_error=%v same_run=%t", got.err, receipt.Outcome, reconcileErr, receipt.RunID != "" && receipt.RunID == original.RunID)
	time.Sleep(1200 * time.Millisecond)
	after, err := fixtureEvents(logPath)
	if err != nil || after["menu_count:1"] {
		t.Fatal("late menu effect after fixture instance exit", err)
	}
	if err := h.EndTurn(context.Background(), turn); err != nil {
		t.Logf("post-exit turn close: %v", err)
	}
	t.Log("published helper retained original in-flight request without late input after exact fixture instance exit")
}
