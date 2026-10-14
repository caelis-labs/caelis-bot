import test from 'node:test';
import assert from 'node:assert/strict';
import { connectionModelOption, defaultChatModels, eligibleSelection } from '../frontend/src/settings/runtime/connection-models.ts';
import { connectSelectedModels } from '../frontend/src/settings/runtime/connection-sequence.ts';
const choice = (value, chatEligibility, extras = {}) => ({ value, label: value, noAuth: false, current: false, chatEligibility, ...extras });

test('only explicit chat entries are default selected; ASR, judgment and unknown are not', () => {
 const models = [
  connectionModelOption(choice('chat', 'chat', { detail: 'Host detail', modelMetadataComplete: true, modelImageInputKnown: false })),
  connectionModelOption(choice('asr', 'non-chat')),
  connectionModelOption(choice('judge-only', 'non-chat')),
  connectionModelOption(choice('opaque', undefined, { modelMetadataComplete: true })),
  connectionModelOption(choice('invalid', 'other')),
 ];
 assert.deepEqual(defaultChatModels(models), ['chat']);
 assert.deepEqual(eligibleSelection(models, models.map(model => model.value), new Set()), ['chat']);
 assert.deepEqual(eligibleSelection(models.slice(1), ['asr', 'judge-only', 'opaque'], new Set()), []);
 assert.equal(models[0].detail, 'Host detail');
 assert.equal(models[0].imageInputKnown, false);
 assert.equal(models[3].chatEligibility, 'unknown');
 assert.equal(models[3].metadataComplete, true);
});

for (const terminal of ['failed', 'unknown']) {
 test(`batch stops at ${terminal}; a retry excludes the confirmed model`, async () => {
  const models = ['chat-a', 'chat-b', 'chat-c'].map(value => connectionModelOption(choice(value, 'chat')));
  const calls = [];
  const confirmed = new Set();
  const client = { startConnection: async input => {
   calls.push(input.model);
   return { id: input.model, revision: '1', sequence: 1, stage: input.model === 'chat-b' && calls.length === 2 ? terminal : 'complete', title: '', message: '' };
  } };
  const selected = defaultChatModels(models);
  const result = await connectSelectedModels(client, { kind: 'api-key', choice: 'fixture' }, selected, new AbortController().signal, () => {}, count => selected.slice(0, count).forEach(id => confirmed.add(id)));
  assert.equal(result.stage, terminal);
  assert.deepEqual(calls, ['chat-a', 'chat-b']);
  assert.deepEqual(eligibleSelection(models, selected, confirmed), ['chat-b', 'chat-c']);
  if (terminal === 'failed') {
   const remaining = eligibleSelection(models, selected, confirmed);
   await connectSelectedModels(client, { kind: 'api-key', choice: 'fixture' }, remaining, new AbortController().signal, () => {}, count => remaining.slice(0, count).forEach(id => confirmed.add(id)));
   assert.deepEqual(calls, ['chat-a', 'chat-b', 'chat-b', 'chat-c']);
  }
 });
}
