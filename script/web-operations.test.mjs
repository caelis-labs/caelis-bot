import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createWebOperations} from '../frontend/src/web-operations.ts';
function fixture(){
 const saved=new Map(),posts=[],receipts=new Map();let counter=0,reply;
 const failure=(code,detail)=>Object.assign(Error(detail||code),{code,detail});
 const call=createWebOperations({storage:{getItem:k=>saved.get(k),setItem:(k,v)=>saved.set(k,v)},identity:()=>String(++counter).padStart(32,'0'),failure,
 post:async(path,body)=>{posts.push(body);return await reply(body)},receipt:async id=>receipts.get(id)||{state:'pending'}});
 return {call,posts,receipts,saved,failure,setReply:v=>reply=v};
}
test('known refusal reads original receipt, correction uses a new explicit ID',async()=>{
 const f=fixture();f.setReply(async b=>{f.receipts.set(b.id,{state:'complete',error:'operation_rejected'});throw f.failure('operation_rejected')});
 await assert.rejects(f.call('/connect',['bad-secret'],false),/operation_rejected/);
 assert(![...f.saved.values()].join('').includes('bad-secret'));
 f.setReply(async b=>({value:'connected'}));
 assert.equal(await f.call('/connect',['corrected-secret',true],false),'connected');
 assert.notEqual(f.posts[0].id,f.posts[1].id);
});
test('lost response and edited payload read original first; never inherit or replay',async()=>{
 const f=fixture();f.setReply(async()=>{throw f.failure('network')});
 await assert.rejects(f.call('/settings',['first'],false),/network/);
 f.receipts.set(f.posts[0].id,{state:'complete',value:'first saved'});
 await assert.rejects(f.call('/settings',['edited'],false),/previous_operation_resolved/);
 assert.equal(f.posts.length,1);
 f.setReply(async()=>({value:'edited saved'}));
 assert.equal(await f.call('/settings',['edited'],false),'edited saved');
 assert.equal(f.posts.length,2);assert.notEqual(f.posts[0].id,f.posts[1].id);
});
test('conflict still reads original; true unknown cannot authorize a new write',async()=>{
 const f=fixture();f.setReply(async b=>{f.receipts.set(b.id,{state:'complete',value:'original'});throw f.failure('request_conflict')});
 assert.equal(await f.call('/settings',['same'],false),'original');
 const u=fixture();u.setReply(async()=>({value:{outcome:'unknown',id:'native-original'}}));
 await u.call('/settings',['old'],false);
 u.receipts.set(u.posts[0].id,{state:'complete',value:{outcome:'unknown'}});
 await assert.rejects(u.call('/settings',['changed'],false),/operation_unconfirmed/);
 assert.equal(u.posts.length,1);
});

test('terminal native refusal carries only its safe detail from the original receipt',async()=>{
 const f=fixture();f.setReply(async b=>{f.receipts.set(b.id,{state:'complete',error:'operation_rejected',detail:'pairing_code_invalid'});throw f.failure('network')});
 await assert.rejects(f.call('/connect',['wrong'],false),error=>error.code==='operation_rejected'&&error.detail==='pairing_code_invalid');
 f.setReply(async()=>({value:'connected'}));
 assert.equal(await f.call('/connect',['correct'],false),'connected');
 assert.equal(f.posts.length,2);
 const unsafe=fixture();unsafe.setReply(async b=>{unsafe.receipts.set(b.id,{state:'complete',error:'operation_rejected',detail:'PRIVATE_NATIVE_BODY'});throw unsafe.failure('network')});
 await assert.rejects(unsafe.call('/connect',['fixture'],false),error=>error.code==='operation_rejected'&&error.detail===undefined&&!error.message.includes('PRIVATE_'));
});

test('HTTP refusal alone never clears an unconfirmed original operation',async()=>{
 const f=fixture();f.setReply(async()=>{throw f.failure('operation_rejected','pairing_code_expired')});
 await assert.rejects(f.call('/connect',['original'],false),error=>error.code==='operation_rejected');
 const original=[...f.saved.values()].join('');
 await assert.rejects(f.call('/connect',['changed'],false),error=>error.code==='operation_unconfirmed');
 assert.equal(f.posts.length,1);assert.equal([...f.saved.values()].join(''),original);
});

