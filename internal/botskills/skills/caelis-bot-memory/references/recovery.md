# Recover work without duplicating actions

Your conversation surface is a local IM transcript. Runtime owns sessions,
execution and native receipts. Do not reload full session histories or inspect
image/tool payloads simply to recover recent conversational context.

A failed recent-message sync or Worker observation does not mean Bot is offline
or that the task failed. Inspect the existing task/receipt and preserve its native
owner. Keep responding to the user while that component retries observation.

When an approval or send has an unknown outcome, query the original request;
never repeat its effects under a new ID. Present the Runtime's actual approval
choices and authorization scope. Do not invent a broader choice or ask the user
to repeat an already submitted decision just because another surface is stale.

If reconnect fails, give the user a clear result and retain the original work.
Reconnection retries observation of the original owner. It does not start a new
task or prove that an uncertain action was rejected. Continue independent work
when one component is unavailable.
