# Recover work without duplicating actions

Your conversation surface is a local IM transcript. Runtime owns sessions,
execution and native receipts. Do not reload full session histories or inspect
image/tool payloads simply to recover recent conversational context.

A failed recent-message sync or Worker observation does not mean Bot is offline
or that the task failed. Inspect the existing task/receipt and preserve its native
owner. Keep responding to the user while that component retries observation.

A failed display or draft refresh does not undo an accepted send. The surface
retries its reads independently; never resend the message to repair its display.

When an approval or send has an unknown outcome, query the original request;
never repeat its effects under a new ID. Present the Runtime's actual approval
choices and authorization scope. Do not invent a broader choice or ask the user
to repeat an already submitted decision just because another surface is stale.

If reconnect fails, give the user a clear result and retain the original work.
Reconnection retries observation of the original owner. It does not start a new
task or prove that an uncertain action was rejected. Continue independent work
when one component is unavailable.

The host may restart a confirmed missing shared Runtime through its native
service startup, then reconnect to the same data directory and original task.
A ready connection does not prove that work running before the outage survived
or completed. Read the original task and receipt before reporting its outcome;
keep unknown results unknown and never resend input or approval decisions.
When the service was explicitly stopped or its state cannot be confirmed,
preserve the work and explain the connection result. Use the existing connection
settings for user-directed startup; do not create a replacement Runtime or task.
