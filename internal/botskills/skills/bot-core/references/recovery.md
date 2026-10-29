# Recover without replaying work

Keep the original task, request receipt and execution owner when an action's
outcome is uncertain. A failed read does not prove failed execution; a ready
connection does not prove earlier work survived. Historical output may remain
readable when its owner cannot accept more work.

Use bounded native recovery, preserve drafts and results, and continue unaffected
work. Keep harmless failures, raw errors and retry details out of conversation.
Explain a material limit plainly and ask only for a real missing input, decision
or authorization. Do not reload full histories to repair recent display context.

## Reconcile the original action

For `bot_delegate`, use the returned task and original `requestId` with `bot_tasks`
`reconcile`. If no handle was returned, search retained tasks and keep the call
unresolved until identified. A new call, even with identical text, is another
submission. Do not create a replacement to recover a missing receipt.

Accepted or rejected input stays settled after a later display/read timeout.
Never resend input or approval to repair a stale surface. Inspect original
arrangements and external action receipts under the same rule. A stop after
dispatch may leave effects unknown; it cannot undo completed effects. Native
approval decisions follow [Interactions](interactions.md).

Keep old task handles after model or connection changes. A capacity wait,
permission error, disconnect, interrupted turn or `notLoaded` is not authority to
replace a Worker or its owner. Use the original task's recovery path; do not
resume explicitly cancelled work. Preserve dependent files until reconciled,
then follow [Resources](resources.md).

## Interpret delegation results

Check the top-level outcome first. `unconfirmed` always requires reconciliation
of the original task/request, regardless of diagnostic detail. A settled refusal
does not change an earlier accepted or unknown receipt.

For confirmed `submission_rejected`, use `error.reason` and safe `error.detail`:

| Reason | Next step |
| --- | --- |
| `updater_paused`, `capacity_full` | Retry later within authorization, at least after the suggested interval; preserve workspace and intent. Arrange a callback if attention must return later. |
| `workspace_invalid` | Inspect and correct the selected path. |
| `source_not_authorized` | Inspect the original invocation binding; waiting or changing directories cannot repair it. |
| `work_admission_failed` | Inspect the cause; do not assume a transient failure or schedule a retry. |
| `native_rejected` | Inspect the refusal; do not bypass a denied approval. |
| `input_preparation_failed`, `input_validation_failed`, `workspace_preparation_failed` | Inspect that preparation stage before correcting it; no automatic timed retry. |

For `submission_conflicted` / `native_conflict`, inspect the original task and
request. Do not resend conflicting input. A completion report neither grants new
authority nor revokes existing user authorization; continue authorized work using
the normal task tools without relabeling the report as user input.

For `bot_tasks observe` returning `observation_changed`, read the same task again
when needed; no finish token was issued because its state kept advancing. Do not
replace the Worker. A failed native turn may still have an accepted submission;
inspect its safe failure metadata through [Tasks](tasks.md).
