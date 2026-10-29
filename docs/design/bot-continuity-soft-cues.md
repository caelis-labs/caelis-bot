# Incremental continuity invitations

This change is stacked on #255 (`b779063b`) and does not merge #254 or #255.
Codex gains private invitations; Caelis persists comparable samples for the same
policy, without evaluating or delivering invitations on submissions. Caelis prompts
remain strictly separate from internal context; delivery requires a suitable public
Runtime port. No Core change is included. The earlier HTTP500 remains independently
unresolved; successful checks here do not close it.

The policy invites the model to assess context usefulness. It neither proves
semantic staleness nor authorizes deletion, Dream, or any other effect. Ordinary
request input counts are not retained working-set size, post-compaction waterlines,
or billing cost. Missing original session baselines do not prevent new sampling.

## Initial internal policy

These are explicitly selected low-frequency heuristics, not calibrated cost limits.
There is no user-facing configuration.

| Parameter | Default |
| --- | --- |
| Session age or known observed-age lower bound | at least 7 days |
| Observation window | 24 hours, UTC |
| Sustained evidence | at least 4 populated windows, 8 independent turns, 48 actual hours |
| Absolute input level | window lower medians at least 64,000 tokens |
| Relative growth | at least twice the observation baseline and at least 16,000 tokens higher |
| Latest evidence | latest window and turn still qualify; last observation at most 24 hours old |
| Accepted invitation cooldown | 7 days; subsequent persistence evidence must arrive after cooldown |
| Storage | 28 days, at most 64 recent independent turns per window |

Each turn contributes the minimum valid ordinary-request input observed in that
turn; repeated steps never add turn votes, including turns crossing midnight.
Window medians use the lower middle value for an even sample. Busy windows retain
the most recent 64 turns, so statistics describe this bounded sample, not every
request. A low window resets the high-window streak. Empty windows are unknown.
The initial comparison baseline is the median of the first sampled windows
that together meet the window/turn/time requirement; it is not a historical session-start baseline.
Visible model changes start a new comparison segment, retaining age and cooldown.
Known cache accounting is normalized to full input; unknown accounting is skipped. Normal memory edits do not reset sampling.

## Runtime evidence

Codex: `Thread.createdAt` supplies age when available. Positive
`thread/tokenUsage/updated.last.inputTokens` is sampled in observed resident turns;
window size is not required. The cumulative total is only a per-turn/compaction
deduplication aid. Zero-input local recomputations, compact intervals, review mode,
children and historical projection are excluded. Compaction drops the in-progress
turn's earlier observations, then ordinary requests can qualify again. Receive
time is observation time, not a claimed request timestamp. Configured model is not
claimed as per-response routing telemetry. The internal-only `rawResponse/completed`
notification is not consumed.

Caelis: sample exact live resident `provider_usage` events through the existing
cursor/seen projection boundary. Native ordinary-response `usage_update._meta.caelis.sdk`
supplies model/provider and reported usage. Child/review envelopes, context gauges,
summary events lacking native response metadata, zero and unknown input are excluded.
For `included`, prompt tokens already include cache reads; for `separate`, add the
explicit reported cache-read count. Unknown accounting does not mix into comparable
samples. Do not subtract cache hits or double-count cache-write breakdowns.
This reuses public metadata; there is no history scan or diagnostic status poll.
The native wire test confirmed this metadata resides on the update; the envelope's
reduced `_meta` is insufficient. Cold cache writes and warm cache reads both yield
34,000 complete input tokens in the synthetic Anthropic fixture.

Caelis age starts with persisted first observation and is explicitly a lower bound;
the Runtime's stored creation time is not exposed by its current public session
schema. An optional later read-only `created_at` projection could improve this,
but it is not required to start sampling. Reconnect preserves observation history;
new native binding resets it. Missing/invalid measurements are not zero.

## Delivery and continuity

Codex uses the existing experimental `turn/start.additionalContext` entry with
`kind: application`; the model receives a separate developer context fragment.
The original native input array is unchanged by the cue. No cue is appended to a
steering request, first-input seed, or separate idle turn. Existing user/Worker
admission and human priority remain authoritative.

The cue is frozen under the original submission before dispatch. Accepted input
records cooldown; unknown input retains the original pending identity and follows
existing reconciliation without a new send. Definitive rejection releases it.
Reconnection and restart retain cooldown. The seed's Injected flag and digest
consumption are unchanged. No prepare/validate/discard tools or new task ledger.

The short invitation prioritizes current instructions and revocations, asks for
selective MEMORY/INDEX review, and permits continuing normal work when context is
useful. It does not require a visible maintenance message. Dream remains model
initiated and retains #255's original-operation, memory-read and handoff safeguards.

## Verification boundaries

Deterministic policy tests cover stable low/high sessions, observed baseline growth,
single spikes, long turns, bounded retention, accounting separation, compaction,
unknown receipts, restart and fresh evidence after cooldown. Adapter tests assert
separate application context and exact original prompt bytes.

Opt-in native synthetic tests passed with Codex 0.164.0 and a local Responses
fixture, and pinned Caelis 0.74.0 (`f69c1866`) with a local Anthropic fixture. They check native fields,
cache accounting and prompt boundaries without user accounts or external models.
Policy age is synthetic in these tests. A synthetic response continuing work proves
absence of automatic renewal, not that a real model makes good staleness decisions.
Real-model judgment, production calibration and Caelis cue delivery remain unverified.

Reproducible focused checks (use externally installed native binaries):

```sh
source script/env.sh
go test -race ./internal/backend/continuity ./internal/backend/codex ./internal/backend/caelis ./internal/backend/contextseed ./internal/bot -run 'Test(Candidate|Spike|PendingReceipt|BoundedRetention|Continuity|Context|Dream|Seed|Human|WorkerReport|Renewal|GrantRevocation|UnreadableSeed)' -count=1
CAELIS_BOT_TEST_CODEX=/path/to/codex CAELIS_BOT_TEST_BINARY=/path/to/caelis go test -v -count=1 -timeout 90s ./internal/backend/codex ./internal/backend/caelis -run '^TestNativeContinuity'
```

The native Caelis test checks unchanged dispatched request digests after the
original operation journal retires its body. The Codex test checks exact user text,
a separate developer fragment, no second injection on the next submission, and
continued native identity. Deterministic adapter tests cover both user and Worker
submissions, unknown receipt reconciliation, restart, steering and model changes.
The existing renewal/seed regressions remain intact; no integration prefix or Core
source is changed.
