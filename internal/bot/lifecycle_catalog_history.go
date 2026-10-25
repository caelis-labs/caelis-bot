package bot

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

// Original callbacks keep their schema, policy and native receipt while new
// turns discover the bounded read capabilities.
type priorLifecycleTools struct{ runtime *Runtime }

func (v priorLifecycleTools) Definitions() []api.ToolDefinition {
	return archivedCatalog("pre-lifecycle")
}
func (v priorLifecycleTools) CallTool(ctx context.Context, name string, raw json.RawMessage) api.ToolResult {
	for _, d := range v.Definitions() {
		if d.Name == name {
			if _, err := tc.Decode(d.InputSchema, raw); err != nil {
				return compactError("invalid_arguments", err)
			}
			return v.runtime.CallTool(ctx, name, raw)
		}
	}
	return compactError("unavailable", errors.New("historical tool unavailable"))
}
