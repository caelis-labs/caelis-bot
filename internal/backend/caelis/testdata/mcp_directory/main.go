package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This process is a task-local synthetic service. It has no account access.
func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "bot-index-fixture", Version: "1.0.0"}, nil)
	for i := range 1200 {
		name := fmt.Sprintf("lookup_%04d", i)
		description := fmt.Sprintf("Synthetic lookup %04d. Find a local test value in the isolated fixture catalog; this capability has no external account or business data. The extra description text verifies that the Bot index does not inherit the 128-character detail-row preview limit.", i)
		mcp.AddTool[map[string]any, any](server, &mcp.Tool{Name: name, Description: description}, func(_ context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "synthetic-only"}}}, nil, nil
		})
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	audit := os.Getenv("FIXTURE_MCP_AUDIT")
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "fixture read failed", http.StatusBadRequest)
			return
		}
		if bytes.Contains(body, []byte(`"method":"tools/list"`)) && audit != "" {
			if f, err := os.OpenFile(audit, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
				_, _ = f.WriteString("tools/list\n")
				_ = f.Close()
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		handler.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	fmt.Printf("http://%s\n", listener.Addr())
	if err := http.Serve(listener, wrapped); err != nil {
		panic(err)
	}
}
