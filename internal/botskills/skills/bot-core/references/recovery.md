# Recover work without duplicating actions

Your conversation surface is a local IM transcript. Runtime owns sessions,
execution and native receipts. Do not reload full session histories or inspect
image/tool payloads simply to recover recent conversational context.

A failed recent-message sync or Worker observation does not mean Bot is offline
or that the task failed. Inspect the existing task/receipt and preserve its native
owner. Keep responding to the user while that component retries observation.

Keep harmless internal failures and recovery attempts out of conversational
replies. Recover or continue unaffected work without narrating retries, transport
errors or stale display state. If user input or authorization is actually needed,
ask for that specific decision in plain language. Never claim success for an
unconfirmed effect, hide lost work or repeat an action to make the UI look healthy.

An accepted or rejected original prompt or steering receipt stays settled even if a late response times out.
Do not ask the user to reconcile receipts or repeat their instruction.

A failed display or draft refresh does not undo an accepted send. The surface
retries its reads independently; never resend the message to repair its display.

When Codex reports a capacity wait, let the host finish its bounded retry of
the original request. Do not create another task or switch models to bypass
the wait. If the user stops it, distinguish cancellation before dispatch from
an unconfirmed stop after dispatch. Read the original task until its native
result is confirmed; a stop acknowledgement does not prove interruption or
undo earlier tool effects.

When an approval or send has an unknown outcome, query the original request;
never repeat its effects under a new ID. Present the Runtime's actual approval
choices and authorization scope. Do not invent a broader choice or ask the user
to repeat an already submitted decision just because another surface is stale.

For `bot_delegate`, the Host derives the submission ID from the native tool
invocation. Do not supply your own requestId or make a fresh call to resolve an
unconfirmed submission. If the original task handle is known, read that task.
If it is not known, search retained tasks and report the unresolved original
call honestly. The Host may reconcile the same call once internally; a second
model call is a new submission even if its prompt text is identical.

If reconnect fails, retain the original work and let the host own recovery.
Reconnection retries observation of the original owner. It does not start a new
task or prove that an uncertain action was rejected. Continue independent work
when one component is unavailable.

The host may restart a confirmed missing shared Runtime through its native
service startup, then reconnect to the same data directory and original task.
A ready connection does not prove that work running before the outage survived
or completed. Read the original task and receipt before reporting its outcome;
keep unknown results unknown and never resend input or approval decisions.
When the selected Caelis Host is stopped, the host may start it for the original
Store even after a separate service stop. If the service state cannot be
confirmed or recovery fails, preserve the work. Explain its actual impact when
the user asks, without exposing technical error text. Do not create a replacement
Runtime or task.

If the original private Codex process is confirmed gone and its original data
source cannot be verified, its old tasks remain read-only evidence. Use the explicit recovery action to establish a new
connection only after preserving those records. Never resume, replay, or send
an old stop request through the replacement connection. If process identity
or exit cannot be verified, retain the unknown state rather than claiming
that the original work stopped.

Runtime connection changes apply without restarting the Bot app. When connection
setup fails, keep the original task receipts and use the reported connection
state; do not tell the user to restart the app as the normal activation step.

The user can change their model or connection while independent tasks continue.
Settings remember the latest choice and apply it automatically; a saved choice
is not proof that execution has changed yet. Never ask the user to stop tasks,
clear history, resolve internal receipts, or repeatedly save to make a model
choice work. Keep helping through available capabilities while the host recovers.
Existing tasks stay with their original owner and approvals. Inspect those tasks
normally; do not recreate or move them because the conversation model changed.

Worker availability is temporary connection information. Preserve the original
task and native binding after a read failure, permission denial or disconnected
Runtime. Do not call a Worker permanently unavailable from `interrupted` or
`notLoaded`. Recovery observations are bounded; after they stop, explain the
impact briefly and offer retry on the original task. Read-only inspection never
resumes execution. Do not auto-continue cancelled work or replay input/tools.
A user may explicitly reopen the original terminal independently of a task turn's
status. Unknown terminal launch receipts still prohibit another launch; let the
existing terminal controller reconcile or revoke that attempt.

A disconnect, interrupted turn or hidden card does not make a worktree unused.
Preserve its files while execution or effects remain uncertain. Once the original
task is reconciled, follow [workspace cleanup](tasks.md#finish-workspace-cleanup).
Carry any retained path, reason and cleanup owner into your next handoff.
