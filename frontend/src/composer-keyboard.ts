// IME boundary events may report isComposing=false but still carry keyCode 229.
// Keep those events (and Shift+Enter) under the editor/input method's control.
export function handleComposerKey(
  event: Pick<KeyboardEvent, 'key' | 'shiftKey' | 'isComposing' | 'keyCode' | 'repeat' | 'defaultPrevented' | 'preventDefault'>,
  send: Pick<HTMLButtonElement, 'disabled' | 'click'> | null,
) {
  if (event.defaultPrevented || event.key !== 'Enter' || event.shiftKey || event.isComposing || event.keyCode === 229) return;
  event.preventDefault();
  // Share the button's availability and action; holding Enter must not resend.
  if (!event.repeat && send && !send.disabled) send.click();
}
