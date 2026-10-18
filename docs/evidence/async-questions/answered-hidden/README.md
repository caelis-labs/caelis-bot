# Answered questions disappear from chat

This supersedes the answered status strip shown in the preceding
[review-fix keyboard capture](../review-fix/after-keyboard-answer.png).
The backend still persists `answered`, original bindings and reply IDs, including
closed-generation tombstones. Only the conversation projection changes.

- [Free answer: no remaining card, status, avatar or empty row](after-free-answer.png).
- [Long original Q1 answered: only pending Q2 remains](after-partial-answer.png).

Both captures use the production History renderer in the independent native
WKWebView preview, launched with `script/build_and_run.sh --chat-preview`, dark
appearance and a 340x560 whole window (340x528 captured content). The free-answer
scene reproduces “独立演示连接位于哪里？” from the feedback. The multiple-question
scene uses the reviewer's full original long title/options; selecting original
option 2 removes only Q1. No semantic alias, hidden context or accordion was added.
The normal assistant message retains its existing avatar.

[19 native checks](native-checks.json) cover immediate removal, persisted-snapshot
state retention, closing/reopening chat, untouched ordinary messages, pending
siblings, original long text, keyboard navigation and layout above the composer.
These are synthetic native renderer checks, not live-model or channel acceptance.

`make check` passed (237 Node tests, TypeScript/Vite and the tagged Go suite).
Focused tests preserve actual ordinary answer records unchanged, handle legacy
`accepted`, ignore stale refreshes after an accepted reply, retain attachments,
and distinguish recycled question identities. The Go restart regression reloads
a stale pending chat card from the durable answered ledger, verifies its original
receipt and archived tombstone, and rejects duplicate dispatch. `make smoke`
passed with actual Codex 0.162.1 (schema baseline 0.153.4), without model requests.

The complete isolated Dev App restart observation, exact source SHA, build hash
and terminal CI result are recorded in the PR handoff. No Bot skill change is
needed: tool contracts, answer delivery and lifecycle semantics are unchanged;
this is solely a chat display correction. No production or reviewer state changed.
