// Package botpolicy owns fixed product roles, independent of provider wire and OS.
// These instructions guide delegation; native execution policy remains authoritative.
package botpolicy

// Worker role is execution context; Bot identity and coordination live in its skill.
const WorkerInstructions = `Complete the assigned task in this dedicated workspace. Return concrete results, artifacts, verification, and any blockers to the coordinating assistant. Ask concise, complete questions naming their object, with at most one necessary background sentence and short distinct options. The exact option text is shown and submitted; write concise original choices without a separate display alias. A completed turn is not proof that unanswered questions are obsolete. Preserve targets and permission scope; do not send long task reports as questions. Async questions expire after 24 hours and can be dismissed. A host question-closure notice is not an answer or authorization. Keep steps requiring the missing information explicitly blocked; never select an option or infer permission from closure. Continue only independently authorized work.`

// ApprovedTools lists observation and expression tools that need no review.
// Presentation and stopping owned work are product-local operations.
// Worker interaction coordination is Bot-owned; the host restricts it to
// original Worker requests and native choices. Starting/steering work and
// persistent schedules use automatic review.
// It grants neither worker execution nor arbitrary MCP/server-wide approval.
func LegacyApprovedTools() []string {
	return []string{"bot_clock", "bot_gesture", "bot_tasks", "bot_task_read", "bot_task_machines", "bot_reminders_list", "bot_task_stop"}
}

// HistoricalLegacyApprovedTools reproduces the old version hash for pending
// callbacks only. The retired memory dispatcher rejects every call.
func HistoricalLegacyApprovedTools() []string {
	return append(RetiredCatalogApprovedTools(), "bot_memory")
}

// PreviousCompactApprovedTools preserves the direct/required policies in the
// 5903254d Caelis digest. The retired memory dispatcher still denies effects.
func PreviousCompactApprovedTools() []string {
	return append(ApprovedTools(), "bot_memory")
}

func ApprovedTools() []string {
	return []string{"bot_tasks", "bot_interactions", "bot_schedule", "bot_gesture", "bot_dream"}
}

// RetiredCatalogApprovedTools is digest metadata only, never current approval authority.
func RetiredCatalogApprovedTools() []string { return append(LegacyApprovedTools(), "bot_care_read") }
