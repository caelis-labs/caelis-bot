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
}

// NativeToolCapability tags identify tool families owned by a Runtime. They
// select the integration owner, not current permission, plugin or tool readiness.
// A native owner's unavailable/refused tool must not enable a host fallback.
type NativeToolCapability string

const NativeComputerUse NativeToolCapability = "computer-use"

// ApplicationCapabilities describes native execution boundaries, not UI state.
// Adapters that implement this port explicitly qualify the available modes.
type ApplicationCapabilities struct {
	NativeTools         []NativeToolCapability
	NativeFiles         bool
	WorkerExecution     bool
	ScheduledActivation bool
}
type ApplicationCapabilityProvider interface {
	ApplicationCapabilities() ApplicationCapabilities
}

func (c ApplicationCapabilities) HasNativeTool(tag NativeToolCapability) bool {
	for _, v := range c.NativeTools {
		if v == tag {
			return true
		}
	}
	return false
}
