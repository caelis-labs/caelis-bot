# Read the original desktop receipt

For transport uncertainty, partial/unknown input, failed restoration, a fenced
seat or oversized output, keep the original requestId/run_id and receipt. Call
`bot_desktop_result` with `request:{"type":"status","requestId":"ORIGINAL_ID"}`
or `request:{"type":"status","runId":"ORIGINAL_RUN"}`. Status never sends input
and can read after the turn ended. Do not resubmit using a new ID or switch
writers to repeat uncertain work. Helper failure does not authorize a restart.
Missing/expired receipts do not prove no effect.

To cancel pending steps in the active turn use
`request:{"type":"cancel","requestId":"STABLE_CANCEL_ID","runId":"ORIGINAL_RUN"}`.
Cancellation cannot retract dispatched events. Inspect each step's delivery and
verification, not only the plan outcome. A confirmed skipped suffix can be
planned only after fresh observation and valid authorization; never repeat an
uncertain prefix. Respect human focus changes and report cleanup uncertainty.

Text plus structured output is bounded to 32 KiB. `model_output_budget` retains
original request/run identity, outcome, seat health, cooperative input report
and bounded step delivery/verification facts when available. Detailed evidence
remains in the original host receipt. Reduce new read scope/fields/pages; never
repeat an action to recover its output. State the verified result and the
remaining limit plainly. Follow [Actions](desktop-actions.md) for a new known
plan and [Observation](desktop-observation.md) for fresh narrow evidence.
