# Dream handoff continuity

This change replaces the continuity portion of draft #255 (`b779063b`), originally
based on unified delivery #258 and now incorporating main
`882853513875316dc6abd242ea7a32ae397c88fd` (#263, Core v0.75.0). It does not
implement soft-cue sampling/thresholds or investigate the separate historical
plugin HTTP 500 and Telegram timeout. Those remain separately owned.

## Migration and simplification

Retain #255's required-memory checks, persisted human supersession, original
unused-create receipts, resident-only Caelis renewal admission and failure tests.
Use #258's `SubmitSystem` unchanged for Worker results, approvals, questions and
other system events; do not restore `SubmitReport` or a second delivery ledger.

Replace #255's independent priority atomics/check snapshots with one host mutex
for human registration and final adoption, plus the existing waiting-user counter.
Keep slow memory reads and native creation outside that commit gate. A host-only
commit callback makes the adapters adopt their prepared native binding and
publish the matching handoff before releasing native input admission. It is not a
new model-facing tool or prepare/validate/commit workflow.

Recheck terminal results under each adapter's native admission. A corrected
Dream becomes a nonterminal refusal before Codex `turn/interrupt` or Caelis
callback terminal publication. Accepted steering keeps its original Turn and
request; it is never replayed. Human ingress includes composer, remote and capture
submission, Stop, human decisions and question answers. Agent decisions/answers
and normal Worker observations do not reserve that priority.

## State and recovery

- `MEMORY.md` stays the Notebook's core identity/preferences/agreements. The
  bounded private handoff carries current goals, original task/operation IDs,
  pending decisions, authorization limits/revocations, retained resources, review
  artifact paths, reviewed/unread scope and next actions. Business records remain
  authoritative; the host does not invent or infer these semantic facts.
- Add only a superseded bit and captured memory digest to the existing private
  handoff. Keep unused native creation receipts in each adapter's existing
  binding/journal. No task-state copies or global continuity revision.
- Required memory must be readable and unchanged at pre-adoption checks. Failure
  retains the old binding. A missing fresh Notebook core remains a valid empty
  initial core; deletion of a previously captured core changes its digest and
  defers renewal.
- Existing Worker approvals, questions and uncertain Worker operations retain
  their owners and receipts. Resident pending authority/uncertain effects still
  block adoption. Normal results do not demand global quiescence or history scans.
- First native acceptance consumes the existing seed digest once, including when
  the first input is an approval/question. A renewal returning afterward cannot
  recreate the consumed handoff. Unknown input acceptance retains the original
  frozen seed and ID; no replay under a replacement ID.
- Native binding and private handoff are separate durable files. After a crash
  between their writes, reconcile the original native renewal receipt before the
  first input. A human input must not erase a handoff for an already committed
  binding. Unknown native creation stays at its original receipt; a known unused
  destination is retained without adoption or another create.

## Verification

Deterministic tests exercise production entry points and native protocol fixtures:

| Case | Evidence |
| --- | --- |
| Approval/question arrivals during renewal | App observers → resident `SubmitSystem`; repeated observations defer, then deliver once; original interactions remain pending. |
| First input immediately after native admission unlocks | Real Codex adapter, paused before renewal returns; system input gets handoff and core, consumes once, and releases the tool fence. |
| Correction before capture / accepted steering before termination | Backend submission → resident → real Codex wire adapter; nonterminal refusal and zero interrupts/replays. |
| Caelis callback after accepted steering | Real callback claim/result entry and HTTP wire fixture; nonterminal result, one original steering, identical repeated receipt. |
| Stop/correction after the last successful memory check | Both real adapters paused at the final check; Backend human ingress wins before adoption; original unused-create receipt retained. |
| Failure/restart | Required-memory failure/change/deletion, durable supersession, unknown native create, failed host receipt writes, crash after binding commit, and first-input seed acceptance/unknown paths. |
| Agent versus human Worker actions | App approval/question paths preserve native ownership; human actions invalidate old handoff, Agent responses do not. |

Native synthetic acceptance uses installed Codex and the pinned Caelis v0.75.0, isolated
stores, real MCP/callback protocols and local synthetic model endpoints. Both
Dream tests make a Worker approval/question the new session's first input and
check that the next user input does not reinject memory/handoff. Both existing
native unified-delivery tests also run unchanged. Logs and exact-head checks are
linked in the PR validation record.

The first Linux CI run exposed a pre-existing cancellation gap in Caelis
`ReadWork` (unchanged since #250): a successfully decoded HTTP read could publish
idle/finish proof after cancellation. A deterministic response-close cancellation
test reproduces it on the unchanged function. A separate follow-up commit checks
cancellation under the publication lock; it does not alter task ownership or
replay policy. Main #263 now owns that fence; this branch keeps its invalidation
of stale idle evidence and both deterministic cancellation orderings. Renewal
also retains main's `configureApproval`, global Team/Guardian and capability gate.
The failed run and red/green evidence remain in the PR record.

The Windows receipt-lifecycle failure did not retain its underlying save error;
its historical filesystem error or deadline cannot be inferred from a later pass.
The original save boundary now records deduplicated, path-free error category,
write stage, OS code and elapsed time while preserving the original error cause.
Main #263's `WriteError` is the sole error wrapper; the text-channel diagnostic
projects that chain instead of adding a second write wrapper or completion flag.
The two-second write wait and unknown-result policy are unchanged. Deterministic
post-answer deadline and filesystem failures verify restart recovery through the
original receipt, never another answer. The 257-answer lifecycle test now verifies
unknown identity before reconciling a proven post-answer deadline once; filesystem
errors still fail that lifecycle test. The same injected deadline at iteration 153
fails its previous immediate-settlement assertion and passes the revised test.
Diagnostic-only CI success is recorded separately from that causal test evidence.

Handoff files use `handoff_continuity` names to avoid add/add conflicts with the
independent soft-cue implementation in #261. No shared binding signature changes
or reminder implementation are part of this follow-up.

## Limits

Semantic completeness of model-written notes, authorization summaries and review
scope is guidance, not a host guarantee. No live model judgment or native GUI
acceptance is claimed by synthetic protocol tests. No cross-file transaction or
Core change is introduced. File validation is point-in-time: a later external
edit is read again at seed preparation, and a read failure blocks that input; it
cannot undo an already committed native binding. Legacy retired-scheduler receipt
migration remains compatibility code, not a new automatic Dream trigger.

## Review follow-up: terminal receipts and original create cleanup

Caelis `Calls` now fences new resident input from the first saved successful
terminal callback receipt until its original Turn is observed terminal. Receipt
publication and input admission share `step`; the cached presentation is fenced
before persistence can wait. `/result` acceptance, a lost response, and the
presence of input in history do not prove that Turn has consumed steering.
Recovery reuses identical result bytes. Unaccepted corrections keep their original
submission ID; native Stop remains available. No extra terminal-state journal is
introduced. Real Core synthetic tests inject failure both before acceptance and
after acceptance; deterministic fixtures also cover restart and repeated receipt.

The Host always asks the adapter to settle the original CallID, including a
superseded handoff. Each adapter consults its existing create receipt before
checking the current Turn. Known unused Codex destinations move to
`DeferredToolRenewals`; Caelis keeps its existing deferred operation record.
An unknown create keeps its original native receipt and archives the Host summary
as `unknown_fallback`, preserving ordinary old-session work. An unrelated later
Dream that cannot start because that receipt is unresolved is definitively
rejected, not mislabelled as another unknown create. A committed binding restores
its matching handoff. Other save/read errors do not let the Host discard pending
cleanup. No original create or accepted user input is replayed.

Codex has no public create lookup when the original thread/start response contains
no new thread ID. That uncertainty stays retained and prevents another renewal;
it does not block ordinary tools/chat. Caelis can read back its original operation.
These are different native capabilities, not an excuse to infer a missing ID.

The Codex native fixture now distinguishes an already-admitted sibling tool from
a tool admitted after handoff capture. Both provider orderings are exercised;
new calls after durable handoff are refused. The earlier zero-sibling assertion
could not establish this temporal distinction. Core terminal callbacks still
provide their native same-Turn terminal contract. These fixtures prescribe model
responses and do not establish autonomous model judgment or note completeness.
No additional Bot-facing workflow is needed for these adapter/Host repairs.
