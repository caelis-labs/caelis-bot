# Bot continuity: memory review and meaningful session renewal

Status: phase-two proposal, not implemented. Review phase one before implementation.
Use a soft cue to invite useful consolidation; it neither authorizes discarding
context nor proves that renewal is appropriate. Keep a healthy Session. A large
window does not make a persistently expensive, stale working set healthy.

## Current evidence and missing measurements

The inspected Bot baseline is `d22c7696800d54d411ae231d7689e31839b6bf79`, including
approval snapshot ordering PR #253. These are source findings, not formal-account
acceptance. The user's time/health proposal supersedes the earlier model-only
direction for future work; [current product behavior](../product.md#连续性与主动工作)
stays unchanged until implementation is reviewed.

| Evidence | Meaning and limit |
| --- | --- |
| Formal Codex Session dates from September 19; no observed Dream/handoff; post-compaction records mostly 80–96k, latest about 119k | User-provided investigation, not re-read here. These records motivate measuring retained context; they do not establish billable cost, a trustworthy live gauge or a trigger. `input_tokens=0` is not a billing total. |
| [`Vault.PrepareContext`](../../internal/notebook/context.go), [`contextseed`](../../internal/backend/contextseed/seed.go), adapter `context.go` | Readable `MEMORY.md` at the Bot Notebook root is supplied at the first input of each new Session. The seed is frozen for its original input; acceptance is saved before handoff consumption. It is not reloaded every turn. |
| [`app.go`](../../internal/app/app.go) context callback | A memory-read error currently permits an empty seed. Missing files are allowed; nonregular, invalid UTF-8 and over-128-KiB files fail. Strict readable-memory admission for planned renewal is a gap. |
| [`Vault.Refresh`](../../internal/notebook/vault.go) | INDEX generates Markdown path/title navigation, descending by path; dated paths form a timeline. It contains neither note bodies nor completion status, and is not injected. |
| [`api/context.go`](../../internal/backend/api/context.go), [Codex usage](../../internal/backend/codex/usage.go), [Caelis usage](../../internal/backend/caelis/usage.go) | Current live usage lacks a persisted comparable baseline series. Codex rejects zero input counters and invalidates its gauge on compaction; Caelis requires a fresh same-turn gauge/provider pair. Billing totals, transcript record counts and token drops are not retained-working-set measurements. |
| [`dream_tool.go`](../../internal/bot/dream_tool.go), [`handoff.go`](../../internal/bot/handoff.go), [Codex renewal](../../internal/backend/codex/context.go), [Caelis renewal](../../internal/backend/caelis/context.go) | Existing `bot_dream({handoff})` saves at most 16 KiB under the original invocation, ends that turn, journals native creation and checks the old binding again before switching. Rejected/unknown creation retains original receipts; no replacement request ID. |
| [`tasks.Manager`](../../internal/tasks/manager.go), [interactions](interaction-lifecycle-followup.md), [arrangements](../architecture.md#registered-event-arrangements) | Work, questions, occurrences and delivery receipts already have durable owners. They do not need to be copied into a new continuity ledger. |
| Caelis retired-tool collection | User-provided Core fact: only marked-retired tools are reclaimed at a safe checkpoint. No schema eviction or reset of active tools is proposed here. |

The native fixtures in phase one use synthetic providers. They verify loading,
transport and isolation, not model judgment, memory quality or formal usage.

## Candidate inputs and testable policy

Measure retained context separately from window pressure and money. Collect only
observations already produced by ordinary resident activity; do not create idle
LLM turns or throwaway Sessions to measure a baseline.

| Input | Proposed interpretation and unavailable case |
| --- | --- |
| Real time | Native creation time if proven, otherwise first observation as an age lower bound. Record sample times and last review. File mtime, upgrade and reconnect are not Session birth. Clock rollback cannot invent elapsed time. |
| Comparable configuration | Provider/model/version where exposed, context-window semantics, resident instructions, initial tool/skill assembly and memory/seed digest. A material configuration change starts a new series; do not compare unlike tokenization or injection. |
| Initial baseline `B0` | First few small ordinary requests after normal new-Session seed injection, before substantial task material. Record only if the native gauge and request phase are known. An old Session without these observations has unknown `B0`; never reconstruct it from the latest low sample. |
| Sustained working-set floor `B` | A lower-envelope summary of valid resident request-context gauges over multiple ordinary turns/days. Large transient inputs are not a floor. Keep sample identity, phase and provenance; this is a context-size proxy, not dollars or semantic necessity. |
| Post-compaction baseline | First valid ordinary request gauges after identified native compactions, compared across compactions/configurations. Require the native event and a valid later gauge; neither a token drop nor a restored transcript counter proves this measurement. It supports `B` but two compactions are not mandatory. |
| Relative retained growth | `B - B0` and `B / B0` only for comparable, positive, known baselines. Absolute `B` remains useful when `B0` is missing. Window utilization is supplementary, never a required high threshold. |
| Work becoming stale | Verified completed/cancelled/superseded handles still referenced in the current working context or a bounded recent review, plus time since they ceased to matter. Task-ledger timestamps alone cannot prove their text remains in context. “Old” is not permission to delete it. |

The public protocols must first be checked for a reliable request-context gauge
independent of billable counters. When `input_tokens=0`, missing phase information
or absent native compaction evidence prevents a measurement, mark that input
unavailable. Do not scrape Runtime Memory or claim a semantic working-set metric
from aggregate counts. Context-to-task linkage is also not implemented: bounded
Bot review can identify examples, but no current host counter proves stale content.

Initial hypotheses for replayable fixtures, not tuned production thresholds:

```text
comparable := current native binding/configuration and a valid recent gauge
              (for example, at most 15 minutes old)
sustained := at least 4 comparable floor observations spanning at least 2 days
material_baseline := B >= 64k context tokens
                     OR (known B0 and B >= 2 * B0 and B - B0 >= 16k)
staleness_evidence := several verified obsolete work references
                     (example: 3 handles, obsolete for at least 7 days)
long_plateau := the material baseline is observed for at least 14 days
candidate := age_lower_bound >= 7 days and comparable and sustained
             and material_baseline and (staleness_evidence or long_plateau)
```

These values must be tested against real provider semantics before implementation.
Use a robust lower envelope/median across distinct observation windows, not the
minimum of one anomalous counter. Post-compaction observations strengthen the
baseline evidence without requiring near-exhaustion. Missing `B0` disables only
the relative branch; a missing trustworthy `B` disables numerical candidacy.
Known recent obsolete work may still inform normal model-led review, clearly
without a measured-cost claim. Explicit user review and model-led Dream remain
available independently of this policy.

At the next ordinary admitted input/report, a candidate can receive one private
cue describing observed age, baseline, comparison and known obsolete references.
Use an initial seven-day cooldown for unchanged evidence; persist the cue's
original admission ID. The Bot may defer because the material is still useful.
A cue does not wake an idle model, certify safe deletion or force Dream. Native
context-exhaustion recovery remains separate from this housekeeping invitation.

## Minimal loop using existing capabilities

1. **Review a bounded slice.** Follow current goal/open-work links in MEMORY, recent
   dated INDEX entries and the last review note. Read relevant note bodies and
   the original records for the work actually being considered. Record where to
   resume if the slice exceeds the current turn's budget. Do not scan every page
   of task history or require proof that no older work exists.
2. **Consolidate incrementally.** Use normal file tools to update notes and core
   memory, preserving user edits. Keep identity/personality, stable preferences,
   important agreements and their scope/revocations, current goals and durable
   links. Move detailed chronology to notes; remove stale or duplicate claims.
   Read back writes. No target word count or quota justifies dropping key facts.
3. **Handoff when useful.** Finish due foreground delivery and call the existing
   `bot_dream` with a bounded summary: current goal/constraints, essential pending
   original handles and unknown receipts, review-note path, and pointers to the
   owners of queued results/questions. State any unreviewed area rather than
   inventing completeness. No `prepare`, `validate`, `discard` or model commit token.
4. **Resume through existing seed/delivery.** Preserve the native renewal journal,
   binding checks and first-input seed. The new Session reads current memory and
   the handoff, then receives pending reports through their original queues and
   reads original records when needed. It does not reconstruct or replay effects.

Review progress belongs in an ordinary dated note. The core file is `MEMORY.md`
relative to the Bot Notebook root, distinct from Runtime Memory. Its useful length
depends on material facts; the existing 128-KiB read ceiling is a safety limit,
not a target. Respect deliberate deletion; do not resurrect a previous copy.
Unreadable required memory should prevent a planned switch while leaving the old
Session usable. After the first new input is dispatched, keep its frozen original
seed on uncertainty rather than rewriting and resending that input.

## Admission without handoff starvation

Separate changes that affect the meaning/authority of the handoff from ordinary
updates already safely retained by their owners:

| Change during review/renewal | Treatment |
| --- | --- |
| New human instruction, correction, answer, Stop or revocation | Admit as high priority. Re-read the affected scope/goal. A relevant change after final handoff capture invalidates that capture; continue the old Session or reconcile the already committed new binding. |
| Change to resident authorization, original resident approval target, runtime/model assembly or required memory | Check the affected existing owner at final admission. Do not copy stale grants or ignore a user file edit. Pending resident effects/approvals that cannot survive rebinding block that switch. |
| Normal Worker result, progress update, question, schedule occurrence or notification retained in a durable queue | Keep the original ID, execution owner and delivery receipt. It does not invalidate the handoff merely by arriving. Let the new Session read it through existing delivery. Independent Workers keep running. |
| A report already admitted as a new resident turn | Existing turn/binding checks apply; do not replace an active resident turn. At the next terminal boundary, give an elected handoff a bounded admission opportunity before another ordinary queued report. Human input keeps priority. |
| Unknown resident effect or unconfirmed renewal | Reconcile the original receipt; do not use renewal to clear uncertainty. Independent unknown Worker effects retain their original owner and can be referenced without a global stop. |

Do not require all queues to be empty, all Worker revisions to match or all old
notes to be read. Pending delivery belongs to existing queues, not to a frozen
handoff snapshot. Owner-provided stable pending handles/cursors can aid resumption;
when no stable cursor exists, use existing pending delivery and exact handles,
not an invented offset checkpoint. This review does not mark messages delivered,
finish tasks, close questions or grant authority.

Reuse resident admission and original-turn checks. If a narrow extra fence is
needed, track only human ingress/current-goal and authorization changes relevant
to the resident handoff, not every task/event revision. Register human ingress
before it waits for resident dispatch. Check again before adopting the new binding;
keep input queued under its original identity throughout native creation. Do not
hold an ingress/UI lock across network or file I/O. Queue ownership continues
while only resident report dispatch waits for the short renewal opportunity.

Caelis currently captures grants before creation and later rechecks resident
state; final grant/revocation comparison is a specific gap to close. Coordinate
that read-only authority snapshot with the approval owner. It is not a new model
approval store. A stale candidate stays unused; never restore a revoked grant.

## New state only where existing records are insufficient

- A bounded provider/configuration observation record: first-observed time,
  optional initial/post-compaction baselines, at most 32 samples and the last cue's
  original receipt/cooldown. Unknown fields remain explicit. No retired scheduler.
- If absent in existing ingress/admission records, a narrow resident human/authority
  generation captured in the existing renewal journal, plus required-memory digest
  checks. Prove the need per adapter; do not add a universal continuity revision.
- No preparation transaction, parallel task ledger, full-history coverage manifest,
  new tool schema or new workflow engine. Use the existing private handoff record
  and Notebook note for the handoff and review progress respectively.

Failed review keeps the old Session and previous successful file writes. A failed
handoff save must not end its turn. Rejected renewal uses the old binding; unknown
creation retains its original attempt and only falls back when that binding is
authoritatively usable. After a committed switch, resume the same new binding and
original first-input receipt. Never run both bindings or repeat unknown creation.
These paths reuse existing recovery; missing guarantees require focused fixes,
not an independent preparation state machine.

## Acceptance to implement and verify

All cases below are future requirements, not phase-one results. Synthetic numbers
exercise policy hypotheses; real-model and formal-profile evidence stay separate.

| Scenario | Observable result |
| --- | --- |
| Month-old stable low floor: `B0=18k`, `B=20k`, 1M window, no obsolete carried work | No cue or renewal from age alone, including reconnect/upgrade. |
| Month-old sustained high floor: `B=90k` for 14 days after compactions, 1M window, `B0` unknown | A soft review candidate despite 9% utilization. No automatic discard or renewal. |
| Same configuration grows from `B0=12k` to `B=36k`, with verified stale handles | Relative branch can invite review below the absolute threshold; still respect time/sustained evidence. |
| One large attachment, model/window change, zero/missing usage or unproven compaction | No invented floor, initial baseline, cost or compaction; retain unavailable evidence and normal model/user review. |
| High baseline contains useful active work | Bot defers; original Session continues, cooldown suppresses repeated invitations. |
| Recent note plus older open-work link; much unrelated history remains unread | Relevant commitments survive; renewal does not require full-history scanning or a claim of exhaustive review. |
| Continuous Worker completions and scheduled arrivals throughout review/switch | Review can finish; original queues retain all records and deliver once through the resulting binding. No global quiet-period requirement. |
| Human correction/revocation during final capture, native creation or first-input admission | Original input retained once; current goal/authority wins; stale grants cannot be copied back. |
| Pending Worker question or newer execution after an older completion | Original routing/generation survives; renewal does not answer, close, finish or restart it. |
| Core memory stale, tiny, oversized, deleted or concurrently edited | Preserve important facts/user edits; no padding/truncation/resurrection; unreadable required seed blocks only the planned switch. |
| Unknown delegation/send, rejected creation, crash at each existing journal boundary | Preserve original IDs, one authoritative binding and seed acceptance semantics; no replay or duplicate callback. |
| Successful renewal on both Runtimes | Stable Bot identity and Worker owners; new native Session gets current core memory and handoff once, then pending records from their existing owners. |

Start with pure-clock/configuration-baseline fixtures and continuous-queue/human-
priority admission tests. Follow with both native Runtime fixtures, then isolated
real-model review quality and deferral tests. Formal-account rotation and native
GUI acceptance are separate; neither is performed by this proposal.
