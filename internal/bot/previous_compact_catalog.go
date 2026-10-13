package bot

// This catalog is the exact pre-tool-redesign compact contract from 5903254d.
// It is only bound to pending Caelis callbacks carrying that historical digest.
import (
	"encoding/json"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

func previousTaskDefinitions() []api.ToolDefinition {
	query := tc.Schema{"query": tc.String("Search title, assignment or task handle"), "status": tc.String("Exact status filter"), "pinned": tc.Schema{"type": "boolean"}, "limit": tc.Integer(1, 50), "cursor": tc.String("Opaque nextCursor; preserve filters")}
	return []api.ToolDefinition{
		tc.Definition("bot_tasks", "Find/read Bot-owned tasks, manage their watchlist, stop the exact requested task, retire an unusable task after original-owner idle confirmation, or find a user-specified machine. Retirement keeps history and unknown receipts but fences continuation. List is paged (20/default, 50/max), without transcripts. Watchlist changes never stop/delete work. Read acknowledges completed notices. Never select a remote automatically; missing receipts are unknown.", tc.Request(
			tc.Branch("list", query), tc.Branch("read", tc.Schema{"task": text(256)}, "task"), tc.Branch("read", tc.Schema{"requestId": requestID()}, "requestId"),
			tc.Branch("machines", tc.Schema{"query": tc.String("Machine name explicitly requested by user")}),
			tc.Branch("watchlist", tc.Schema{"action": tc.Enum("pin", "unpin", "lock", "unlock"), "task": text(256)}, "action", "task"),
			tc.Branch("watchlist", tc.Schema{"action": tc.Enum("clear")}, "action"), tc.Branch("stop", tc.Schema{"task": text(256)}, "task"),
			tc.Branch("retire", tc.Schema{"task": text(256)}, "task"))),
		tc.Definition("bot_delegate", "Start sustained user-requested work or continue/steer an owned task. Stable requestId prevents duplicates; unknown outcomes require bot_tasks.read, not a new submission. Omit machine for local work; remote must be explicitly user-selected. User-configured backend/model defaults apply only to new work. Accepted is not completed; host notifies completion.", tc.Request(
			tc.Branch("start", tc.Schema{"requestId": requestID(), "title": text(160), "prompt": text(24000), "machine": text(256), "workspace": tc.String("Optional absolute existing project directory; omitted creates a managed workspace")}, "requestId", "prompt"),
			tc.Branch("continue", tc.Schema{"requestId": requestID(), "task": text(256), "prompt": text(24000)}, "requestId", "task", "prompt"))),
		tc.Definition("bot_interactions", "Inspect and resolve native Worker approvals and asynchronous questions. Use current offered options and exact request IDs. Answer from known task context or ask the user when unclear. Runtime remains authoritative; never decide this Bot's own approval or retry an unknown result.", tc.Request(
			tc.Branch("list", tc.Schema{}),
			tc.Branch("decide", tc.Schema{"approval": text(256), "choice": text(256), "answers": tc.Schema{"type": "object", "additionalProperties": tc.Schema{"type": "array", "items": text(16384)}}}, "approval", "choice"),
			tc.Branch("answer", tc.Schema{"question": text(32), "value": text(16384), "requestId": requestID()}, "question", "value", "requestId"))),
	}
}
func previousCompactDefinitions() []api.ToolDefinition {
	var old []api.ToolDefinition
	raw, _ := json.Marshal(append([]any{retiredMemorySpec()}, toolSpecs()...))
	_ = json.Unmarshal(raw, &old)
	out := []api.ToolDefinition{}
	for _, d := range old {
		if d.Name == "bot_memory" || d.Name == "bot_gesture" {
			d.ResultFormat = "content-v1"
			out = append(out, d)
		}
	}
	out = append(out, previousTaskDefinitions()...)
	out = append(out, scheduleDefinitions()...)
	out = append(out, tc.Definition("bot_dream", "Replace this Bot's current context and recalled tool schemas when completed work, obsolete detail, or tools no longer needed make the context redundant. First preserve durable knowledge in the Notebook and MEMORY.md. Supply a complete handoff with identity and standing constraints, the current objective, unfinished tasks and their original handles, uncertain operations and their original receipts, and the next action. The host saves the handoff privately and starts a fresh internal session. This does not change the user's Bot identity, task ownership, connections, or authorization.", tc.Object(tc.Schema{"handoff": text(16384)}, "handoff")))
	return out
}
