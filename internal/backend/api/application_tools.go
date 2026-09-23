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
}
type ToolResult struct {
	Content []map[string]string `json:"content"`
	IsError bool                `json:"isError"`
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
