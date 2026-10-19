package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

// Preserve the bb00cd46 callback catalog across the additive present action.
// It validates the old schema before using unchanged answer/decision handlers.
type priorQuestionTools struct{ runtime *Runtime }

func (v priorQuestionTools) Definitions() []api.ToolDefinition { return archivedCatalog("pre-present") }
func (v priorQuestionTools) CallTool(ctx context.Context, name string, raw json.RawMessage) api.ToolResult {
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
