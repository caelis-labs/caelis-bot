package caelis

import (
	"encoding/json"
	"os"
	"testing"
)

// TestRuntimeACPProcess is a separate ACP peer launched by the real Host fixture.
// It declares two models; no model inference, credentials or network are involved.
func TestRuntimeACPProcess(t *testing.T) {
	if os.Args[len(os.Args)-1] != "runtime-acp-fixture" {
		t.Skip("ACP subprocess only")
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var request struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
		}
		if decoder.Decode(&request) != nil {
			os.Exit(0)
		}
		if request.ID == nil {
			continue
		}
		var result any = map[string]any{}
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentInfo": map[string]string{"name": "Settings ACP Fixture", "version": "1"}, "agentCapabilities": map[string]any{}, "authMethods": []any{}}
		case "session/new":
			result = map[string]any{"sessionId": "settings-fixture", "models": map[string]any{"currentModelId": "fixture-one", "availableModels": []any{map[string]string{"modelId": "fixture-one", "name": "Fixture One"}, map[string]string{"modelId": "fixture-two", "name": "Fixture Two"}}}}
		case "session/set_model", "authenticate":
		default:
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32601, "message": "method unavailable"}})
			continue
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
}
