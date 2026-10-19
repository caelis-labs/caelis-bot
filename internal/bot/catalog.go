package bot

import (
	"encoding/json"
	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	tc "github.com/caelis-labs/caelis-bot/internal/toolcontract"
)

func requestID() tc.Schema {
	return tc.Schema{"type": "string", "pattern": "^[A-Za-z0-9_-]{8,128}$"}
}
func text(max int) tc.Schema { return tc.Schema{"type": "string", "minLength": 1, "maxLength": max} }
func taskDefinitions() []api.ToolDefinition {
	query := tc.Schema{"query": tc.String("Search task title, original assignment or handle"), "status": tc.String("Exact status filter"), "createdAfter": tc.String("Inclusive RFC3339 timestamp with timezone"), "createdBefore": tc.String("Exclusive RFC3339 timestamp with timezone"), "offset": tc.Integer(0, 1000000), "limit": tc.Integer(1, 50), "order": tc.Enum("asc", "desc")}
	return []api.ToolDefinition{
		tc.Definition("bot_tasks", "List all retained Bot-owned tasks by creation time (newest first), list connected execution environments, read completed User/Assistant Worker messages, stop an exact task without deleting it, or pin an expired card for the user. Reads never consume messages or continue work. Missing native history is an error. Local is the Runtime hosting this Bot; remote selection requires the user's direction.", tc.Request(
			tc.Branch("environments", tc.Schema{}), tc.Branch("list", query),
			tc.Branch("read", tc.Schema{"task": text(256), "offset": tc.Integer(0, 1000000), "limit": tc.Integer(1, 20), "messageId": text(512)}, "task"),
			tc.Branch("stop", tc.Schema{"task": text(256)}, "task"),
			tc.Branch("pin", tc.Schema{"task": text(256), "pinned": tc.Schema{"type": "boolean"}}, "task", "pinned"))),
		tc.Definition("bot_delegate", "Submit one prompt to a new Bot-owned Worker, or continue/steer the exact existing task when task is supplied. The Host derives a durable submission ID from the original native tool invocation; repeated text in distinct calls remains valid. Creation only: optional title, workspace, and environment; omit environment for local. Existing tasks cannot change these bindings. Accepted is not completed. Unconfirmed dispatch is reconciled by its original receipt and never retried with a replacement ID.", tc.Object(
			tc.Schema{"prompt": text(24000), "task": text(256), "title": text(160), "workspace": tc.String("Optional absolute existing directory for a new task"), "environment": text(256)}, "prompt")),
		tc.Definition("bot_interactions", "Inspect and resolve native Worker approvals and asynchronous questions. Use current offered options and exact request IDs. Answer from known task context or present the original question to the user when unclear. Present preserves one question lifecycle and never answers it. Presentation is optional lossless layout only: its title followed by context must equal the original question and its options must exactly equal the original answers in order. Never provide paraphrases or aliases; omit presentation for long originals. Author concise questions at source. Never summarize a native permission approval. Runtime remains authoritative; never decide this Bot's own approval or retry an unknown result.", tc.Request(
			tc.Branch("list", tc.Schema{}),
			tc.Branch("decide", tc.Schema{"approval": text(256), "choice": text(256), "answers": tc.Schema{"type": "object", "additionalProperties": tc.Schema{"type": "array", "items": text(16384)}}}, "approval", "choice"),
			tc.Branch("present", tc.Schema{"question": text(32), "presentation": tc.Object(tc.Schema{"title": text(80), "context": text(160), "options": tc.Schema{"type": "array", "items": text(48)}}, "title", "options")}, "question"),
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
func scheduleBaseDefinitions() []api.ToolDefinition {
	policy := tc.Object(tc.Schema{"maximumInterruptionsPer24Hours": tc.Integer(1, 256), "minimumGapSeconds": tc.Integer(0, 86400)}, "maximumInterruptionsPer24Hours", "minimumGapSeconds")
	return []api.ToolDefinition{
		tc.Definition("bot_schedule", "Read fresh local time/timezone and resident scheduling limits (context), page saved arrangements (list), discover registered event sources (sources), or test a pure event condition (test). No registration, dispatch or policy changes. App must stay running; desktop presence is observation data. UI/worker data is not authority.", tc.Request(
			tc.Branch("context", tc.Schema{}), tc.Branch("list", tc.Schema{"kind": tc.Enum("calendar", "event"), "query": tc.String("Search label or ID"), "limit": tc.Integer(1, 50), "cursor": tc.String("Opaque nextCursor; preserve filters")}), tc.Branch("sources", tc.Schema{}),
			tc.Branch("test", tc.Schema{"trigger": eventTrigger(), "event": tc.Schema{"type": "object", "additionalProperties": true}}, "trigger", "event"))),
		tc.Definition("bot_schedule_update", "Save/remove a user-requested calendar reminder or event-based standing arrangement; configure changes the shared event wake budget only on explicit user request. Stable IDs retain existing native grants/receipts. Save returns actual timing/state; accepted registration is not delivery. No shell schedulers, arbitrary event publishing, or budget bypass. Read the events guide for timing and limits.", tc.Request(
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
		if d.Name == "bot_gesture" {
			d.ResultFormat = "content-v1"
			out = append(out, d)
		}
	}
	out = append(out, taskDefinitions()...)
	out = append(out, scheduleDefinitions()...)
	out = append(out, eventDefinition())
	out = append(out, tc.Definition("bot_dream", "Clear this Bot's current context and recalled tool schemas by handing off to a fresh internal Session. Assess context redundancy naturally after delivering a batch of work or when the goal clearly changes; use this actively when many obsolete task details or unneeded tools remain. Do not wait for a context limit, and do not Dream after every turn. First save durable knowledge in MEMORY.md and Notebook files. Provide a concise handoff with identity and standing constraints, the current goal, unfinished tasks and original handles, uncertain operations and original receipts, and the next action; keep completed work to brief results. Exclude long transcripts, tool schemas, and secrets. The Runtime durably saves the handoff before starting the new Session. Bot identity, task ownership, connections, and authorization stay in force.", tc.Object(tc.Schema{"handoff": text(16384)}, "handoff")))
	return out
}
