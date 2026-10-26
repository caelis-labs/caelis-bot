package bot

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

// Preserve the complete v0.18.3 callback digest, including unaffected tools.
// An old unbound answer cannot guess a newly recycled question generation.
type priorFollowupTools struct{ runtime *Runtime }

func (v priorFollowupTools) Definitions() []api.ToolDefinition {
	return archivedCatalog("pre-followup")
}
func (v priorFollowupTools) CallTool(ctx context.Context, name string, raw json.RawMessage) api.ToolResult {
	for _, d := range v.Definitions() {
		if d.Name != name {
			continue
		}
		if _, err := tc.Decode(d.InputSchema, raw); err != nil {
			return compactError("invalid_arguments", err)
		}
		if name == "bot_interactions" {
			var body struct{ Request struct{ Type string } }
			_ = json.Unmarshal(raw, &body)
			if body.Request.Type == "answer" {
				return compactError("question_binding_required", errors.New("read the current original question binding before answering; no input was sent"))
			}
		}
		return v.runtime.CallTool(ctx, name, raw)
	}
	return compactError("unavailable", errors.New("historical tool unavailable"))
}
