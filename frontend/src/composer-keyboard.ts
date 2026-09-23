// IME boundary events may report isComposing=false but still carry keyCode 229.
// Keep those events (and Shift+Enter) under the editor/input method's control.
export function handleComposerKey(
  event: Pick<KeyboardEvent, 'key' | 'shiftKey' | 'isComposing' | 'keyCode' | 'repeat' | 'defaultPrevented' | 'preventDefault'>,
  send: Pick<HTMLButtonElement, 'disabled' | 'click'> | null,
  action: 'send' | 'stop' = 'send',
) {
  if (event.defaultPrevented || event.key !== 'Enter' || event.shiftKey || event.isComposing || event.keyCode === 229) return;
  event.preventDefault();
  // Enter submits text only. An empty editor must never trigger the same
  // button's stop action; stopping is an explicit pointer/button-focus action.
  if (action === 'send' && !event.repeat && send && !send.disabled) send.click();
}
