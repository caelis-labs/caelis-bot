# Continue helping through a local failure

Inspect the original task or request when a send, approval, observation or action
has an uncertain result. Keep its handle, receipt and execution owner. Check the
actual outcome rather than repeating work to repair a display or obtain a result.
A failed read is not proof of failed execution; a ready connection is not proof
that earlier work survived. Historical messages can remain readable even when
their original owner cannot accept more work.

Keep harmless failures and automatic retries out of conversation. Continue
unaffected work, preserve drafts and useful results, and explain a material limit
in plain language when it prevents progress. Ask for the specific input, choice
or authorization needed; do not ask the user to debug receipts or repeat an
already accepted instruction. The connection's recovery controls handle bounded
reconnection. Do not reload full histories or image/tool payloads just to recover
recent conversational context.

## Check an uncertain submission

An accepted or rejected prompt stays settled even if a later response times out.
A failed message or draft refresh does not undo an accepted send. Never resend
input or an approval merely because another surface is stale.

For `bot_delegate`, inspect its returned task and original `requestId` with the
task read/reconciliation tools. If no handle was returned, search retained tasks
and keep the original call unresolved until it can be identified. Do not supply
a new requestId or make another delegation call: identical text still creates a
new submission. The same principle applies to arrangements and external effects;
inspect their original receipts rather than changing IDs or tools to retry them.

A pending approval means the command has not started; acceptance permits it to
proceed but does not prove success. Use the exact offered choices. A denied review
does not authorize the operation through another route. After a stop, distinguish
confirmed cancellation before dispatch from an unconfirmed stop after dispatch.
Read the original result; stopping does not undo earlier effects.

## Keep the original work

A capacity wait, disconnect, permission error, interrupted turn or `notLoaded`
result does not justify a replacement Worker, model switch or new execution owner.
Let the original request's bounded recovery finish. If it cannot continue, explain
the impact and use the retry or recovery action for that original task. Read-only
inspection does not resume work; do not auto-continue cancelled work.

Model and connection choices may change while Workers continue. A saved choice
may still be applying. Inspect old tasks through their existing handles without
asking the user to switch back, stop work or clear history. A replacement
connection cannot receive an old approval, stop or uncertain input. If the original
owner's exit cannot be verified, keep the outcome unknown.

A hidden card or disconnected terminal does not make its files unused. Preserve
resources that may still be in use, then follow [Working resources](resources.md)
once the original work is reconciled. Carry retained paths and cleanup obligations
in the ongoing notes or handoff.

For `submission_rejected`, use `error.reason` and the privacy-safe `error.detail`
to decide the next step. `updater_paused`, `capacity_full` and
`work_admission_failed` permit retrying later within the original authorization;
wait at least the suggested interval, avoid a tight retry loop, and keep the same
workspace and intent. Only `workspace_invalid` calls for correcting the selected
path. For `native_rejected`, inspect the refusal before taking another action;
do not bypass a denied approval. For `submission_conflicted` / `native_conflict`,
inspect the original task and request rather than resending conflicting input.
A settled refusal does not change an earlier accepted or uncertain receipt.
An `unconfirmed` response retains the original task/request and must be reconciled
without replay, regardless of any error detail. Keep diagnostic detail out of
user-facing chat.
A failed native turn can still have an accepted submission receipt. Inspect its
safe `failure` metadata using the original task; unknown fields stay unknown.
