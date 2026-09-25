import { test } from 'node:test';
import assert from 'node:assert/strict';
import { activeReplyID, canSubmit, chatActivity, composerAction, withOutgoing } from '../frontend/src/chat-presentation.ts';

const running = { connection:'ready', phase:'working', currentTurn:'current',
 canInterrupt:true, canSend:false, canSteer:true, items:[], approvals:[], reviews:[] };

test('empty running input stops; text or attachments switch back to send without changing capability', () => {
 assert.equal(composerAction(running,false,false),'stop');
 assert.equal(composerAction(running,false,true),'send');
 assert.equal(composerAction({...running,canSteer:false},false,true),'send');
 assert.equal(composerAction({...running,canInterrupt:false},false,false),'send');
 assert.equal(composerAction(null,false,false),'send');
 // Quick input supports steering but does not add a stop button.
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
 const item={id:'answer',kind:'assistant',turnKey:'current',text:'正在回复',status:''};
 assert.equal(chatActivity({...running,items:[item]}),null);
 assert.equal(chatActivity({...running,items:[{...item,status:'inProgress'}]}),null);
 for(const change of [{text:''},{turnKey:'previous'},{status:'completed'},{kind:'activity'}]) {
  assert.equal(chatActivity({...running,items:[{...item,...change}]}),'thinking');
 }
});

test('only the latest streaming avatar moves; approval, stop and disconnect keep a static identity',()=>{
 const item={id:'first',kind:'assistant',turnKey:'current',text:'第一段',status:'inProgress'};
 const snapshot={...running,items:[item,{...item,id:'latest'}]};
 assert.equal(activeReplyID(snapshot),'latest');assert.equal(chatActivity(snapshot),null);
 for(const update of [{phase:'interrupting'},{phase:'completed'},{connection:'disconnected'},{approvals:[{status:'pending'}]},{reviews:[{status:'inProgress'}]}])assert.equal(activeReplyID({...snapshot,...update}),null);
 assert.equal(activeReplyID({...running,items:[{...item,turnKey:'previous'}]}),null);
});

test('automatic checks and silent completion do not add thinking rows',()=>{
 assert.equal(chatActivity({...running,scheduled:true,quiet:true}),null);
 assert.equal(chatActivity({...running,scheduled:true,quiet:false,phase:'interrupting'}),'stopping');
 assert.equal(chatActivity({...running,scheduled:true,quiet:false,phase:'failed'}),null);
});


test('all editors use native send/steer capability and optimistic inputs merge by identity',()=>{
 assert.equal(canSubmit(running),true);
 assert.equal(canSubmit({...running,canSend:false,canSteer:false}),false);
 assert.equal(canSubmit(null),false);
 const first={id:'local-1',requestId:'one',kind:'user',text:'same',status:'sending'};
 const second={...first,id:'local-2',requestId:'two'};
 assert.deepEqual(withOutgoing([], [first]),[first]);
 const native={...first,id:'native-1',status:'completed'};
 assert.deepEqual(withOutgoing([native],[first,second]),[native,second]);
 assert.deepEqual(withOutgoing([{id:'foreign',text:'same'}],[first]),[{id:'foreign',text:'same'},first]);
});
