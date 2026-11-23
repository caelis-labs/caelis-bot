package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// The Caelis adapter supplies the context only after claiming the authenticated
// Control callback. Both ends of the framed MCP connection stay inside Bot.
// No other process can acquire the right to call through this connection.
func (b *Bridge) callResidentMCP(ctx context.Context, name string, args json.RawMessage) api.ToolResult {
	invocation, ok := api.ToolInvocationFromContext(ctx)
	if !ok || invocation.Provider != "caelis" || invocation.Session == "" || invocation.Turn == "" || invocation.CallID == "" ||
		b.life.Err() != nil || ctx.Err() != nil || name != "bot_delegate" && name != "bot_tasks" {
		return compactError("invocation_unavailable", errors.New("The original resident tool call could not be confirmed."))
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(b.life, cancel)
	defer stop()
	var input, output bytes.Buffer
	input.WriteString("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-06-18\",\"capabilities\":{},\"clientInfo\":{\"name\":\"caelis-bot\",\"version\":\"1\"}}}\n")
	input.WriteString("{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n")
	request := struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"params"`
	}{JSONRPC: "2.0", ID: 2, Method: "tools/call"}
	request.Params.Name, request.Params.Arguments = name, args
	unconfirmed := func() api.ToolResult {
		return compactError("invocation_unavailable", errors.New("The original resident tool call could not be confirmed."))
	}
	if err := json.NewEncoder(&input).Encode(request); err != nil {
		return unconfirmed()
	}
	err := serveStdio(&input, &output, func(key string) string {
		if key == "CAELIS_BOT_SERVICE_NAME" {
			return "caelis_tasks"
		}
		return ""
	}, func(call toolRequest) toolResult {
		return b.runtime.CallTool(ctx, call.Name, call.Arguments)
	})
	if err != nil {
		return unconfirmed()
	}
	decoder := json.NewDecoder(&output)
	var handshake struct {
		ID     int `json:"id"`
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	var response struct {
		ID     int             `json:"id"`
		Result *api.ToolResult `json:"result"`
	}
	if decoder.Decode(&handshake) != nil || handshake.ID != 1 || handshake.Result.ProtocolVersion != "2025-06-18" ||
		decoder.Decode(&response) != nil || response.ID != 2 || response.Result == nil {
		return unconfirmed()
	}
	return *response.Result
}

// Retained profiles may try to start the old bearer transport before rebind.
// Only the ordinary environment-configured stdio service remains executable.
func RunStdioArgs(args []string, in io.Reader, out io.Writer) error {
	if len(args) != 0 {
		return errors.New("this Bot service launch is no longer supported")
	}
	return RunStdio(in, out)
}
