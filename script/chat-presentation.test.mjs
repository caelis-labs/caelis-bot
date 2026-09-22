import { test } from 'node:test';
import assert from 'node:assert/strict';
import { chatActivity, composerAction } from '../frontend/src/chat-presentation.ts';

const running = { connection:'ready', phase:'working', currentTurn:'current',
 canInterrupt:true, canSend:false, canSteer:true, items:[], approvals:[], reviews:[] };

test('empty running input stops; text or attachments switch back to send without changing capability', () => {
 assert.equal(composerAction(running,false,false),'stop');
 assert.equal(composerAction(running,false,true),'send');
 assert.equal(composerAction({...running,canSteer:false},false,true),'send');
 assert.equal(composerAction({...running,canInterrupt:false},false,false),'send');
 assert.equal(composerAction(null,false,false),'send');
 // The quick capsule remains a new-prompt surface; live steering belongs to chat.
 assert.equal(composerAction(running,true,false),'send');
});

test('waiting excludes approvals, recovery and terminal states, even with a remaining interrupt capability', () => {
 assert.equal(chatActivity(running),'thinking');
 assert.equal(chatActivity({...running,phase:'sending'}),'thinking');
 for(const phase of ['completed','failed','interrupted','unknown','unconfirmed','attention']) {
  assert.equal(chatActivity({...running,phase}),null,phase);
 }
 assert.equal(chatActivity({...running,connection:'disconnected'}),null);
 for(const status of ['pending','sending','sent','unknown','unavailable']) {
  assert.equal(chatActivity({...running,approvals:[{status}]}),null,status);
 }
 assert.equal(chatActivity({...running,phase:'interrupting'}),'stopping');
 assert.equal(chatActivity({...running,reviews:[{status:'inProgress'}]}),'reviewing');
});

test('streaming reply takes the place of dots; empty, earlier or completed messages do not hide tool waiting', () => {
 const item={kind:'assistant',turnKey:'current',text:'正在回复',status:''};
 assert.equal(chatActivity({...running,items:[item]}),null);
 assert.equal(chatActivity({...running,items:[{...item,status:'inProgress'}]}),null);
 for(const change of [{text:''},{turnKey:'previous'},{status:'completed'},{kind:'activity'}]) {
  assert.equal(chatActivity({...running,items:[{...item,...change}]}),'thinking');
 }
});
