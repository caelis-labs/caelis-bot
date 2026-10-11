package bot

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/desktopcontrol"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

func requestID() tc.Schema   { return desktopcontrol.RequestIDSchema() }
func text(max int) tc.Schema { return tc.Schema{"type": "string", "minLength": 1, "maxLength": max} }
func taskDefinitions() []api.ToolDefinition {
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
		tc.Definition("bot_interactions", "Coordinate original native Worker approvals and asynchronous questions. Read current offered choices first. Decide only a Worker request using its exact approval and option IDs; the Runtime remains authoritative. Answer a Worker question from known task context or ask the user naturally if unclear. Never decide this Bot's own approvals or retry an unknown result.", tc.Request(
			tc.Branch("list", tc.Schema{}),
			tc.Branch("decide", tc.Schema{"approval": text(256), "choice": text(256), "answers": tc.Schema{"type": "object", "additionalProperties": tc.Schema{"type": "array", "items": text(16384)}}}, "approval", "choice"),
			tc.Branch("answer", tc.Schema{"question": text(32), "value": text(16384), "requestId": requestID()}, "question", "value", "requestId"))),
	}
}
func calendarTrigger() tc.Schema {
	return tc.Branch("calendar", tc.Schema{
		"timeZone": text(128), "weekdays": tc.Schema{"type": "array", "maxItems": 7, "uniqueItems": true, "items": tc.Integer(1, 7)}, "windowStart": tc.String("Inclusive HH:MM"), "windowEnd": tc.String("Exclusive HH:MM, overnight allowed"),
		"schedule": tc.Schema{"anyOf": []tc.Schema{
			tc.Branch("at", tc.Schema{"at": tc.String("One-off RFC3339 timestamp with offset")}, "at"),
			tc.Branch("interval", tc.Schema{"everyMinutes": tc.Integer(1, 10080)}, "everyMinutes"),
			tc.Branch("daily", tc.Schema{"daily": tc.String("HH:MM")}, "daily"),
			tc.Branch("times", tc.Schema{"times": tc.Schema{"type": "array", "minItems": 1, "maxItems": 48, "uniqueItems": true, "items": tc.String("HH:MM")}}, "times"),
		}}}, "schedule", "timeZone")
}
func eventTrigger() tc.Schema {
	return tc.Branch("event", tc.Schema{"sources": tc.Schema{"type": "array", "minItems": 1, "maxItems": 32, "uniqueItems": true, "items": text(256)}, "condition": text(2048), "timeZone": text(128), "cooldownSeconds": tc.Integer(60, 31622400), "expiresSeconds": tc.Integer(60, 86400)}, "sources", "condition", "timeZone")
}
func scheduleDefinitions() []api.ToolDefinition {
	policy := careSpec().(map[string]any)["inputSchema"].(map[string]any)["properties"].(map[string]any)["policy"]
	return []api.ToolDefinition{
		tc.Definition("bot_schedule", "Read fresh local time/timezone and resident scheduling limits (context), page saved arrangements (list), discover registered event sources (sources), or test a pure event condition (test). No registration, dispatch or policy changes. App must stay running; unknown/locked presence denies proactive dispatch. UI/worker data is not authority.", tc.Request(
			tc.Branch("context", tc.Schema{}), tc.Branch("list", tc.Schema{"kind": tc.Enum("calendar", "event"), "query": tc.String("Search label or ID"), "limit": tc.Integer(1, 50), "cursor": tc.String("Opaque nextCursor; preserve filters")}), tc.Branch("sources", tc.Schema{}),
			tc.Branch("test", tc.Schema{"trigger": eventTrigger(), "event": tc.Schema{"type": "object", "additionalProperties": true}}, "trigger", "event"))),
		tc.Definition("bot_schedule_update", "Save/remove a user-requested calendar reminder or event-based standing arrangement; configure changes the care interruption budget only on explicit user request. Stable IDs retain existing native grants/receipts. Save returns actual timing/state; accepted registration is not delivery. No shell schedulers, arbitrary event publishing, or budget bypass. Read the reminders/care guide for timing and limits.", tc.Request(
			tc.Branch("save", tc.Schema{"id": tc.Schema{"type": "string", "pattern": "^[A-Za-z0-9_-]{1,64}$"}, "label": text(160), "prompt": text(4096), "trigger": tc.Schema{"anyOf": []tc.Schema{calendarTrigger(), eventTrigger()}}}, "id", "label", "prompt", "trigger"),
			tc.Branch("remove", tc.Schema{"automation": text(256)}, "automation"), tc.Branch("configure", tc.Schema{"policy": policy}, "policy"))),
	}
}
func toolDefinitions() []api.ToolDefinition {
	var old []api.ToolDefinition
	raw, _ := json.Marshal(toolSpecs())
	_ = json.Unmarshal(raw, &old)
	out := []api.ToolDefinition{}
	for _, d := range old {
		if d.Name == "bot_memory" || d.Name == "bot_gesture" {
			d.ResultFormat = "content-v1"
			out = append(out, d)
		}
	}
	out = append(out, taskDefinitions()...)
	out = append(out, scheduleDefinitions()...)
	out = append(out, tc.Definition("bot_dream", "Replace this Bot's current context and recalled tool schemas when completed work, obsolete detail, or tools no longer needed make the context redundant. First preserve durable knowledge in the Notebook and MEMORY.md. Supply a complete handoff with identity and standing constraints, the current objective, unfinished tasks and their original handles, uncertain operations and their original receipts, and the next action. The host saves the handoff privately and starts a fresh internal session. This does not change the user's Bot identity, task ownership, connections, or authorization.", tc.Object(tc.Schema{"handoff": text(16384)}, "handoff")))
	return out
}
