# Async question lifecycle

Decision record, 2026-10-18. Base: bb00cd465a8a756eb5b4274b6bcb6a7a5abbaf42.

## Authority and time

- This policy applies only to completed `request_user_input_async` items. Native
  approvals, elicitation forms, secrets and permissions retain native decisions.
- An unanswered async question is valid for 24 hours from first observation.
  A persisted deadline never extends on reopen, replay, reconnect or restart.
  Legacy undated questions receive one 24-hour migration window.
- Persist pending, dispatching, answered, unknown, dismissed, expired and
  superseded outcomes. `answered` means accepted input, not model consumption.
  Unknown/dispatching answers retain their original receipt and never expire into
  permission or allow a second answer. Legacy `accepted` maps to `answered`.
- Dismiss is an explicit durable close, independent of collapsing a panel. Closed
  cards lose their decision controls and remain as compact history with their reason.
  Expiry is checked without a Runtime event, including while disconnected.
- Confirmed Worker completion with a final result, failure, cancellation, retirement
  or an explicit stop invalidates unanswered questions. A completed native turn
  which emitted the question is a yield for input, even if it also contains
  commentary; its prose is not proof the user's task ended. It keeps the bounded
  window. A later completed turn with a final result closes remaining questions.
  A changed original item/schema supersedes its old generation. Changing the resident native owner supersedes its old questions.
  Replacing a task requires explicitly stopping/retiring the old task before
  delegating its replacement; the original stop/terminal fact closes its questions.
  An ordinary follow-up, a new task or missing bounded history is not proof of
  replacement. Answering one field does not expire its unanswered sibling fields.
- Closed generations remain tombstoned even after their Q number is reused.
  Every desktop action carries its immutable generation, never only a Q number.

## Delivery and blocked work

- Closing supplies no answer, default option, permission or new work request.
  Required information remains missing. The Bot and Worker must keep dependent
  steps blocked and name the missing input; independent authorized work may proceed.
- Notify the original Codex owner of closure as host context, never in a question
  answer envelope. Persist that context before delivery. An active original turn
  may receive one steer; an idle/finished task is not started to send a notice.
  The original owner also receives retained closure context on its next authorized
  input. Unknown notice delivery is not replayed as a new input.
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

Conciseness is authored at question generation. `bot_interactions present` also
accepts a structured display presentation for an existing Worker question. It must
preserve the original options' count, order, meanings, targets and scope; selecting
a label still submits the original native option, never the summary. Display
summaries are size-validated, durable and immutable after presentation. They confer
no execution authority and cannot apply to native permission approvals. Legacy
questions without summaries remain fully visible, never mechanically truncated or
hidden. Tab, arrow navigation, Enter/Space, visible focus and narrow windows must
work. Status is resolved from the durable controller at read time, not a cached copy.

## Evidence requirements

Regression coverage: original answer identity, cross-entry duplicate, dismiss vs
answer race, deadline/offline/restart, stale history and recycled Q ID, terminal
Worker, supersession, save failure and no native permission dispatch. Native UI
acceptance uses an independent fixture/Dev bundle through build_and_run.sh. Synthetic
native evidence is separate from real-model and published-app acceptance.
