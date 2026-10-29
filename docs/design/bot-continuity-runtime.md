# Continuity implementation and context metric audit

Phase two is stacked on reviewed phase-one PR #254 at
`f51f029f64acd7d6d27b9e59bc2c9717a3d9d7bb`. It fixes continuity through the existing
Dream, handoff, native admission and first-input seed. It does not enable automatic
time/health cues. The [candidate policy](bot-continuity-renewal.md) remains a set of
testable hypotheses, including its numeric thresholds.

## What can actually be measured

Inspected versions: Codex 0.164.0, official source
`6aaac694803004a63c6f393d153c96d22521a6b9`; pinned Caelis 0.74.0,
`f69c186659efcf4c1b58ba73d390b69a4c3a06d1`. Source evidence and isolated native
observations below are separate from the user's formal-account investigation.

| Path | Available evidence | Sampling limit |
| --- | --- | --- |
| Codex App Server `thread/tokenUsage/updated` | Cumulative usage, `last` counters and configured window, scoped to thread/turn | `last` has mixed provenance. A local recomputation replaces it with an estimated total and zero input/output counters without increasing cumulative totals. The notification does not identify the sampling phase or provenance. |
| Bot Codex adapter | Accepts current resident turn's positive-input counters with increasing cumulative totals; invalidates the observation on compaction | This is a last accepted model observation, not a complete retained working set. Receive/native event time does not prove post-compaction measurement. Zero input is not billing or an established compacted baseline. |
| Caelis native provider events | Provider token/cost counters | Provider accounting does not itself emit a context gauge. Native acceptance with synthetic input/output 50/3 observed `awaiting_context_gauge`, with no window-bearing live gauge. |
| Caelis ACP ingress | Replaceable `ContextUsageSnapshot`, projected as `context_gauge` | The current Bot pairs a gauge with recent same-turn provider usage. Same turn and freshness do not establish the same response or post-compaction phase. |
| Caelis native diagnostic status | Reconstructs a token estimate from session events plus a static prefix estimate; exposes configured window | A lightweight status may know only the window. `includeDiagnostics` can estimate usage, but it lacks a comparable post-compaction sample identity. Repeated full-history diagnostic reads are not introduced into renewal admission. |

Primary source anchors:

