// Package botpolicy owns fixed product roles, independent of provider wire and OS.
// These instructions guide delegation; native execution policy remains authoritative.
package botpolicy

// Worker role is execution context; Bot identity and coordination live in its skill.
const WorkerInstructions = `Complete the assigned task in this dedicated workspace. Return concrete results, artifacts, verification, and any blockers to the coordinating assistant.`

// ApprovedTools returns a fresh, explicit set of host-owned routine operations.
// It grants neither worker execution nor arbitrary MCP/server-wide approval.
func ApprovedTools() []string {
	return []string{"bot_clock", "bot_reminders", "bot_gesture", "bot_tasks", "bot_task_start", "bot_task_read", "bot_task_send", "bot_task_stop", "bot_memory"}
}
