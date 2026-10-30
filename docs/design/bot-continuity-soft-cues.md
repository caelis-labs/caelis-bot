# Lightweight memory-review invitations

Based on main `882853513875316dc6abd242ea7a32ae397c88fd` and #258 system delivery.
Replaces #256's stacked, multi-window implementation. Dream handoff stability
belongs to #262; this producer never calls Dream or creates a native session.
The sole model workflow is [bot-core memory guidance](../../internal/botskills/skills/bot-core/references/memory.md).

## Signals actually available

| Signal | Acquisition and missing handling | Meaning and limits |
| --- | --- | --- |
| Elapsed time | First live resident turn observation, persisted with the binding. Future timestamps/clock rollback add no age. | Age **lower bound**, not native birth or file mtime. No history scan. |
| Completed turns | Exact live resident start + successful completion; one vote per native turn. Interrupted/failed turns, historical projection, review/child and maintenance invitations and legacy host notices without known business provenance excluded. | Observed count, not the total session history or amount of obsolete work. Restart during a turn may miss one observation. |
| Compactions | Codex completed `contextCompaction` items; Caelis typed successful `compact` notices, deduplicated by native identity. Count only within an observed completed ordinary turn. | Observed lower bound. No notice is not proof that the Runtime never compacted. Token drops and `compact_failed` never count. |
| Ordinary input | Up to 8 positive per-turn minimum inputs, same known model/provider/assembly. Codex `last.inputTokens` with increasing native totals; Caelis `usage_update._meta.caelis.sdk.usage` with explicit cache-read accounting. Separate cache reads are added once; cache writes are already included. | Request-input proxy, **not** retained-context waterline, stale content or money. Missing accounting/model/positive input makes this turn unavailable; no zero sample. Compaction/review turns excluded. Inputs are diagnostic only and never a score or decision term. |
| Staleness/cost/benefit of renewal | Not measured. | The model must judge useful content and current user intent. A large input can be entirely useful. |

The eight samples compare observed ordinary inputs, not an initial-session or
post-compaction baseline. Known model/provider/assembly changes reset the samples.
Unexposed provider/tool changes remain a limitation; this signal never decides
whether content can be deleted. No new sampling requests, timer model calls,
user settings, history indexing or semantic-obsolescence framework are added.

## Signal review and minimal invitation policy

| Signal | Why inspect? | Limit / overlap | Decision |
| --- | --- | --- | --- |
| Live-observed age lower bound | Work has spanned time and possibly changed goals. | Age is not staleness; overlaps with activity as exposure. | Joint pacing gate with completed business turns, never an independent vote. |
| Successful resident business turns | Actual work has accumulated, including Worker/reminder/callback wakeups. | Neither amount nor source proves obsolete contents. | Joint work-span gate; exclude maintenance, review, child, failed and replayed turns. |
| Successful compact | The runtime performed maintenance. | Usually useful recovery, not damage. Multiple events in one turn are correlated. | One recent-maintenance observation per completed business turn; only a recurring recent burst can invite inspection. A later successful non-compacting business turn removes that candidate. |
| Positive request input tokens | Auditable provider request-volume observation. | Not retained context, cost, stale content or cross-harness capacity. High stable use can be healthy; first/last-four comparisons have arbitrary scale and selection. | Keep bounded, comparable sampling for diagnosis; remove all magnitude and growth votes, including 16k/32k/64k and 2x. |
| Trusted context capacity | Could normalize occupancy if request and capacity bases matched. | Not consistently available with comparable semantics in both adapters; occupancy is not semantic health. | Do not invent or add a pressure vote. |
| Source identity | Distinguishes genuine resident work from the invitation itself. | Old HostNotice has no authoritative business source. | Persist existing SystemSource in native delivery records; whitelist known business sources, never infer from prose. |

Minimal initial invitation policy: at least 14 days observed and 64 successful
business turns since the last invitation, plus maintenance in at least 3 distinct
turns among the last 8 completed business turns, including the latest completed
turn. These are conservative invitation pacing/pattern choices, not calibrated
health thresholds. A single compact, stable high request volume, age alone, or
busy operation alone cannot invite. Missing input stays unavailable and cannot
contribute a fabricated value. Normal completion after the latest compact stops
the maintenance candidate; same-head admission also discards a prepared cue when
new work arrives. Existing seven-day persisted cooldown and original unknown
receipt reconciliation remain. The invitation itself does not count as business
work or maintenance evidence. No classifier, model idle call, context deletion,
new protocol or automatic Dream.

