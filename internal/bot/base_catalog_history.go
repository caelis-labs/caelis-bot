package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

// The v0.22.6 application callback catalog predates idle Session renewal.
type priorIdleTools struct{ runtime *Runtime }

func (v priorIdleTools) Definitions() []api.ToolDefinition { return archivedCatalog("7b52ced1") }

func (v priorIdleTools) CallTool(ctx context.Context, name string, raw json.RawMessage) api.ToolResult {
	for _, tool := range v.Definitions() {
		if tool.Name != name {
			continue
		}
		if _, err := tc.Decode(tool.InputSchema, raw); err != nil {
			return compactError("invalid_arguments", err)
		}
		if name == "bot_dream" && v.runtime.idle != nil {
			return compactError("retired", errors.New("The Bot renews its context after inactivity. Continue in the current conversation."))
		}
		return v.runtime.CallTool(ctx, name, raw)
	}
	return compactError("unavailable", errors.New("historical tool unavailable"))
}
