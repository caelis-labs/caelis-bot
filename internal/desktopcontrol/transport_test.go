package desktopcontrol

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	output.Encode(map[string]any{"type": "hello", "protocol": "desktop-world/helper-v0.1", "managed": true, "environment": map[string]any{"epoch": "fixture-epoch", "platform": "darwin"}})
	go func() {
		control := bufio.NewScanner(os.NewFile(3, "host-control"))
		responses := json.NewEncoder(os.NewFile(4, "control-responses"))
		for control.Scan() {
			var r struct {
				ID, Op, Turn string
				Application  dw.Ref
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
			}
			mu.Unlock()
			responses.Encode(host.Reply{ID: r.ID, Protocol: "desktop-world/host-control-v0.1", World: "fixture-epoch"})
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
			mu.Lock()
			allowed := grant && active == r.Turn
			mu.Unlock()
			if !allowed {
				reply.Error = &dw.Fault{Code: "unauthorized", Message: "fixture grant required"}
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
		output.Encode(reply)
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
