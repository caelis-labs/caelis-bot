package desktopcontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	dw "github.com/caelis-labs/desktop-world"
	"github.com/caelis-labs/desktop-world/host"
	"github.com/caelis-labs/desktop-world/protocol"
)

// This is a protocol fixture, not a native UI backend. Running our test binary
// as the helper exercises the real published Go host and separate OS pipes.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		protocolFixture()
		return
	}
	os.Exit(m.Run())
}
func protocolFixture() {
	var mu sync.Mutex
	var active string
	grant := false
	revoked := make(chan struct{})
	once := sync.Once{}
	output := json.NewEncoder(os.Stdout)
	writeReply := func(reply host.Reply) {
		body, err := protocol.Marshal(reply)
		if err != nil {
			os.Exit(8)
		}
		// Fault fields use the published wire codec, not Go's capitalized names.
		output.Encode(json.RawMessage(body))
	}
	mode, policy := dw.InputModeShared, dw.InputShared
	assets := ""
	for i, arg := range os.Args {
		if i+1 < len(os.Args) {
			switch arg {
			case "--input-mode":
				mode = dw.InputMode(os.Args[i+1])
			case "--input-policy":
				policy = dw.InputPolicy(os.Args[i+1])
			case "--assets-dir":
				assets = os.Args[i+1]
			}
		}
	}
	output.Encode(map[string]any{"type": "hello", "protocol": "desktop-world/helper-v0.1", "managed": true, "input_mode": mode, "input_policy": policy, "environment": map[string]any{"epoch": "fixture-epoch", "platform": "darwin", "input_mode": mode}})
	go func() {
		control := bufio.NewScanner(os.NewFile(3, "host-control"))
		responses := json.NewEncoder(os.NewFile(4, "control-responses"))
		for control.Scan() {
			var r struct {
				ID, Op, Turn, Name, WindowTitle, GrantID string
				Application                              dw.Ref
			}
			if protocol.Decode(control.Bytes(), &r) != nil {
				os.Exit(2)
			}
			mu.Lock()
			switch r.Op {
			case "begin_turn":
				active = r.Turn
				grant = false
			case "grant":
				grant = r.Turn == active && r.Application == "fixture-app"
			case "end_turn":
				active = ""
				grant = false
				once.Do(func() { close(revoked) })
			case "grants":
				if active != r.Turn {
					responses.Encode(host.Reply{ID: r.ID, Protocol: "desktop-world/host-control-v0.1", World: "fixture-epoch", Error: dw.NewFault("turn_expired", "no active turn", "never_automatically")})
					mu.Unlock()
					continue
				}
			}
			status, _ := protocol.Marshal(host.GrantStatus{Turn: active})
			mu.Unlock()
			responses.Encode(host.Reply{ID: r.ID, Protocol: "desktop-world/host-control-v0.1", World: "fixture-epoch", Result: status})
		}
		os.Exit(0) // Private control EOF fences the fixture just like the helper.
	}()
	data := bufio.NewScanner(os.Stdin)
	for data.Scan() {
		var r struct {
			ID, Op, Turn string
			Args         json.RawMessage
		}
		if protocol.Decode(data.Bytes(), &r) != nil {
			os.Exit(3)
		}
		reply := host.Reply{ID: r.ID, Protocol: "desktop-world/helper-v0.1", World: "fixture-epoch"}
		if r.Op == "observe" {
			name := "Fixture"
			reply.Result, _ = protocol.Marshal(dw.Observation{Epoch: "fixture-epoch", Objects: []dw.Object{{Ref: "fixture-app", Kind: dw.KindApplication, Lifecycle: dw.LifeLive, Name: dw.Fact[string]{Status: dw.FactKnown, Value: &name}}}})
		} else if r.Op == "act" {
			var plan dw.Plan
			if protocol.Decode(r.Args, &plan) != nil || plan.Epoch != "" || plan.RequestID != "" {
				os.Exit(4)
			}
			// Match the pinned helper's identity injection and whole-plan
			// validation gate, before any fixture input can be delivered.
			plan.Epoch = "fixture-epoch"
			plan.RequestID = dw.RequestID("fixture-epoch:" + r.Turn + ":" + r.ID)
			if err := plan.Validate(); err != nil {
				if !errors.As(err, &reply.Error) {
					reply.Error = dw.Invalid(err.Error())
				}
				writeReply(reply)
				continue
			}
			mu.Lock()
			allowed := grant && active == r.Turn
			mu.Unlock()
			if !allowed {
				reply.Error = &dw.Fault{Code: "unauthorized", Message: "fixture grant required"}
			} else if _, err := os.Stat(filepath.Join(assets, "fixture-complete-plans")); err == nil {
				// Explicit fixture mode: independent event evidence, not native UI.
				events, err := os.OpenFile(filepath.Join(assets, "fixture-input-events"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					os.Exit(5)
				}
				receipt := dw.Receipt{RunID: dw.RunID("fixture-run-" + r.ID), Outcome: "completed", State: "terminal", SeatHealth: "ready"}
				for _, step := range plan.Steps {
					if err := json.NewEncoder(events).Encode(map[string]string{"id": step.ID, "op": step.Op}); err != nil {
						os.Exit(6)
					}
					receipt.Steps = append(receipt.Steps, dw.StepResult{ID: step.ID, Channel: "semantic", State: "dispatched", Delivery: dw.DeliveryComplete, Verification: dw.Verification("not_requested")})
				}
				if events.Close() != nil {
					os.Exit(7)
				}
				reply.Result, _ = protocol.Marshal(receipt)
			} else {
				// Publish a fixture-only fence after reading and authorizing the
				// data request; EndTurn is tested while that request is blocked.
				for i, arg := range os.Args {
					if arg == "--assets-dir" && i+1 < len(os.Args) {
						_ = os.WriteFile(filepath.Join(os.Args[i+1], "fixture-entered"), []byte("entered"), 0600)
					}
				}
				<-revoked
				reply.Result = json.RawMessage(`{"run_id":"fixture-run","outcome":"cancelled","seat_health":"ready"}`)
			}
		}
		writeReply(reply)
	}
}

func TestPublishedHostHelperValidationRequiresNewIDForCorrectedPlan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("alpha managed host is Unix only")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	assets := t.TempDir()
	if err := os.WriteFile(filepath.Join(assets, "fixture-complete-plans"), []byte("enabled"), 0600); err != nil {
		t.Fatal(err)
	}
	c := New(exe, assets)
	t.Cleanup(c.Close)
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "validation-turn"), 8*time.Second)
	defer cancel()
	if r := c.CallTool(ctx, Prefix+"observe", json.RawMessage(`{"requestId":"validation-observe","args":{"scope":{"desktop":true}}}`)); r.IsError {
		t.Fatal(r)
	}
	if r := c.CallTool(ctx, Prefix+"authorize", json.RawMessage(`{"application":"fixture-app","name":"Fixture","purpose":"controlled validation fixture"}`)); r.IsError {
		t.Fatal(r)
	}
	bad := `{"requestId":"helper-original","args":{"steps":[{"id":"one","op":"invoke","target":{"ref":"fixture-button"}},{"id":"one","op":"invoke","target":{"ref":"fixture-button"}}]}}`
	first := c.CallTool(ctx, Prefix+"act", json.RawMessage(bad))
	if !first.IsError || !strings.Contains(first.Content[0]["text"], "empty or duplicate step id") {
		t.Fatal("duplicate step IDs did not reach the helper's validator", first)
	}
	recorded := c.requests["helper-original"]
	if recorded == nil || recorded.reply.Error == nil || recorded.reply.Error.Code != "invalid_argument" {
		t.Fatal("helper rejection was mistaken for an unrecorded local preflight")
	}
	if _, err := os.Stat(filepath.Join(assets, "fixture-input-events")); !os.IsNotExist(err) {
		t.Fatal("rejected plan delivered fixture input", err)
	}
	corrected := strings.Replace(bad, `},{"id":"one"`, `},{"id":"two"`, 1)
	conflict := c.CallTool(ctx, Prefix+"act", json.RawMessage(corrected))
	if !conflict.IsError || conflict.StructuredContent["error"].(map[string]any)["code"] != "request_conflict" {
		t.Fatal("helper request ID permitted changed arguments", conflict)
	}
	if same := c.CallTool(ctx, Prefix+"act", json.RawMessage(bad)); !same.IsError || same.Content[0]["text"] != first.Content[0]["text"] {
		t.Fatal("identical retry lost the original helper rejection", same)
	}
	corrected = strings.Replace(corrected, "helper-original", "helper-corrected", 1)
	if r := c.CallTool(ctx, Prefix+"act", json.RawMessage(corrected)); r.IsError {
		t.Fatal("corrected plan under a new ID failed", r)
	}
	events, err := os.ReadFile(filepath.Join(assets, "fixture-input-events"))
	if err != nil {
		t.Fatal(err)
	}
	var delivered []string
	scan := bufio.NewScanner(strings.NewReader(string(events)))
	for scan.Scan() {
		var event map[string]string
		if err := json.Unmarshal(scan.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		delivered = append(delivered, event["id"]+":"+event["op"])
	}
	if err := scan.Err(); err != nil || strings.Join(delivered, ",") != "one:invoke,two:invoke" {
		t.Fatal("corrected plan did not deliver exactly two ordered fixture events", delivered, err)
	}
	c.EndTurn("validation-turn")
	// Both Controller and the actual published SDK retain the first rejection.
	got := c.CallTool(t.Context(), Prefix+"reconcile", json.RawMessage(`{"requestId":"helper-original"}`))
	sdk, err := c.client.Reconcile(t.Context(), "validation-turn", "helper-original")
	if !got.IsError || got.Content[0]["text"] != first.Content[0]["text"] || err != nil || sdk.Error == nil || sdk.Error.Code != "invalid_argument" {
		t.Fatal("original helper rejection lost after correction/end", got, sdk, err)
	}
	after, err := os.ReadFile(filepath.Join(assets, "fixture-input-events"))
	if err != nil || string(after) != string(events) {
		t.Fatal("receipt recovery replayed input", string(after), err)
	}
}
func TestPublishedHostPrivateControlCanStopPendingDataAndReconcile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("alpha managed host is Unix only")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := New(exe, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "managed-turn"), 8*time.Second)
	defer cancel()
	if r := c.CallTool(ctx, Prefix+"observe", json.RawMessage(`{"requestId":"transport-observe","args":{"scope":{"desktop":true}}}`)); r.IsError {
		t.Fatal(r)
	}
	if r := c.CallTool(ctx, Prefix+"authorize", json.RawMessage(`{"application":"fixture-app","name":"Fixture","purpose":"controlled protocol fixture"}`)); r.IsError {
		t.Fatal(r)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.CallTool(ctx, Prefix+"act", json.RawMessage(`{"requestId":"transport-action","args":{"steps":[{"id":"pending","op":"invoke","target":{"ref":"fixture-button"}}]}}`))
	}()
	// Wait for the fixture's independent evidence of a blocked data request.
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(c.assets, "fixture-entered")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not enter pending action")
		}
		time.Sleep(time.Millisecond)
	}

	c.EndTurn("managed-turn")
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("independent revocation failed")
	}
	recovered := c.CallTool(t.Context(), Prefix+"reconcile", json.RawMessage(`{"requestId":"transport-action"}`))
	if len(recovered.Content) != 1 {
		t.Fatal("original response missing")
	}
	if r := c.CallTool(ctx, Prefix+"act", json.RawMessage(`{"requestId":"late-action","args":{"steps":[]}}`)); !r.IsError {
		t.Fatal("ended turn accepted a new request")
	}
}
