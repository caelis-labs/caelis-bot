package desktopcontrol

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in. This checks the real packaged helper transport/argument
// boundary without granting any application or sending any native input.
func TestPackagedHelperReadAndDeniedInput(t *testing.T) {
	helper := os.Getenv("CAELIS_BOT_TEST_DESKTOP_WORLD")
	if helper == "" {
		t.Skip("set an absolute packaged Desktop World helper")
	}
	c := New(helper, t.TempDir())
	defer c.Close()
	ctx, cancel := context.WithTimeout(WithTurn(t.Context(), "packaged-helper-test"), 20*time.Second)
	defer cancel()
	read := c.CallTool(ctx, Prefix+"observe", json.RawMessage(`{"requestId":"packaged-read","args":{"scope":{"desktop":true},"fields":["role"],"budget":{"max_results":1,"max_output_bytes":1024}}}`))
	c.mu.Lock()
	started := c.client != nil
	c.mu.Unlock()
	if !started {
		t.Fatal("packaged helper handshake failed", read.Content)
	}
	// Empty steps are rejected before effects, even if AX permission exists.
	denied := c.CallTool(ctx, Prefix+"act", json.RawMessage(`{"requestId":"packaged-refusal","args":{"steps":[]}}`))
	if !denied.IsError || strings.Contains(denied.Content[0]["text"], "helper owns epoch") {
		t.Fatal("wrong managed argument contract", denied.Content)
	}
	c.EndTurn("packaged-helper-test")
	got := c.CallTool(t.Context(), Prefix+"reconcile", json.RawMessage(`{"requestId":"packaged-refusal"}`))
	if !got.IsError || len(got.Content) != 1 || got.Content[0]["text"] != denied.Content[0]["text"] {
		t.Fatal("packaged refusal receipt lost after EndTurn")
	}
	t.Log("real packaged helper handshake, bounded read response, invalid-input refusal and original receipt recovery; no application grants or UI input")
}
