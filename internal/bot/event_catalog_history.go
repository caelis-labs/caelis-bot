package bot

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
	"slices"
)

// Preserve the exact pre-events catalog for original pending Caelis callbacks.
type priorEventTools struct{ runtime *Runtime }

func (v priorEventTools) Definitions() []api.ToolDefinition {
	out := slices.DeleteFunc(v.runtime.Definitions(), func(d api.ToolDefinition) bool { return d.Name == "bot_event" })
	for i := range out {
		for _, old := range legacyScheduleDefinitions() {
			if out[i].Name == old.Name {
				out[i] = old
			}
		}
	}
	return out
}
func (v priorEventTools) CallTool(ctx context.Context, name string, raw json.RawMessage) api.ToolResult {
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
