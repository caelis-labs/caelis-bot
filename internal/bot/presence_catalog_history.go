package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

// Preserve the e65385fa catalog for pending native callbacks across the purpose
// addition. Historical validation never publishes the old schema to new turns.
type priorPresenceTools struct{ runtime *Runtime }

func (v priorPresenceTools) Definitions() []api.ToolDefinition {
	out := v.runtime.Definitions()
	for i := range out {
		for _, old := range scheduleDefinitionsWithPurpose(false) {
			if out[i].Name == old.Name {
				out[i] = old
			}
		}
	}
	return out
}
func (v priorPresenceTools) CallTool(ctx context.Context, name string, raw json.RawMessage) api.ToolResult {
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
