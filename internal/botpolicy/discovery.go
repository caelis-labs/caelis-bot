package botpolicy

// Fixed across turns. Bot identity and trigger text never rewrite this prefix.

// A source listing is not a per-tool catalog in Codex 0.153.4. Keep only names
// and purpose upfront; schemas and callable handles remain native tool_search data.
const ToolDiscovery = `
Caelis Bot provides the caelis_bot MCP source. Discover the needed tool before calling it: use tool_search when exposed; in Code Mode use the native ALL_TOOLS name/description lookup if tool_search is not exposed.
- bot_clock: read local time and scheduling availability.
- bot_reminders: list, create, update or remove user-requested resident reminders.
- bot_memory: recall, remember, correct or forget bounded memory evidence. Core identity and durable notes live in Notebook.
- bot_gesture: brief attention, nod or celebrate feedback on the desktop pet.
- bot_tasks: list Bot-owned tasks and their status, without scanning other Codex conversations.
- bot_task_start: delegate user-requested professional work to a dedicated managed workspace; use a stable requestId.
- bot_task_read: inspect a task and its bounded result; use authoritative status, not prose.
- bot_task_send: continue or steer an owned task with a stable requestId.
- bot_task_stop: interrupt an owned task's exact active turn.
Use the discovered schema and native receipt; if discovery or execution fails, report that instead of claiming success.`
