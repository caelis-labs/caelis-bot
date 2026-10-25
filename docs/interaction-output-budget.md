# Worker interaction output budget

Previously `bot_interactions list` serialized all eligible approvals and Worker
questions, including terminal history. Once the combined JSON exceeded 31 KiB,
`compactValue` replaced it with `model_output_budget` and an empty data object.
The recommendation to narrow a read was unusable because this tool had no ID read
or pagination. Repeated allow-once requests can accumulate this history; they do
not grant a session permission.

The todo projection now excludes only explicit terminal states (resolved,
answered, expired, dismissed, superseded and completed/cancelled). Sent, sending,
dispatching and unknown remain visible with original identities. Native approval
eligibility, offered choices, authorization and decision/answer receipt handling
are unchanged. This does not authorize any currently unread or pending request.

`list` has bounded `limit`/`nextCursor` pages tied to the current snapshot. Ordinary
pending records retain all original fields. An individually large original has
an exact `kind`/`id` entry under `reads`; `read` returns the full original, or
lossless UTF-8 JSON chunks with offset, SHA-256 and nextCursor. All chunks must be
read before interpreting the original. Revision changes invalidate the cursor.
No pending permission text is summarized or truncated and the 31 KiB budget is
not raised. A retained record may still be read for receipt reconciliation; recycled short
question numbers are not a permanent historical-ID API.

The text-channel queue also removes redundant complete display payloads from
terminal closure-index copies at save/load when the same generation already has
its original in the current index. Minimal source/owner/request/fingerprint,
closure and receipt evidence remains. Short-number recycling first preserves the
one history projection needed by older cards. Unknown/dispatching payloads are
never compacted. Task/native history is untouched: this is duplicate cleanup,
not a second receipt ledger or deletion of necessary historical originals.

Regression coverage includes hundreds of terminal records, multiple pending
pages, lossless Unicode/choices, sent/unknown retention, changed-cursor rejection,
save/load cleanup, repeated native polling, short-ID reuse and historical cards.
The running installed Bot needs an upgrade/restart through the normal coordinated
flow to receive this fix. Building this PR does not patch a running instance.

Terminal display history is limited to 128 recycled generations for seven days;
older entries keep compact binding/outcome/receipt tombstones. Cached IM cards
retain their original display text and receive only lifecycle updates from a
compacted tombstone. Unknown and dispatching originals are not compacted.
Question closure never sends model input. See [the complete tool audit](bot-tool-lifecycle.md).
