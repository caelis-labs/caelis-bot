import test from 'node:test';
import assert from 'node:assert/strict';
import {TaskPreferencesSaver,validTaskLimit} from '../frontend/src/task-preferences.ts';
const initial={maxRunning:3,terminal:'system',revision:1};
test('autosave serializes writes and preserves edits made before a receipt',async()=>{
 let finish;const calls=[],states=[];
 const saver=new TaskPreferencesSaver(initial,async()=>initial,p=>{calls.push(p);return new Promise(resolve=>{finish=resolve;});},state=>states.push(state));
 saver.change({maxRunning:5});const first=saver.flush();
 saver.change({maxRunning:12});saver.change({terminal:'ghostty'});await saver.flush();
 assert.equal(calls.length,1);finish({...calls[0],revision:2});await Promise.resolve();
 assert.deepEqual(calls[1],{maxRunning:12,terminal:'ghostty',revision:2});
 finish({...calls[1],revision:3});await first;
 assert.deepEqual(states,['saving','saved']);
});
test('failed autosave is not retried on cleanup and next edit refreshes revision',async()=>{
 let fail=true,reads=0;const calls=[],states=[];
 const saver=new TaskPreferencesSaver(initial,async()=>{reads++;return {...initial,terminal:'iterm2',revision:9};},async p=>{calls.push(p);if(fail)throw Error('disk full');return {...p,revision:p.revision+1};},s=>states.push(s));
 saver.change({maxRunning:7});await saver.flush();await saver.flush();assert.equal(calls.length,1);
 fail=false;saver.change({maxRunning:8});await saver.flush();assert.equal(reads,1);
 assert.deepEqual(calls[1],{maxRunning:8,terminal:'iterm2',revision:9});assert.equal(states.at(-1),'saved');
});
test('invalid numeric drafts do not save the previous pending number',async()=>{
 const calls=[];const saver=new TaskPreferencesSaver(initial,async()=>initial,async p=>{calls.push(p);return p;},()=>{});
 saver.change({maxRunning:12});saver.discardLimit();saver.change({terminal:'terminal'});await saver.flush();
 assert.equal(calls[0].maxRunning,3);
 for(const v of ['', ' ', '0','-1','1.1','NaN','9007199254740992'])assert.equal(validTaskLimit(v),false,v);
 for(const v of ['1','3','12','1000'])assert.equal(validTaskLimit(v),true,v);
});
