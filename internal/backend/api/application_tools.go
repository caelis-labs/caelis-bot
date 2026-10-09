package api

import (
	"context"
	"encoding/json"
)

// ApplicationTools is a native-only, per-session binding. Implementations own
// business behavior; adapters own authenticated invocation provenance/transport.
// Never expose this capability through the renderer or a global tool registry.
type ApplicationTools interface {
	Definitions() []ToolDefinition
	CallTool(context.Context, string, json.RawMessage) ToolResult
}
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	// ResultFormat is an adapter contract, not an MCP tool schema keyword.
	ResultFormat string `json:"-"`
}
type ToolResult struct {
	Content           []map[string]string `json:"content"`
	IsError           bool                `json:"isError"`
	StructuredContent map[string]any      `json:"structuredContent,omitzero"`
	// TurnComplete is an adapter disposition, never a model-visible instruction.
	TurnComplete bool `json:"-"`
}

// ToolInvocation binds a Bot tool to the adapter's original native call and
// resident Turn. The application callback uses its opaque receipt ID; Codex
// uses the MCP request ID because app-server does not send provider call_id to
// the MCP server.
type ToolInvocation struct {
	Provider, CallID, Session, Turn string
}

type toolInvocationKey struct{}

func WithToolInvocation(ctx context.Context, invocation ToolInvocation) context.Context {
	return context.WithValue(ctx, toolInvocationKey{}, invocation)
}
func ToolInvocationFromContext(ctx context.Context) (ToolInvocation, bool) {
	invocation, ok := ctx.Value(toolInvocationKey{}).(ToolInvocation)
	return invocation, ok && invocation.Provider != "" && invocation.CallID != "" && invocation.Session != "" && invocation.Turn != ""
}

// ToolTurnTerminator stops the exact Codex Turn after a terminal Bot MCP tool.
// Application callback results instead use Core's persisted turn_complete bit.
type ToolTurnTerminator interface {
	CompleteToolTurn(context.Context, ToolInvocation) error
}

// ToolContextRenewer creates and commits a fresh resident Session after the
// original terminal tool Turn is observed. Repeated calls use the original ID.
type ToolContextRenewer interface {
	RenewAfterTool(context.Context, ToolInvocation) (string, error)
}

// ApplicationCapabilities describes native execution boundaries, not UI state.
// Adapters that implement this port explicitly qualify the available modes.
type ApplicationCapabilities struct {
	NativeFiles         bool
	WorkerExecution     bool
	ScheduledActivation bool
}
type ApplicationCapabilityProvider interface {
	ApplicationCapabilities() ApplicationCapabilities
}

// LegacyToolProvider restores exact historical invocation bindings without
// advertising their tools to new model turns.
type LegacyToolProvider interface{ LegacyToolConnection() *ToolConnection }
