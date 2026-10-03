import {test} from 'node:test';
import assert from 'node:assert/strict';
import {ConversationOrder,SubmissionProgress,DraftQueue} from '../frontend/src/chat-observation.ts';
import {settingsDestination,settingsSections} from '../frontend/src/settings-navigation.ts';

test('a late poll cannot replace a refreshed outbox at the same backend revision',()=>{
 const state=new ConversationOrder();state.reset();
 const poll=state.request(),refresh=state.request();
 assert.equal(state.accept(refresh,12),true);
 assert.equal(state.accept(poll,12),false);
 assert.equal(state.accept(state.request(),11),false);
 assert.equal(state.accept(state.request(),12),true);
});
test('closing and reopening creates a fresh baseline and rejects hidden-surface reads',()=>{
 const state=new ConversationOrder();const closing=state.request();state.accept(closing,25);
 state.reset();assert.equal(state.revision,0);assert.equal(state.accept(closing,30),false);
 assert.equal(state.accept(state.request(),25),true);
});
test('polling and direct acceptance consume the draft once, including delayed completion',async()=>{
 const state=new SubmissionProgress();let resolve,count=0;
 const read=()=>{count++;return new Promise(r=>resolve=r);};
 state.observe('accepted');const poll=state.synchronize(read);
 state.observe('accepted');const direct=state.synchronize(read);
 assert.equal(poll,direct);await Promise.resolve();assert.equal(count,1);
 resolve();await Promise.all([poll,direct]);assert.equal(state.observe('unknown'),'accepted');
});
test('an accepted send is not downgraded or retried after draft synchronization fails',async()=>{
 const state=new SubmissionProgress();let reads=0;
 assert.throws(()=>state.synchronize(async()=>{}));state.observe('accepted');
 const read=()=>{reads++;return Promise.reject(new Error('draft offline'));};
 await assert.rejects(state.synchronize(read));assert.equal(state.outcome,'accepted');
 await assert.rejects(state.synchronize(read));assert.equal(reads,1);
 assert.equal(state.observe('rejected'),'accepted');
});
test('uncertain submissions stay uncertain until that exact request is confirmed',()=>{
 const state=new SubmissionProgress();assert.equal(state.observe(''),'unknown');
 assert.throws(()=>state.synchronize(async()=>{}));assert.equal(state.observe('accepted'),'accepted');
});
test('settings separate models, accounts and machines while native repair links remain valid',()=>{
 assert.deepEqual(settingsSections,['general','appearance','models','connections','machines','permissions','updates']);
 assert.equal(settingsDestination('runtime'),'connections');assert.equal(settingsDestination('machines'),'machines');
 assert.equal(settingsDestination('execution'),'permissions');assert.equal(settingsDestination('setup'),'setup');
 assert.equal(settingsDestination('invalid'),null);
});

test('typing coalesces only pending draft replacements and send flush waits for the newest revision',async()=>{
 const queue=new DraftQueue(),writes=[];let release;
 queue.enqueue(async()=>{writes.push('first');await new Promise(resolve=>release=resolve);});
 await Promise.resolve();
 queue.enqueue(async()=>{writes.push('intermediate');});
 queue.enqueue(async()=>{writes.push('newest');});
 let flushed=false;const sending=queue.flush().then(()=>flushed=true);
 await Promise.resolve();assert.equal(flushed,false);release();await sending;
 assert.deepEqual(writes,['first','newest']);assert.equal(flushed,true);
 await queue.enqueue(async()=>{writes.push('after send');});assert.equal(writes.at(-1),'after send');
});
