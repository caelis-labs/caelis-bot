# Lightweight memory-review invitations

Based on main `16dd63547a934cacf4e44e415a4375c03026cec6` and #258 system delivery.
Replaces #256's stacked, multi-window implementation. Dream handoff stability
belongs to #255; this producer never calls Dream or creates a native session.
The sole model workflow is [bot-core memory guidance](../../internal/botskills/skills/bot-core/references/memory.md).

## Signals actually available

| Signal | Acquisition and missing handling | Meaning and limits |
| --- | --- | --- |
| Elapsed time | First live resident turn observation, persisted with the binding. Future timestamps/clock rollback add no age. | Age **lower bound**, not native birth or file mtime. No history scan. |
| Completed turns | Exact live resident start + successful completion; one vote per native turn. Interrupted/failed turns, historical projection, review/child and known host-notice turns excluded. | Observed count, not the total session history or amount of obsolete work. Restart during a turn may miss one observation. |
| Compactions | Codex completed `contextCompaction` items; Caelis typed successful `compact` notices, deduplicated by native identity. Count only within an observed completed ordinary turn. | Observed lower bound. No notice is not proof that the Runtime never compacted. Token drops and `compact_failed` never count. |
| Ordinary input | Up to 8 positive per-turn minimum inputs, same known model/provider/assembly. Codex `last.inputTokens` with increasing native totals; Caelis `usage_update._meta.caelis.sdk.usage` with explicit cache-read accounting. Separate cache reads are added once; cache writes are already included. | Request-input proxy, **not** retained-context waterline, stale content or money. Missing accounting/model/positive input makes this turn unavailable; no zero sample. Compaction/review turns excluded. Unknown latest input supplies no numerical credit; other signals remain usable. |
| Staleness/cost/benefit of renewal | Not measured. | The model must judge useful content and current user intent. A large input can be entirely useful. |

The eight samples compare observed ordinary inputs, not an initial-session or
post-compaction baseline. Known model/provider/assembly changes reset the samples.
Unexposed provider/tool changes remain a limitation; this signal never decides
whether content can be deleted. No new sampling requests, timer model calls,
user settings, history indexing or semantic-obsolescence framework are added.

## Explainable accumulation

At the first idle observation after **a new successful Turn Complete**, add:

- Age: `min(observed days / 14, 1)`.
- Activity: `min(new completed turns / 64, 1)`.
- Compact activity: `min(new observed compactions / 3, 2)`.
- Input: at most 1 point. The lower median grows linearly from 0 at 32k to 1 at
  64k. With 8 samples, also compare the earlier/later four medians: growth needs
  both a doubling and a 16k rise for the full point. Take the larger evidence,
  weighted by `sample count / 8`. Missing latest input contributes no evidence,
  and stays explicitly unavailable in the message.

Invite at total **3**. This is an uncalibrated evidence score, not a probability.
14 days/64 turns give slow bounded time/activity support; neither can dominate.
Three compactions can contribute one point without requiring input telemetry.
32k/64k and 2x/+16k are provisional scales for conspicuous request growth, not
context capacity or cost limits. Eight independent minima plus a lower median
reduce sensitivity to one attachment or a tool-heavy turn. There are no required
UTC windows, minimum session days, window utilization, 48-hour spans, baseline
provenance trees or simultaneous mandatory health thresholds.

Examples: a month-old 20k stable session with no observed compactions stays
below threshold regardless of turn count. A 14-day observed session with 64 completed turns and either 3
compactions or sustained 64k input can invite review. Missing input does not block
the compaction path. A one-day session gets only 1/14 of the age point.

Accepted **or definitively rejected** invitations start a seven-day cooldown and
reset activity/compact credits. The latter avoids a rejection loop. Bot deferral
requires no tool or special protocol: it may continue the same session, with the
same cooldown. Expiry alone does not wake it; a new completion and sufficient new
activity are required. A one-minute completion freshness bound rejects old
reconnect evidence; it is a transport freshness check, not an additional health
threshold. Missed completions conservatively undercount, never invent votes.

## Delivery and persistence

The resident Tick reads adapter observations after ordinary schedule/report
producers. Only a fresh completed resident head can prepare a message. Persist the
original ID, text and native owner before delivery. `SubmitSystem` is the sole
submission path; `SystemReceipt`/the #258 ledger own delivery outcomes. Unknown
keeps its original message across restart and only reconciles that receipt.
Definitive Deferred non-dispatch can retry that same ID; stale or changed-binding
candidates may be retired only after this proof. User ingress, pending handoff,
busy turns, approvals and pause keep the existing admission rules.

`continuity_review` is an independent hidden system input. It uses the existing
`Scheduled` **presentation-only** flag for quiet final responses. Caelis routes it
as `external_material`, without an arrangement grant; Codex uses its existing
system adapter. Codex owner fencing uses the same native binding as the published snapshot
(the old check read an unset internal presentation field). Original user bytes
and native authority are unchanged. No new
role handling, `application_summary`, second receipt ledger or automatic Dream.
The message describes limited observations and points to the one memory workflow.

Adapter observations persist with existing binding/projection checkpoints. The
small per-runtime `continuity-<runtime>.json` holds evaluated counts, cooldown and
the pending producer message; it contains no task, authorization or delivery
outcome ledger. Bad storage disables this optional feature, preserving chat and
original bytes. The existing diagnostic logger emits `continuity` invitation
evaluated score terms/availability once per fresh eligible completion, invitation
ready/accepted/rejected records with the original ID,
and one storage warning. No prompts, Notebook text or user content are logged.

## Reproduction and evaluation

Run the deterministic clock/accumulation/cooldown/deferral/missing/replay tests:

```sh
source script/env.sh
go test -race ./internal/backend/continuity ./internal/bot ./internal/backend/codex ./internal/backend/caelis -run 'Test(Confidence|InputTrend|Observation|Continuity|System|Usage|LiveUsage)' -count=1
CAELIS_BOT_TEST_CODEX=/path/to/codex go test ./internal/backend/codex -run '^TestNativeUnifiedSystemDelivery$' -count=1
CAELIS_BOT_TEST_BINARY=/path/to/caelis go test ./internal/backend/caelis -run '^TestNativeUnifiedSystemDelivery$' -count=1
```

Native fixtures use isolated real Runtime processes and synthetic local providers;
they verify transport, hidden input, quiet deferral, original bytes and duplicate
receipts, not real model judgment or production memory quality. Time/score inputs
are synthetic. No formal Bot rotation or live-account acceptance is implied.

After review/rollout, observe a few weeks via existing diagnostics and selected
Notebook review notes: invitation frequency, unavailable-signal rate among evaluated completions, useful
consolidation vs deferral, preserved goals/permissions/unknown receipts, and user
interruption. Compare sessions that stayed healthy and sessions actually reviewed;
record consolidation separately from Dream. A lower input count after renewal
is not proof of improved memory or lower cost. Tune these internal constants only
with such evidence; do not add knobs or make Dream the success metric.
