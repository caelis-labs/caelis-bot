import { test } from 'node:test';
import assert from 'node:assert/strict';
import { activeReplyID, canSubmit, chatActivity, composerAction, incompleteAssistant, liveReplyIDs, withOutgoing } from '../frontend/src/chat-presentation.ts';

const running = { connection:'ready', phase:'working', currentTurn:'current',
 canInterrupt:true, canSend:false, canSteer:true, items:[], approvals:[], reviews:[] };

test('a prior partial is marked incomplete after its turn, while later completed text stays final',()=>{
 const partial={id:'partial',kind:'assistant',turnKey:'old',text:'已收到的片段',status:'inProgress'};
 const following={...partial,id:'following',text:'完整回复',status:'completed'};
 const done={...running,phase:'completed',currentTurn:'',items:[partial,following]};
 assert.equal(incompleteAssistant(partial,done),true);
 assert.equal(incompleteAssistant(following,done),false);
 assert.equal(incompleteAssistant({...partial,status:'incomplete'},done),true);
 assert.equal(incompleteAssistant({...partial,turnKey:'current'},running),false);
 assert.equal(incompleteAssistant({...partial,kind:'activity'},done),false);
 assert.equal(activeReplyID(done),null);
 assert.deepEqual([...liveReplyIDs(running,{...running,items:[partial,following]},new Set())],['partial','following']);
});

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
 assert.equal(chatActivity({...running,reviews:[{status:'inProgress'}]}),'thinking');
});

test('worker and historical reviews never animate the current conversation',()=>{
 const active={...running,reviews:[{status:'inProgress',turnKey:'worker'}],approvals:[{status:'pending',turnKey:'worker'}]};
 assert.equal(chatActivity(active),'thinking');
 assert.equal(composerAction({...active,canInterrupt:false,canSend:true,currentTurn:''},false,false),'send');
 const replying={...active,items:[{id:'answer',kind:'assistant',turnKey:'current',text:'answer',status:'inProgress'}]};
 assert.equal(activeReplyID(replying),'answer');
 assert.equal(chatActivity(replying),null);
 assert.equal(chatActivity({...active,reviews:[{status:'inProgress',turnKey:'current'}]}),'thinking');
 assert.equal(chatActivity({...active,phase:'completed',currentTurn:''}),null);
});

test('streaming reply takes the place of dots; empty, earlier or completed messages do not hide tool waiting', () => {
 const item={id:'answer',kind:'assistant',turnKey:'current',text:'正在回复',status:'inProgress'};
 assert.equal(chatActivity({...running,items:[item]}),null);
 assert.equal(chatActivity({...running,items:[{...item,status:'inProgress'}]}),null);
 for(const change of [{text:''},{turnKey:'previous'},{status:''},{status:'completed'},{kind:'activity'}]) {
  assert.equal(chatActivity({...running,items:[{...item,...change}]}),'thinking');
 }
});

test('only the latest streaming reply owns reply animation; control states and quiet work preempt it',()=>{
 const item={id:'first',kind:'assistant',turnKey:'current',text:'第一段',status:'inProgress'};
 const snapshot={...running,items:[item,{...item,id:'latest'}]};
 assert.equal(activeReplyID(snapshot),'latest');assert.equal(chatActivity(snapshot),null);
 for(const update of [{quiet:true},{phase:'interrupting'},{phase:'completed'},{connection:'disconnected'},{approvals:[{status:'pending'}]}])assert.equal(activeReplyID({...snapshot,...update}),null);
 assert.equal(activeReplyID({...snapshot,reviews:[{status:'inProgress'}]}),'latest');
 assert.equal(activeReplyID({...running,items:[{...item,turnKey:'previous'}]}),null);
});

test('automatic checks and silent completion do not add thinking rows',()=>{
 assert.equal(chatActivity({...running,scheduled:true,quiet:true}),null);
 assert.equal(chatActivity({...running,scheduled:true,quiet:false,phase:'interrupting'}),'stopping');
 assert.equal(chatActivity({...running,scheduled:true,quiet:false,phase:'failed'}),null);
});

