package caelis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
	"github.com/caelis-labs/caelis-bot/internal/plugins"
)

type acceptanceToolHost interface {
	Definitions() []api.ToolDefinition
	CallTool(context.Context, string, json.RawMessage) api.ToolResult
}

// A synthetic tool port for transport and policy tests outside the resident
// catalog. Core discovers it as an ordinary MCP plugin.
func fixtureMCP(t *testing.T, host acceptanceToolHost) plugins.SelectedServer {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			ID     json.RawMessage
			Method string
			Params struct {
				Name      string
				Arguments json.RawMessage
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "acceptance-fixture", "version": "1"}}
		case "tools/list":
			result = map[string]any{"tools": host.Definitions()}
		case "tools/call":
			result = host.CallTool(r.Context(), request.Params.Name, request.Params.Arguments)
		default:
			result = map[string]any{}
		}
		writeFixture(w, map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	t.Cleanup(server.Close)
	return plugins.SelectedServer{PackageID: "acceptance", Name: "fixture", Server: plugins.Server{Type: "streamable-http", URL: server.URL + "/mcp"}}
}

func fixtureMCPConfig(t *testing.T, config *api.ToolConnection, host acceptanceToolHost) *api.ToolConnection {
	t.Helper()
	config = config.Clone()
	config.Plugins.Servers = append(config.Plugins.Servers, fixtureMCP(t, host))
	return config
}

func fixtureMCPStep(tool string, args any) modelStep {
	return nativeMCPStep(fixtureMCPName()+"__"+strings.ToLower(tool), args)
}

func mcpServerHasTool(ctx context.Context, s *Session, server, tool string) bool {
	var status wire.SessionMCPStatus
	if err := s.client.json(ctx, "GET", "/sessions/"+idPath(s.ConversationState().Session)+"/mcp-status", nil, &status, "", ""); err != nil {
		return false
	}
	return slices.ContainsFunc(status.Servers, func(item wire.SessionMCPServerStatus) bool {
		return item.Name == server && value(item.Ready) && slices.Contains(item.Tools, tool)
	})
}

func waitForFixtureMCP(t *testing.T, ctx context.Context, s *Session) {
	t.Helper()
	ready, stop := context.WithTimeout(ctx, 8*time.Second)
	defer stop()
	waitAcceptance(t, ready, func() bool {
		return mcpServerHasTool(ready, s, fixtureMCPName(), "FixtureLookup")
	})
}

func fixtureMCPName() string { return plugins.RuntimeName("acceptance", "fixture") }

// Tests that exercise native manual mode can advance an explicitly bound
// synthetic fixture call while waiting. The actual Bot, Worker and built-in
// tool approvals are unaffected and still require the test's native decision.
func allowFixtureMCP(t *testing.T, ctx context.Context, s *Session, approvals []api.Approval) {
	t.Helper()
	for _, approval := range approvals {
		if !slices.ContainsFunc(approval.Choices, func(c api.Choice) bool { return c.ID == "allow_once" }) {
			continue
		}
		s.mu.Lock()
		ref, ok := s.approvalLocked(approval.ID)
		var tool string
		if ok {
			if active := s.state.Views[ref.sid].State.Approval.Active; active != nil && active.Subject != nil {
				tool = value(active.Subject.Tool.Text)
			}
		}
		s.mu.Unlock()
		if strings.HasPrefix(tool, fixtureMCPName()+"__") {
			if err := s.Decide(ctx, api.Decision{ID: approval.ID, Choice: "allow_once"}); err != nil {
				var replaced *api.DecisionValidationError
				if !errors.As(err, &replaced) {
					t.Fatalf("native synthetic MCP approval: %v", err)
				}
				// State may advance between stream projection and the read
				// before Decide. The next poll observes the new native ID.
			}
		}
	}
}