- Codex [recompute_token_usage](https://github.com/openai/codex/blob/6aaac694803004a63c6f393d153c96d22521a6b9/codex-rs/core/src/session/mod.rs#L5223)
  zeroes counters while preserving cumulative usage; [history accounting](https://github.com/openai/codex/blob/6aaac694803004a63c6f393d153c96d22521a6b9/codex-rs/core/src/context_manager/history.rs#L925)
  separately estimates local items after the last model response; [App Server projection](https://github.com/openai/codex/blob/6aaac694803004a63c6f393d153c96d22521a6b9/codex-rs/app-server/src/bespoke_event_handling.rs#L1602)
  forwards the counters without provenance.
- Caelis [event projection](https://github.com/caelis-labs/caelis/blob/f69c186659efcf4c1b58ba73d390b69a4c3a06d1/control/appserver/projection/gateway.go),
  [usage types](https://github.com/caelis-labs/caelis/blob/f69c186659efcf4c1b58ba73d390b69a4c3a06d1/agent-sdk/session/usage.go),
  [status adapter](https://github.com/caelis-labs/caelis/blob/f69c186659efcf4c1b58ba73d390b69a4c3a06d1/app/gatewayapp/controladapter/adapter_status.go)
  and [diagnostic estimator](https://github.com/caelis-labs/caelis/blob/f69c186659efcf4c1b58ba73d390b69a4c3a06d1/app/gatewayapp/session_service.go).

Missing inputs are a comparable initial baseline under the same configuration,
provenance/phase of retained or post-compaction samples, and evidence that carried
content is stale. INDEX dates identify candidate notes, not stale model content or
task completion. Neither 64k nor an age threshold resolves those gaps. No bounded
sample store or numeric cue is added before its inputs have useful semantics.

Current scope: ordinary model-led review and proactive Dream continue. No idle
LLM turn or automatic numeric/time cue is dispatched. A month-old stable 20k
baseline and a month-old 90k baseline in a 1M window are both automatically silent
with today's incomplete evidence. The future policy must distinguish the latter
when sustained, comparable measurements and obsolete carried work are established,
without requiring near-exhaustion. That contrast remains unverified product policy.

## Minimal continuity changes

1. Read current core memory before accepting terminal Dream. Save its digest with
   the existing handoff; reject invalid reads and detect edits/deletion during
   preparation. Each adapter checks again before creation and binding commit.
   Memory reads reject concurrent replacement/change. A deliberately absent file
   remains absent; no history copy is resurrected.
2. Register human input, answers, decisions and Stop before dispatch locks. Reserve
   their priority over reports and mark an uncommitted handoff superseded. Retain
   the current native turn target so a correction already waiting when Dream is
   captured also fences that old turn. The saved marker survives restart. Native
   source/turn checks remain authoritative; no Worker/event revision invalidates
   the handoff.
   Agent-side Worker decisions use the same native decision route without
   registering human ingress; target/choice validation stays with the existing
   Worker interaction owner. Direct human decisions still reserve priority.
3. Reuse owned task, interaction, arrangement and operation stores. Caelis renewal
   checks resident admission instead of requiring every Worker to be idle. Its
   existing admission lock serializes grant rebind/revocation, retaining original
   authorization source and operation. Unknown resident or unowned effects still
   block renewal. No full-history pagination or global quiet period is required.
4. Keep original creation receipts. A known unused destination cannot later be
   adopted by the superseded invocation. For an unknown empty creation, keep old
   conversation available and prevent another create under a different ID. Caelis
   reconciles the original operation and never auto-adopts a late result; Codex
   retains its attempted native request. Unknown is never converted to success.
5. Keep the existing seed admission. First input receives current MEMORY plus the
   private handoff; the frozen original input survives uncertainty, and durable
   acceptance precedes consuming its digest. Required-read failure does not become
   an empty successfully injected seed. Failure before commit retains the old
   binding; failure after commit but before first dispatch retains the same new
   binding and unconsumed handoff until repair. There is no atomic transaction with
   arbitrary external file writers after the final admission check.

New persisted fields are only optional supersession/memory-digest fields on the
handoff and deferral/original-destination receipt fields in existing native journals.
No new model tool, approval protocol, preparation state machine or task ledger.
Legacy completed handoff migration also checks required memory; the retired Dream
scheduler is not restored.

Bot guidance now breaks review into resumable steps: read relevant current core
and recent INDEX entries, inspect original pending records, update core and linked
notes, incorporate human corrections, then write a bounded handoff. Tasks, stable
preferences, authorization limits/revocations, pending decisions and uncertain
operations retain original identities. Summaries never answer questions or grant
authority. Semantic completeness of model-written memory still requires model
evaluation; these tests prove transport/admission, not judgment quality.

## Verification and remaining evidence

| Regression | Evidence |
| --- | --- |
| Unreadable/edited/deleted memory during preparation | Host fixtures keep the source; app callback rejects invalid UTF-8 instead of swallowing the error; repair allows seed preparation. |
| Human already queued, correction during creation, Stop across restart | Host fixtures fence only the captured old turn, retain the original summary receipt and admit human work before queued reports. |
| Continuing Worker arrivals | Host fixture delivers queued original report/question IDs once after renewal; Caelis fixture retains owned active approval and unknown Worker operation unchanged, with the new resident available. |
| Rebind/revoke race | Caelis fixture checks current target admission and original authorization source/operation; no added authority revision. |
| Rejected/unknown creation and known unused destination | Adapter fixtures retain original IDs across reopen, preserve the source and prohibit replay/adoption; late Caelis operation result stays unused. |
| Seed once and uncertainty | Both installed native runtimes renew through real Bot tools and accept two later user inputs with one core/handoff injection. Shared seed fixtures freeze unknown input, retry reads after repair and consume only after durable acceptance. |
| Context metrics | Codex fixture rejects local re-estimates as fresh model/baseline samples; pinned native Caelis synthetic-provider acceptance observes counters without a gauge. Source audit supplies the sampling interpretation. |

Native commands (isolated synthetic model, no live account):

```sh
source script/env.sh
CAELIS_BOT_TEST_CODEX=/Users/xueyongzhi/.local/bin/codex go test -count=1 -v -timeout 4m -run '^TestNativeBotDreamRenewal$' ./internal/backend/codex
CAELIS_BOT_TEST_BINARY="$PWD/.cache/native-runtime/caelis" go test -count=1 -v -timeout 4m -run '^TestNativeHostBotDreamRenewal$' ./internal/backend/caelis
```

Required checks remain `make check`, `make smoke` and an ad-hoc `make build`;
targeted race checks accompany the regressions. Exact final-head CI belongs to the
phase-two PR. Native transport acceptance is not GUI, formal-profile, real-model
memory-quality or distribution acceptance.

The prior full-suite HTTP500 remains a separate unresolved investigation. Original
failure: `TestNativeHostIntegration/B00_reviewed_plugin_activation`, first plugin
disable, HTTP500/internal. Original log SHA-256:
`ff57ce6fcd3f3b885b0cbc5f61dfa878a30577fea083e3f19c2bd7a06ad6aeeb`.
The 6,356-byte prefix was identical between `d22c7696` and `f51f029f`, SHA-256
`0b084d2f11eb0df76f1a845aca0fdf70c81071881ee0e3a34cb093e2b3d2051b`.
The original temporary native store and internal cause are unavailable. Prior
passing comparisons neither identify a cause nor close it. This phase does not
change that integration prefix or expand stress reruns. The preserved local
`.cache/continuity-triage/evidence.json` records the original logs, comparison
commands, overlays and hashes; further investigation needs diagnostic evidence
from a recurrence.