test('streaming text hides tool waiting; completed commentary restores it; control states still win',()=>{
 const snapshot={...running,activity:{kind:'read',target:'README.md'},items:[{id:'answer',kind:'assistant',turnKey:'current',text:'I will check',status:'inProgress'}]};
 assert.equal(chatActivity(snapshot),null);
 assert.equal(chatActivity({...snapshot,items:[{...snapshot.items[0],status:'completed'}]}),'tool');
 // Recovery has no agentMessage status; old commentary is not proof of streaming.
 assert.equal(chatActivity({...snapshot,items:[{...snapshot.items[0],status:''}]}),'tool');
 assert.equal(chatActivity({...snapshot,items:[{...snapshot.items[0],text:''}]}),'tool');
 assert.equal(chatActivity({...snapshot,items:[{...snapshot.items[0],turnKey:'old'}]}),'tool');
 assert.equal(chatActivity({...snapshot,reviews:[{status:'inProgress'}]}),null);
 assert.equal(chatActivity({...snapshot,approvals:[{status:'pending'}]}),null);
 assert.equal(chatActivity({...snapshot,phase:'interrupting'}),'stopping');
 assert.equal(chatActivity({...snapshot,phase:'completed'}),null);
 assert.equal(chatActivity({...snapshot,connection:'offline'}),null);
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


test('new completed replies animate even when no in-progress snapshot was observed',()=>{
 const history={...running,phase:'completed',currentTurn:'',items:[{id:'old',kind:'assistant',text:'历史',status:'completed'}]};
 const answer={id:'new',kind:'assistant',turnKey:'new-turn',text:'在两次轮询之间完成的新回复',status:'completed'};
 const next={...history,items:[...history.items,answer]};
 assert.deepEqual([...liveReplyIDs(null,history,new Set())],[]);
 const live=liveReplyIDs(history,next,new Set());
 assert.deepEqual([...live],['new']);
 assert.equal(activeReplyID(next),null);
 assert.deepEqual([...liveReplyIDs(next,next,live)],['new']);
});

test('initial/reopened/recovered history and prepended pages do not replay',()=>{
 const item={id:'latest',kind:'assistant',text:'最新历史',status:'completed'};
 const snapshot={...running,items:[item]};
 assert.equal(liveReplyIDs(null,snapshot,new Set(['latest'])).size,0);
 assert.equal(liveReplyIDs({...snapshot,connection:'offline'},snapshot,new Set()).size,0);
 assert.equal(liveReplyIDs(snapshot,{...snapshot,items:[{...item,id:'earlier'},item]},new Set()).size,0);
 assert.equal(liveReplyIDs(snapshot,{...snapshot,items:[{...item,id:'replacement'}]},new Set()).size,0);
});

test('empty started items and extensions stay live across final status and cleared turn',()=>{
 const item={id:'answer',kind:'assistant',turnKey:'current',text:'',status:'inProgress'};
 const started={...running,items:[item]};
 const completed={...running,phase:'completed',currentTurn:'',items:[{...item,text:'完整回复',status:'completed'}]};
 assert.deepEqual([...liveReplyIDs(started,completed,new Set())],['answer']);
 const initial={...started,items:[{...item,text:'已有部分'}]};
 const extended={...started,items:[{...item,text:'已有部分，加上增量'}]};
 assert.deepEqual([...liveReplyIDs(initial,extended,new Set())],['answer']);
 const commentary={...initial,items:[{...initial.items[0],status:'completed'}]};
 assert.deepEqual([...liveReplyIDs(commentary,extended,new Set())],['answer'],'an ACP message can resume streaming after a tool boundary');
 const corrected={...completed,items:[{...completed.items[0],text:'完整回复，历史修正'}]};
 assert.equal(liveReplyIDs(completed,corrected,new Set()).size,0);
});

test('bounded pet snapshots may replace their previous tail with a new completed reply',()=>{
 const previous={...running,items:[{id:'old',kind:'assistant',text:'旧回复',status:'completed'}]};
 const next={...previous,phase:'completed',items:[{id:'new',kind:'assistant',text:'新回复',status:'completed'}]};
 assert.deepEqual([...liveReplyIDs(previous,next,new Set(),true)],['new']);
 assert.equal(liveReplyIDs(null,next,new Set(),true).size,0);
});

test('a completed reply after an accepted outbox replacement is still a live arrival',()=>{
 const pending={id:'outgoing:request',requestId:'request',kind:'user',text:'你好',status:'sending'};
 const previous={...running,items:[pending]};
 const next={...running,phase:'completed',currentTurn:'',items:[{...pending,id:'native',status:'completed'},
  {id:'reply',kind:'assistant',text:'你好！',status:'completed'}]};
 assert.deepEqual([...liveReplyIDs(previous,next,new Set())],['reply']);
 // Identical prose from a different request cannot establish this boundary.
 assert.equal(liveReplyIDs(previous,{...next,items:[{...next.items[0],requestId:'other'},next.items[1]]},new Set()).size,0);
});

test('confirmed Dream has a quiet status that yields to attention and ends with the turn',()=>{
 const dreaming={...running,quiet:true,maintenance:'dreaming'};
 assert.equal(chatActivity(dreaming),'dreaming');
 assert.equal(chatActivity({...dreaming,maintenance:undefined}),null);
 assert.equal(chatActivity({...dreaming,approvals:[{status:'pending'}]}),null);
 assert.equal(chatActivity({...dreaming,phase:'interrupting'}),'stopping');
 for(const change of [{phase:'completed'},{phase:'unknown'},{connection:'offline'},{message:'Recovery required'}])assert.equal(chatActivity({...dreaming,...change}),null);
 assert.equal(canSubmit(dreaming),true,'new user input retains its native capability');
});
