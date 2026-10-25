# Async question lifecycle

Decision record, 2026-10-18. Base: bb00cd465a8a756eb5b4274b6bcb6a7a5abbaf42.

## Authority and time

- This policy applies only to completed `request_user_input_async` items. Native
  approvals, elicitation forms, secrets and permissions retain native decisions.
- An unanswered async question is valid for 24 hours from first observation.
  Worker questions carry the original adapter's fixed deadline through peer
  observations into the Host card. Delayed first import takes that deadline,
  never another fresh window; legacy questions without it keep their saved expiry.
  A persisted deadline never extends on reopen, replay, reconnect or restart.
  Legacy undated questions receive one 24-hour migration window.
- Persist pending, dispatching, answered, unknown, dismissed, expired and
  superseded outcomes. `answered` means accepted input, not model consumption.
  Unknown/dispatching answers retain their original receipt and never expire into
  permission or allow a second answer. Legacy `accepted` maps to `answered`.
- Dismiss is an explicit durable close, independent of collapsing a panel. Dismissed/expired
  cards lose their decision controls and retain their closure reason. Answered
  cards disappear from chat; their durable terminal records remain unchanged.
  Expiry is checked without a Runtime event, including while disconnected.
- Explicit Worker task termination, failure, cancellation
  or an explicit stop invalidates unanswered questions. A completed native turn
  which emitted the question is a yield for input, even if it also contains
  commentary; its prose is not proof the user's task ended. It keeps the bounded
  window. Later completed turns also keep unanswered fields: neither commentary
  nor final-answer prose is a task termination contract.
  A changed original item/schema supersedes its old generation. Changing the resident native owner supersedes its old questions.
  Replacing a task requires explicitly stopping the old task before
  delegating its replacement; the original stop/terminal fact closes its questions.
  An ordinary follow-up, a new task or missing bounded history is not proof of
  replacement. Answering one field does not expire its unanswered sibling fields.
- Closed generations remain tombstoned even after their Q number is reused.
  Every desktop action carries its immutable generation, never only a Q number.

## Delivery and blocked work

- Closing supplies no answer, default option, permission or new work request.
  Required information remains missing. The Bot and Worker must keep dependent
  steps blocked and name the missing input; independent authorized work may proceed.
- Closing updates the local interaction ledger only. Do not notify or steer the
  Worker, retain closure context for a later input, or modify a submitted prompt.
  The retired peer closure operation acknowledges without effects. Legacy
  `questionClosures` input context is ignored and omitted on the next normal save.
- Host-validated original question answer delivery targets the existing Worker owner independently of the resident
  Bot's turn. New task creation retains its separate native activation check.
  Rejected input remains answerable; unknown input keeps its original receipt.
- The Bot can present the original Worker question to the user using its original
  handle. This provides source-task context and one shared answer/close state.
  Do not create another resident async question to paraphrase the same request.
- Natural-language replies cannot be matched by prose. Historical unbound duplicate
  questions are not guessed answered; they can be dismissed and expire normally.

## Presentation

The question itself appears directly in the conversation, with no second summary,
collapsed tray, accordion or internal scroll area. Show a complete question naming
its object, at most one necessary context sentence, distinct short options and an
always-visible compact free-answer input. Source task and deadline share one muted
line; a single explicit Dismiss action has no explanatory footer. Native permission
approvals retain their separate control surface. Closing a window is not Dismiss.

Conciseness is authored at question generation. `bot_interactions present` shows
an existing original Worker question. Optional presentation only partitions the
original question text into title and following context; it must preserve the
complete original text and exact option strings in order. Aliases/paraphrases are
rejected. Displayed option text is exactly the native answer sent. Invalid legacy
presentations are ignored on projection and removed from the live ledger on open.
Long originals remain fully visible, with normal background typography and
full-width left-aligned options, never truncated or hidden. New source questions
must name their object and be concise. Tab, arrows, Enter/Space, visible focus and
narrow windows must work. Status resolves from the durable ledger at read time.

### Answered-card display correction

After accepted answer delivery, remove that field from the conversation immediately.
When every field in a question item is answered, remove the entire synthetic row
before timeline construction, including its empty bubble, avatar and timestamp.
Keep unanswered siblings and ordinary answer messages unchanged. Do not synthesize
an answer message or duplicate an existing one. The same projection applies to
refreshed/restarted history, including legacy `accepted` states. Persistence, reply
IDs, original bindings and replay tombstones remain authoritative and intact.
Unknown delivery still requires reconciliation; it is not an answered state.

## Evidence requirements

Regression coverage: original answer identity, cross-entry duplicate, dismiss vs
answer race, deadline/offline/restart, stale history and recycled Q ID, terminal
Worker, supersession, save failure and no native permission dispatch. Native UI
acceptance uses an independent fixture/Dev bundle through build_and_run.sh. Synthetic
native evidence is separate from real-model and published-app acceptance.

## PR195 review corrections (before implementation)

- Presentation is lossless layout only: options must equal the original values,
  byte for byte and in order. Title/context may only partition the original title
  at an existing boundary, without deletion, substitution or reordering. Persisted
  aliases from the prior implementation are discarded at projection/migration.
  The original complete conditions remain visible. A model-authored summary is
  not evidence of semantic equivalence. New questions must be concise at source.
- A completed Codex turn (including a `final_answer`) is not a task-end signal.
  `Task.Result` is presentation prose and must never close unanswered siblings.
  Only explicit task end/stop/failure or the fixed deadline invalidates
  unanswered fields. Adapters without an explicit task-completion contract retain
  the bounded question rather than guess that the task ended.
- Persist retired display fingerprints separately from answer outcomes. Retiring
  an unknown/dispatching generation preserves its exact receipt/state; it cannot
  become active again, even after restart or replay. Only unseen schemas advance
  the current display generation.
- Use the reviewer's exact legacy title/options and two-question scene at a
  340x560 whole window (340x528 content), not just a short fixture. Full-width
  question rows, restrained type, left-aligned full option text, and earliest
  pending-question reading position keep the source/deadline/close accessible.
  No accordion, semantic truncation, or internal question scrollbar.
- Smoke evidence must distinguish the schema baseline from the actual initialized
  runtime version. The independent reviewer used Codex 0.162.1; 0.153.4 was only
  the pinned schema baseline, not that runtime's tested version.

## Runtime recovery boundary (2026-10-20)

A turn interrupted by a connection/Runtime event does not by itself cancel the
user's task. `interrupted` and legacy `unavailable` do not expire its questions.
Explicit stop/cancel, native task-end facts and the existing deadline still do.
Legacy question tombstones are retained: removing Worker retirement never
reopens an already dismissed/expired/answered question or grants missing approval.
