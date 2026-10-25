//go:build workerpeere2e

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type botMCP struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *json.Decoder
	encode *json.Encoder
}

// Start the exact stdio MCP configured by the real App. This never calls a
// product HTTP API or tasks.Manager directly, and does not persist its env.
func startBotMCP(ctx context.Context, config mcpConfig, cwd string) (*botMCP, error) {
	if !filepath.IsAbs(config.Command) || len(config.Args) != 1 || config.Args[0] != "--bot-tools" || config.Env["CAELIS_BOT_TOOLS_VERSION"] != "2" || config.Env["CAELIS_BOT_ENDPOINT"] == "" || config.Env["CAELIS_BOT_TOKEN"] == "" {
		return nil, errors.New("fixture requires the real App's scoped --bot-tools stdio configuration")
	}
	cmd := exec.CommandContext(ctx, config.Command, config.Args...)
	cmd.Dir = cwd
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := config.Env[key]; !overridden {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for key, value := range config.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		return nil, err
	}
	c := &botMCP{cmd: cmd, input: in, output: json.NewDecoder(out), encode: json.NewEncoder(in)}
	if _, err := c.request("fixture-mcp-initialize", "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": fixtureBuild, "version": "0.1.0"}}); err != nil {
		c.close()
		return nil, err
	}
	if err := c.encode.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}
func (c *botMCP) close() {
	_ = c.input.Close()
	done := make(chan struct{})
	go func() { _ = c.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		_ = c.cmd.Process.Kill() // Only this fixture's owned MCP child.
		<-done
	}
}
func (c *botMCP) request(id, method string, params any) (map[string]any, error) {
	if err := c.encode.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		var reply nativeMessage
		if err := c.output.Decode(&reply); err != nil {
			return nil, err
		}
		if len(reply.ID) == 0 {
			continue
		}
		if string(reply.ID) != string(raw(id)) {
			return nil, errors.New("fixture MCP reply identity mismatch")
		}
		if reply.Error != nil {
			return nil, errors.New("real Bot MCP rejected the synthetic call")
		}
		var result map[string]any
		if json.Unmarshal(reply.Result, &result) != nil {
			return nil, errors.New("invalid real Bot MCP response")
		}
		return result, nil
	}
}

func (s *nativeServer) callBot(ctx context.Context, c *botMCP, thread, turn, step, tool string, args any) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := "fixture-call-" + turn + "-" + step
	s.item(thread, turn, nativeItem{ID: id, Type: "mcpToolCall", Server: "caelis_tasks", Tool: tool, Arguments: args, Status: "inProgress"}, false)
	result, err := c.request(id, "tools/call", map[string]any{"name": tool, "arguments": args})
	s.item(thread, turn, nativeItem{ID: id, Type: "mcpToolCall", Server: "caelis_tasks", Tool: tool, Arguments: args, Result: raw(result), Status: "completed"}, true)
	if err != nil {
		return nil, err
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		return nil, errors.New("real Bot tool omitted structuredContent")
	}
	if failed, _ := result["isError"].(bool); failed || structured["ok"] != true {
		return structured, fmt.Errorf("real Bot tool %s refused: %v", tool, structured["error"])
	}
	return structured, nil
}
