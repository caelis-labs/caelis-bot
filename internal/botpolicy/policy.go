// Package botpolicy owns fixed product roles, independent of provider wire and OS.
// These instructions guide delegation; native execution policy remains authoritative.
package botpolicy

// Worker role is execution context; Bot identity and coordination live in its skill.
const WorkerInstructions = `Complete the assigned task in this dedicated workspace. Return concrete results, artifacts, verification, and any blockers to the coordinating assistant.`

// ApprovedTools lists observation and expression tools that need no review.
// Presentation and stopping owned work are product-local operations.
// Worker interaction coordination is Bot-owned; the host restricts it to
// original Worker requests and native choices. Starting/steering work and
// persistent schedules use automatic review.
// It grants neither worker execution nor arbitrary MCP/server-wide approval.
func LegacyApprovedTools() []string {
	return []string{"bot_clock", "bot_gesture", "bot_tasks", "bot_task_read", "bot_task_machines", "bot_reminders_list", "bot_care_read", "bot_task_stop"}
}

// HistoricalLegacyApprovedTools reproduces the old version hash for pending
// callbacks only. The retired memory dispatcher rejects every call.
func HistoricalLegacyApprovedTools() []string {
	return append(LegacyApprovedTools(), "bot_memory")
}

func ApprovedTools() []string {
	return []string{"bot_tasks", "bot_interactions", "bot_schedule", "bot_gesture", "bot_dream"}
}
