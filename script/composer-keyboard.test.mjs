import { test } from 'node:test';
import assert from 'node:assert/strict';
import { handleComposerKey } from '../frontend/src/composer-keyboard.ts';

function press(overrides = {}, disabled = false, action = 'send') {
  let prevented = false, sends = 0;
  handleComposerKey({ key: 'Enter', shiftKey: false, isComposing: false, keyCode: 13,
    repeat: false, defaultPrevented: false, preventDefault() { prevented = true; }, ...overrides },
  { disabled, click() { sends++; } }, action);
  return { prevented, sends };
}
test('Enter shares the send action, but disabled send and key repeat cannot dispatch', () => {
  assert.deepEqual(press(), { prevented: true, sends: 1 });
  assert.deepEqual(press({}, true), { prevented: true, sends: 0 });
  assert.deepEqual(press({ repeat: true }), { prevented: true, sends: 0 });
});
test('Enter in an empty running composer never invokes the stop button', () => {
  assert.deepEqual(press({}, false, 'stop'), { prevented: true, sends: 0 });
  assert.deepEqual(press({ repeat: true }, false, 'stop'), { prevented: true, sends: 0 });
});
test('newline and IME candidate confirmation remain editor-owned', () => {
  for (const event of [{ shiftKey: true }, { isComposing: true },
    { isComposing: false, keyCode: 229 }, { key: 'Escape' }, { defaultPrevented: true }]) {
    assert.deepEqual(press(event), { prevented: false, sends: 0 });
  }
});