Residual: repeated successful compaction can be healthy for a particular workload.
This policy may invite review during that maintenance burst, never diagnose it;
the model can defer. Old high-volume sessions without recurring maintenance no
longer qualify from token level alone. Semantic obsolescence still requires the
model's judgment, and proactive model Dream remains available.

All three maintenance turns must be newer than the previous invitation and at or
after its cooldown expiry. Maintenance observed during cooldown cannot be reused
on the first later turn, even across restart. Up to eight timestamped maintenance
turn ordinals live in the existing observation. Multiple compactions in one turn
are one maintenance observation, regardless of the diagnostic event count.
A missing age lower bound cannot satisfy the work-span pacing gate; it is unknown,
not a fabricated zero-day session. Historical aggregate compaction counts without
these live turn identities/timestamps cannot establish a recent pattern.

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
Before attempting delivery, an expired or changed completed head limits the
unified entry to reconciliation. An unavailable delivery log cannot prove
non-dispatch and leaves the original candidate intact. A producer save failure
disables this optional feature until restart/readback; ordinary chat continues.

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
evaluated work-span and recent-maintenance availability once per fresh eligible completion, invitation
ready/accepted/rejected records with the original ID,
and one storage warning. No prompts, Notebook text or user content are logged.

## Reproduction and evaluation

Run the deterministic clock/accumulation/cooldown/deferral/missing/replay tests:

```sh
source script/env.sh
go test -race ./internal/backend/continuity ./internal/bot ./internal/backend/codex ./internal/backend/caelis -run 'Test(Invitation|InputTrend|StableHighInput|Observation|Continuity|System|Usage|LiveUsage)' -count=1
CAELIS_BOT_TEST_CODEX=/path/to/codex go test ./internal/backend/codex -run '^TestNativeUnifiedSystemDelivery$' -count=1
CAELIS_BOT_TEST_BINARY=/path/to/caelis go test ./internal/backend/caelis -run '^TestNativeUnifiedSystemDelivery$' -count=1
```

Native fixtures use isolated real Runtime processes and synthetic local providers;
they verify transport, hidden input, quiet deferral, original bytes and duplicate
receipts, not real model judgment or production memory quality. Time/maintenance observations
are synthetic. No formal Bot rotation or live-account acceptance is implied.

After review/rollout, observe a few weeks via existing diagnostics and selected
Notebook review notes: invitation frequency, unavailable-signal rate among evaluated completions, useful
consolidation vs deferral, preserved goals/permissions/unknown receipts, and user
interruption. Compare sessions that stayed healthy and sessions actually reviewed;
record consolidation separately from Dream. A lower input count after renewal
is not proof of improved memory or lower cost. Tune these internal constants only
with such evidence; do not add knobs or make Dream the success metric.

## Provenance in the original delivery record

Worker completion/failure, Worker approval/question, reminder, callback and
command-result sources are resident business wakeups. Their successful live
turns count just like user turns. `SystemSource` is copied only from the host
producer into the existing Codex pending/report-turn record or Caelis operation
journal. It is observation metadata, not a new admission or authorization source;
renderer input cannot set it and prompt prose is never parsed for provenance.
`continuity_review`, native review/child work, failed/interrupted turns and replay
remain excluded. Missing legacy source stays unknown. A completion that beats
reliable operation/turn correlation is conservatively skipped. Restart restores
source and original receipts together; no backfill scan or second source ledger.
Excluded/failed turns cannot reset the input series by announcing another model.

No new Bot skill protocol is needed: the existing memory reference already lets
the Bot keep working, postpone review, consolidate notes and decide independently
whether to Dream. Policy constants and signal limitations remain developer docs.

Native Core 0.75 verification found transient lifecycle envelopes have an exact
feed cursor but no event/projection ID. The observer accepts that cursor only on
caught-up exact deliveries, retains native Turn completion deduplication, and
never samples replacement/history pages. Synthetic fixtures now use that wire
shape. This is verified transport evidence, not model judgment.

Reopening an older settled Caelis journal also exposed `body:null` being mistaken
for retained input. The original load boundary normalizes null to absent bytes;
new writes omit absent bodies. Original digest/outcome/source remain intact and
same-ID readback performs no native POST. Unknown operations are not replayed.
