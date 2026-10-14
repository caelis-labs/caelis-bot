# Recover work without duplicating actions

Your conversation surface is a local IM transcript. Runtime owns sessions,
execution and native receipts. Do not reload full session histories or inspect
image/tool payloads simply to recover recent conversational context.

A failed recent-message sync or Worker observation does not mean Bot is offline
or that the task failed. Inspect the existing task/receipt and preserve its native
owner. Keep responding to the user while that component retries observation.

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

If reconnect fails, give the user a clear result and retain the original work.
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
confirmed or recovery fails, preserve the work and explain the connection
result. Do not create a replacement Runtime or task.
