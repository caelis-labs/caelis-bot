package api

import (
	"context"
	"encoding/json"
)

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}
type ToolResult struct {
	Content           []map[string]string `json:"content"`
	IsError           bool                `json:"isError"`
	StructuredContent map[string]any      `json:"structuredContent,omitzero"`
	// TurnComplete is an adapter disposition, never a model-visible instruction.
	TurnComplete bool `json:"-"`
	// Recheck whether the original handoff is still valid before acknowledging
	// it. Persisted result retries retain their original bytes.
	CheckTurnComplete func() error `json:"-"`
}

// ToolInvocation preserves a Bot tool's original delivery identity. MCP
// uses the original transport request identity within a stdio process.
type ToolInvocation struct {
	Provider, CallID, Session, Turn string
	TurnKey                         string // presentation identity of the original resident Turn
	CaelisMCP                       bool   // private Notebook MCP delivery without a native Session or Turn identity
}

type toolInvocationKey struct{}

func WithToolInvocation(ctx context.Context, invocation ToolInvocation) context.Context {
	return context.WithValue(ctx, toolInvocationKey{}, invocation)
}
func ToolInvocationFromContext(ctx context.Context) (ToolInvocation, bool) {
	invocation, ok := ctx.Value(toolInvocationKey{}).(ToolInvocation)
	if invocation.CaelisMCP {
		return invocation, ok && invocation.Provider == "caelis" && invocation.CallID != "" && invocation.Session == "" && invocation.Turn == "" && invocation.TurnKey == ""
	}
	return invocation, ok && invocation.Provider != "" && invocation.CallID != "" && invocation.Session != "" && invocation.Turn != ""
}

// ToolTurnTerminator stops the exact Codex Turn after a terminal Bot MCP tool.
type ToolTurnTerminator interface {
	CompleteToolTurn(context.Context, ToolInvocation) error
}

// ToolContextRenewer creates and commits a fresh resident Session for a durable
// handoff. Repeated calls use the original ID.
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
