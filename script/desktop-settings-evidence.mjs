// Validate evidence recorded by the native synthetic fixture, not a substitute
// for opening and inspecting its actual windows.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
const root=process.argv[2]||'.cache/settings-acceptance';
const read=name=>JSON.parse(readFileSync(join(root,name+'.json'),'utf8'));
const navigation=read('navigation');
assert.deepEqual(navigation.after,navigation.before);
assert.equal(navigation.before.scroll,140);
assert.equal(navigation.before.files,true);
assert.ok(navigation.before.draft);
const failed=read('chat-error'),dismissed=read('chat-dismissed'),unknown=read('chat-unknown');
for(const state of [failed,dismissed,unknown]){
 assert.equal(state.composerFeedback,0);
 assert.equal(state.overflow,false);
}
assert.equal(failed.messageFeedback,1);
assert.equal(dismissed.messageFeedback,0);
assert.equal(failed.messages,24);
assert.equal(dismissed.messages,24);
assert.equal(unknown.messages,25);
assert.equal(unknown.calls.filter(method=>method==='Submit').length,1);
assert.equal(unknown.messageFeedback,1);
assert.equal(dismissed.draft,navigation.before.draft);
console.log('Native fixture evidence: retained navigation, local feedback, dismissal and unknown-receipt retry passed.');
