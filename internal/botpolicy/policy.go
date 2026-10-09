// Package botpolicy owns fixed product roles, independent of provider wire and OS.
// These instructions guide delegation; native execution policy remains authoritative.
package botpolicy

// Worker role is execution context; Bot identity and coordination live in its skill.
const WorkerInstructions = `Complete the assigned task in this dedicated workspace. Return concrete results, artifacts, verification, and any blockers to the coordinating assistant.`

// ApprovedTools lists observation and expression tools that need no review.
// Notebook memory, presentation and stopping owned work are product-local operations.
// Starting/steering work and persistent schedules use automatic review.
// It grants neither worker execution nor arbitrary MCP/server-wide approval.
func LegacyApprovedTools() []string {
	return []string{"bot_clock", "bot_gesture", "bot_tasks", "bot_task_read", "bot_task_machines", "bot_memory", "bot_reminders_list", "bot_care_read", "bot_task_stop"}
}

func ApprovedTools() []string {
	return []string{"bot_memory", "bot_tasks", "bot_schedule", "bot_gesture", "bot_dream"}
}
