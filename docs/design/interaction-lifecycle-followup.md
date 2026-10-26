# Interaction lifecycle follow-up after v0.18.3

## Evidence and task boundary

A read-only production snapshot reproduced the Notebook report: Q16/Q17 carried
unknown answer delivery; Q23/Q24 were unanswered although their task cards said
completed. The captured sample also contained a newer answered question, giving
26 questions / 22 closed / 2 unknown. The checked-in sample replaces all original
prose and private/native identifiers and preserves lifecycle states, field presence,
bindings and relative times. Production data is never edited by a regression.

`Task.Status=completed` projects a native **turn**, including a yield while waiting
for an asynchronous answer. Neither final-answer prose nor a delivered completion
report proves that the user's task ended. In v0.18.3 `WorkState.TaskEnded` had no
production writer. Thus Q23/Q24 had no task-end evidence the controller could use;
changing every completed turn to TaskEnded would break valid cross-turn questions.

The Bot now explicitly finishes a task after inspecting its result:

1. Read the original messages and `bot_tasks observe`.
2. Inspect the terminal result; keep needed questions pending across turns.
3. If the work is finished, call `bot_tasks finish` with the task and `finishToken`
   from that terminal, idle observation.

The token binds the exact owner, task, machine, native generation, final status,
result and question set. Active observations have no finish token. Finish makes a
fresh original-owner read, checks idle/terminal status and pending native approvals,
and persists a generation-fenced product receipt. It registers and closes that
inspection's temporary questions before a later continuation can proceed. A stale
or changed result token fails. Peer reads release the global local-admission lock.
Finish supplies no answer, permission, native stop or uncertain-effect resolution.
An explicitly continued new execution can ask new questions; old cards stay closed.
Migration never infers finish from a legacy completed task or its prose.

## Temporary payloads and original receipts

`bot_interactions list` now contains only actionable pending approvals and questions.
Unknown/sending/dispatching delivery is a receipt to reconcile, not another user
answer to request. Exact retained receipts remain readable by ID; original Worker
conversation content remains in the native transcript, accessible through tasks.
The question ledger immediately removes title/options/presentation/task-title
payloads from every settled or uncertain record, including the current short-number
copy. The IM transcript owns its historical display. This fixes the old compactor
which removed a duplicate in AsyncClosed while leaving all original prose intact
in AsyncQuestions. Empty, unrecoverable source payloads cannot dispatch an answer.

Uncertain input evidence lives in `asyncReceipts`, independently of the actionable
projection: original owner/task/item/call/fingerprint, reply ID, operation ID,
deadline and outcome. An in-flight dispatch keeps its slot until its callback ends.
On restart a persisted dispatching claim becomes unknown; it does not become unsent.
Legacy operation IDs are recovered only from one unambiguous retained ingress
binding. If the original digest is unavailable, the record stays unknown.

At most one original receipt read per host tick, with a 400 ms timeout and three
attempts per operation/process, can reconcile unknown Worker answers. Explicit
`bot_tasks reconcile` uses the same exact original operation. A complete matching
native query and accepted/rejected outcome can settle the local ledger; failed,
missing, changed or unsupported receipts cannot. Rejection can restore an unexpired
original question only after its exact source text/options are re-observed. Neither
path sends an answer. Successful/rejected answers remove the intermediate receipt;
the bounded attempt map drops settled entries. Resident replies use only the exact
original receipt and runtime owner. Native task request journals remain untouched.

## Bounds and stale-source fencing

| State | Bound and collection rule |
| --- | --- |
| Current question references | 999 Q-number slots shared with native question forms; only pending controls retain payload. |
| Unknown answer evidence | Minimal records bound to reserved Q slots, never evicted to authorize replay. Exact proof releases them. If all slots remain genuinely uncertain, only new temporary-question admission fails closed; ordinary chat and unrelated capabilities remain available. |
| Closed question generations | Only unexpired source generations, at most 4096; expired generations are collected on the host clock. Current Q slots retain compact outcomes until recycled. Active/retired display indexes keep only referenced generations. |
| Native Worker question cache | At most 4096 unexpired source items per task; expired payloads and per-item digests are removed. Native input journals/transcripts are separate. |
| Native approval references | Separate 999 A / 999 Q pools. Exact owner/turn/schema resolved evidence releases unknown claims; expiry alone never approves or proves an unknown effect. Expired unknown references keep only their minimal binding/claim. Orphan form drafts and in-memory secret drafts are collected. |
| Settled claim/card/feedback caches | At most 4096 each; cards/claims also have the seven-day reference lifetime. These are caches, not native permission authority. |
| Retired secret quote privacy | Fixed 32 KiB monotone privacy filter after exact card collection. It can only hide a quoted reply; it never supplies a native target, choice or authorization. False positives conservatively hide extra text. |

Deleting deduplication entries alone would renew historical questions. Codex now
persists one fixed admission epoch per current native execution. Only a locally
accepted original start/continue or a live in-progress generation establishes it;
history reads and late terminal notifications never create or renew one. The epoch
has a fixed maximum 24-hour window. A native turn that remains open beyond that
window cannot admit new unknown async calls until a real new generation occurs.
Existing registered questions retain their original, non-extensible deadlines.
Unregistered historical sources carry a negative admission deadline and cannot
replace a current schema or create a fresh card. New Bot answer calls also require
the original question binding, protecting recycled short numbers after feedback
cache collection. Old unbound schemas reject without sending input.

The complete v0.18.3 tool directory is frozen for original callback digests,
including tools unchanged by this work. No closure notice or lifecycle instruction
is appended to a model prompt. No migration starts/resumes a task, decides an
approval or replays an uncertain input. IM/native history and original task journals
are intentionally outside temporary-control cache collection.

## Verification boundaries

Regression tests cover the redacted production snapshot, restart/idempotent
migration, real accepted/rejected answer dispatch and number reuse, 12,000 terminal
generations, 8,000 approval caches/drafts, retired secret Telegram quote ingress,
source/schema replay, unknown original receipt settlement, source rehydration,
finish token staleness, product finish/restart and later execution. Runtime
protocol fixtures and synthetic channel clients do not prove live model consumption
or DTW GUI effects. Daily Dev native launch and the user's DTW verification are
reported separately in the handoff.
