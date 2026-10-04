# Perform known desktop actions

Authorize the observed application as described in [Observation](desktop-observation.md).
Prefer supported semantic operations: `invoke`, `set_value`, `set_expanded`,
`set_selected`, `set_checked`, and `scroll_into_view`. Desired state operations
always verify, including no-op results; use an explicit boolean, including false:

```json
{"requestId":"uncheck-once-001","steps":[{"id":"uncheck","op":"set_checked","target":{"ref":"OBSERVED_CHECKBOX_REF"},"set_checked":{"checked":false},"completion":"verify"}]}
```

Expanded and selected use `set_expanded:{"expanded":false}` and
`set_selected:{"selected":true}` respectively. `scroll_into_view` has no argument
arm. Neither do `invoke`, `focus` or `pointer.move`; do not add `invoke:{}`.
Semantic state changes have no automatic pointer/keyboard fallback. Unknown or
mixed checked state is not false. Selection follows the provider's rules and
may affect other items; do not clear siblings yourself.

A Ref and a capability establish identity/support, not what an unnamed button
does. Use an observed menu, documented in-app shortcut, or pixels when needed.
After creating a document verify its identity and initial content; closing a
gallery can reveal an existing document. Do not guess or relabel a stale Ref.

# Keep cooperative input within one short plan

`bot_desktop_act` accepts a stable `requestId` and up to 16 ordered steps.
Physical input temporarily borrows the target application's focus, then attempts
to restore the user's prior app/window. Semantic steps can avoid borrowing.
Keep known focus/click, keyboard text and submit together in one short plan;
a separate focus call restores focus before the next call. Do not assume a
previous transaction left your target active. Focus an observed window or
focusable UI Ref, not an application. Keyboard actions must target the verified
focused object belonging to the intended app/window within that transaction.

Use `keyboard.type_text` with `type_text:{"text":...}` (at most 256 UTF-16 units,
so emoji can count as two), or `keyboard.press` with
`press:{"key":"Enter","modifiers":[]}`. `primary` means Command on macOS;
keys are `Enter`, `Tab`, `Escape`, uppercase letters, not `Return` or `cmd`.
Newlines and tabs in typed text are real Enter/Tab keys. Prefer semantic
`set_value` for supported fields or longer text. Drag lasts at most 500ms, within
one observed window. Use Refs/target anchors; raw Points are unavailable.
The helper enforces a 1-second input budget; it is not a total-call time promise.
Never split, truncate or replay a rejected/partial plan automatically.

Batch only known targets and effects. A known future menu/dialog can use a
unique, narrowly scoped `bind` and its alias within that plan. Unknown future UI
requires a fresh observation; never predict its Ref. Use explicit `before` and
`after` predicates where useful, and local `wait` for known conditions instead
of model polling. `completion:"verify"` for other writes requires nonempty
`after` predicates. To invoke without one use `completion:"dispatch"` and inspect
the result separately. A validation rejection before dispatch is correctable
using the original ID; it is different from uncertain input.

# Verify the user result

Receipts separate channel, delivery and verification. `semantic` and
`foreground_transaction` describe how the step ran, not success. Read `input.mode`,
`foreground_ms` and `restoration`: `not_borrowed`, `restored`, `user_superseded` or
`failed`. A user switching applications takes priority; do not reactivate your
target. Cleanup failure or a fenced seat requires [Recovery](desktop-recovery.md).
Same-app human changes are best effort; do not promise an independent virtual
mouse or background input.

Dispatch alone does not prove text reached the right field, a file was saved,
or a dialog opened. Verify using narrow observation/text/delta or explicit
pixels. `set_value` proves the native control value, not an application commit:
a title/filename may need Enter or a focus change. Check the resulting title,
file entry or saved-document state. For numeric native `value` predicates use
its actual text (checkbox "0"/"1", mixed "2"); a label is not its value. A
verification timeout after complete delivery is not permission to toggle again.