test('network timeout and response decoding failures retain the same original identity',async()=>{
 for(const error of [new TypeError('network failed'),new DOMException('timeout','TimeoutError'),new SyntaxError('response JSON was truncated')]){
  const f=fixture();f.setReply(async()=>{throw error});
  await assert.rejects(f.call('/connect',['original'],false),cause=>cause===error);
  const original=[...f.saved.values()].join('');
  await assert.rejects(f.call('/connect',['edited'],false),cause=>cause.code==='operation_unconfirmed'&&cause.detail===undefined);
  assert.equal(f.posts.length,1);assert.equal([...f.saved.values()].join(''),original);
 }
});

test('uncertain remote approval cannot consume a different local approval slot',async()=>{
 const saved=new Map(),posts=[];let n=0;
 const invoke=createWebOperations({storage:{getItem:k=>saved.get(k)||null,setItem:(k,v)=>saved.set(k,v)},identity:()=>`isolated-request-${++n}`,failure:code=>Object.assign(Error(code),{code}),post:async(path,body)=>{posts.push(body);if(body.args[0].id==='remote-request')throw Object.assign(Error('operation_unconfirmed'),{code:'operation_unconfirmed'});return {value:null}},receipt:async()=>({state:'complete',error:'operation_unconfirmed'})});
 const path='/api/v1/conversation/decide';
 await assert.rejects(invoke(path,[{id:'remote-request',choice:'approve'}],false));
 const original=JSON.parse(saved.get('caelisbot.pending-operations'))[path+'@remote-request'];assert.ok(original);
 await invoke(path,[{id:'local-request',choice:'approve'}],false);
 await assert.rejects(invoke(path,[{id:'remote-request',choice:'decline'}],false));
 assert.equal(posts.length,2);assert.equal(JSON.parse(saved.get('caelisbot.pending-operations'))[path+'@remote-request'],original);
});

test('one Bot connection unknown cannot gate another stable environment',async()=>{
 const saved=new Map(),posts=[];let n=0,reads=0;
 const invoke=createWebOperations({storage:{getItem:k=>saved.get(k)||null,setItem:(k,v)=>saved.set(k,v)},identity:()=>`isolated-peer-${++n}`,failure:code=>Object.assign(Error(code),{code}),post:async(path,body)=>{posts.push(body);if(body.args[0].id==='peer-one')throw Object.assign(Error('operation_unconfirmed'),{code:'operation_unconfirmed'});return {value:{id:'peer-two',state:'ready'}}},receipt:async id=>{reads++;return {state:'complete',error:'operation_unconfirmed',recovery:{operationId:id,profileId:'peer-one',canAuthorize:true}}}});
 const path='/api/v1/machines/connect-bot';
 await assert.rejects(invoke(path,[{id:'peer-one',address:'http://127.0.0.1:1',code:'one'}],false));
 const before=reads;await invoke(path,[{id:'peer-two',address:'http://127.0.0.1:2',code:'two'}],false);
 assert.equal(reads,before);assert.equal(posts.length,2);assert.ok(JSON.parse(saved.get('caelisbot.pending-operations'))[path+'@peer-one']);
});

test('inflight remote question answer leaves another original question usable',async()=>{
 let release,entered;const held=new Promise(r=>release=r),started=new Promise(r=>entered=r),posts=[];let n=0;
 const invoke=createWebOperations({storage:{getItem:()=>null,setItem(){}},identity:()=>`question-original-${++n}`,failure:code=>Object.assign(Error(code),{code}),post:async(path,body)=>{posts.push(body);if(body.args[0]==='Q-remote'){entered();await held}return {value:'recorded'}},receipt:async()=>({state:'pending'})});
 const path='/api/v1/conversation/answer-question';const remote=invoke(path,['Q-remote','native-binding','answer','original-remote-answer'],false);await started;
 assert.equal(await invoke(path,['Q-local','local-binding','answer','original-local-answer'],false),'recorded');
 release();await remote;assert.equal(posts.length,2);
});
