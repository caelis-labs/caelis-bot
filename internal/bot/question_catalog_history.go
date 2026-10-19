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

func (v priorQuestionTools) Definitions() []api.ToolDefinition {
	out := (priorEventTools{v.runtime}).Definitions()
	for i, d := range out {
		if d.Name == "bot_interactions" {
			out[i] = tc.Definition("bot_interactions", "Inspect and resolve native Worker approvals and asynchronous questions. Use current offered options and exact request IDs. Answer from known task context or ask the user when unclear. Runtime remains authoritative; never decide this Bot's own approval or retry an unknown result.", tc.Request(
				tc.Branch("list", tc.Schema{}),
				tc.Branch("decide", tc.Schema{"approval": text(256), "choice": text(256), "answers": tc.Schema{"type": "object", "additionalProperties": tc.Schema{"type": "array", "items": text(16384)}}}, "approval", "choice"),
				tc.Branch("answer", tc.Schema{"question": text(32), "value": text(16384), "requestId": requestID()}, "question", "value", "requestId")))
		}
	}
	return out
}
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
