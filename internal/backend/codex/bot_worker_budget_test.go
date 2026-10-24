package codex

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"testing"
)

func TestTaskMCPBudgetCoversFileBridgeWithoutChangingOtherServices(t *testing.T) {
	catalog, _ := json.Marshal([]api.ToolDefinition{{Name: "bot_delegate"}, {Name: "bot_clock"}, {Name: "bot_dream"}})
	c := &api.ToolConnection{Env: map[string]string{"CAELIS_BOT_TOOL_CATALOG": string(catalog)}}
	for _, tc := range []struct {
		name, tool string
		seconds    int
	}{{"caelis_tasks", "bot_delegate", int(api.WorkerMCPTimeout.Seconds())}, {"caelis_context", "bot_dream", 45}, {"caelis_schedule", "bot_clock", 15}} {
		config := serviceToolConfig(c, api.ToolService{Name: tc.name, Tools: []string{tc.tool}})
		if config["tool_timeout_sec"] != tc.seconds {
			t.Fatal(tc.name, config)
		}
	}
}
