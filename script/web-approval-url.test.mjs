import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,writeFileSync,mkdtempSync,rmSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {stripTypeScriptTypes} from 'node:module';
import {ConversationOrder} from '../frontend/src/chat-observation.ts';

// Run the actual browser transport with only TypeScript syntax removed. The
// browser APIs and authenticated host response are fixtures, never a live Bot.
const sourceURL=new URL('../frontend/src/web-transport.ts',import.meta.url);
const source=stripTypeScriptTypes(readFileSync(sourceURL,'utf8'),{mode:'transform'})
 .replace(/from '([.][^']+)'/g,(_,path)=>`from '${new URL(path.endsWith('.ts')?path:path+'.ts',sourceURL).href}'`);
const {createWebTransport}=await import('data:text/javascript;base64,'+Buffer.from(source+'\n//# sourceURL=web-transport.fixture.mjs').toString('base64'));

function browser(t,read){
 const requests=[],opened=[],events=new EventTarget(),saved=new Map(),streams=[];
 t.mock.method(globalThis,'fetch',async(path,init)=>{
  requests.push({path,body:JSON.parse(init.body)});
  const result=read(path,JSON.parse(init.body));
  return new Response(JSON.stringify(result.body),{status:result.status??200,headers:{'Content-Type':'application/json'}});
 });
 class EventSource extends EventTarget{
  constructor(){super();streams.push(this);}
  close(){}
  emit(type,value){this.dispatchEvent(new MessageEvent(type,{data:JSON.stringify(value)}));}
 }
 const globals={EventSource,window:{addEventListener:events.addEventListener.bind(events),removeEventListener:events.removeEventListener.bind(events),dispatchEvent:events.dispatchEvent.bind(events),open:(...args)=>opened.push(args)},
  document:{addEventListener(){},removeEventListener(){}},sessionStorage:{getItem:key=>saved.get(key)??null,setItem:(key,value)=>saved.set(key,value)}};
 const descriptors=Object.fromEntries(Object.keys(globals).map(name=>[name,Object.getOwnPropertyDescriptor(globalThis,name)]));
 for(const [name,value]of Object.entries(globals))Object.defineProperty(globalThis,name,{value,configurable:true,writable:true});
 const client=createWebTransport({csrf:'fixture-csrf',version:'FIXTURE',capabilities:{reason:''}});
 t.after(()=>{client.close();for(const [name,descriptor]of Object.entries(descriptors)){if(descriptor)Object.defineProperty(globalThis,name,descriptor);else delete globalThis[name]}});
 return {client,requests,opened,events,streams};
}

test('Web snapshot cache compares service epochs even at equal revisions',async t=>{
 const snapshot={revision:120,botStatus:'',processEpoch:'old',items:[]};
 let authoritative={changed:true,snapshot};
 const f=browser(t,()=>({body:{value:authoritative}}));
 f.client.start();const stream=f.streams[0],order=new ConversationOrder();
 const read=()=>f.client.transport('backend','ChatSnapshot',[order.revision,'',order.processEpoch]);
 stream.emit('snapshot',{changed:true,snapshot});
 let update=await read();assert.equal(order.accept(order.request(),update.snapshot.revision,update.snapshot.processEpoch),true);
 assert.equal((await read()).changed,false);
 authoritative={changed:true,snapshot:{...snapshot,processEpoch:'new',items:[{text:'first reply'}]}};
 stream.emit('snapshot',authoritative);
 update=await read();assert.equal(update.changed,true);
 assert.equal(order.accept(order.request(),update.snapshot.revision,update.snapshot.processEpoch),true);
 assert.equal(update.snapshot.items[0].text,'first reply');
 assert.equal((await read()).changed,false);
 assert.equal(f.requests.length,2,'initial and foreign epochs require confirmation');
});

test('SSE reset notifies a retained observer, invalidates cached and in-flight reads, and accepts a lower revision without epoch metadata',async t=>{
 const snapshot={revision:8,botStatus:'',items:[{text:'reply after restart'}]};
 const f=browser(t,()=>({body:{value:{changed:true,snapshot}}}));
 const order=new ConversationOrder();let resets=0;
 f.events.addEventListener('conversation-reset',()=>{resets++;order.reset();});
 f.client.start();const stream=f.streams[0];
 stream.emit('snapshot',{changed:true,snapshot:{...snapshot,revision:120,items:[]}});
 const before=await f.client.transport('backend','ChatSnapshot',[0,'','']);
 order.accept(order.request(),before.snapshot.revision);
 const stale=order.request();
 stream.emit('reset',{});
 assert.equal(resets,1);assert.equal(order.revision,0);
 assert.equal(order.accept(stale,121),false);
 const update=await f.client.transport('backend','ChatSnapshot',[order.revision,'',order.processEpoch]);
 assert.equal(f.requests.length,1,'reset must discard the old stream cache');
 assert.equal(order.accept(order.request(),update.snapshot.revision),true);
 assert.equal(update.snapshot.items[0].text,'reply after restart');
});

test('Web cannot open a remote loopback approval from a renderer snapshot',async t=>{
 const f=browser(t,path=>path==='/api/v1/conversation/snapshot'
  ?{body:{value:{approvals:[{id:'retained-original',status:'pending',url:'http://127.0.0.1:4444/native-challenge'}]}}}
  :{status:409,body:{error:'remote_native_url_not_reachable'}});
 await assert.rejects(f.client.transport('backend','OpenApprovalURL',['retained-original']));
 assert.deepEqual(f.opened,[],'B loopback must never be interpreted as A browser loopback');
 assert.deepEqual(f.requests.map(r=>r.path),['/api/v1/conversation/approval-url']);
 assert.deepEqual(f.requests[0].body.args,['retained-original']);
});

test('Web opens only the original owner validated approval URL and never decides it',async t=>{
 const f=browser(t,path=>{
  assert.equal(path,'/api/v1/conversation/approval-url');
  return {body:{value:'https://approval.example.test/original-challenge'}};
 });
 await f.client.transport('backend','OpenApprovalURL',['source-scoped-native-request']);
 assert.deepEqual(f.opened,[['https://approval.example.test/original-challenge','_blank','noopener,noreferrer']]);
 assert.equal(f.requests.length,1);
 assert.deepEqual(f.requests[0].body.args,['source-scoped-native-request']);
});

test('URL page submission has only the original adapter receipt and never persists entered fields',async t=>{
 const f=browser(t,path=>{
  assert.equal(path,'/api/v1/conversation/approval-page-submit');
  return {body:{value:{requestId:'original-url-operation',fingerprint:'intent-digest',outcome:'unknown'}}};
 });
 const value=await f.client.transport('backend','SubmitApprovalPage',['original-scoped-request','original-url-operation','original-page-revision',{secret:'synthetic-only'}]);
 assert.equal(value.outcome,'unknown');assert.equal(f.requests.length,1);assert.equal(f.requests[0].body.id,undefined,'do not create a second Web operation');assert.deepEqual(f.opened,[]);
 assert.equal(sessionStorage.getItem('caelisbot.pending-operations'),null,'form values/duplicate Web receipt must not persist');
});

// Inputs come from actual Go Service/HTTP/SSE handlers. Only browser delivery
// timing is controlled; no sends, approvals or effect receipts are replayed.
test('reset then HTTP B then cached A cannot retire the current Runtime',async t=>{
 const directory=mkdtempSync(join(tmpdir(),'bot-epoch-cache-')),file=join(directory,'proof.json');
 t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const producer=spawnSync('go',['test','./internal/websurface','-run','^TestChatResetCacheEpochSequence$','-count=1'],{cwd:new URL('..',import.meta.url),env:{...process.env,GOWORK:'off',CAELIS_BOT_TEST_EPOCH_PROOF:file},encoding:'utf8',timeout:180000});
 assert.equal(producer.status,0,producer.stdout+producer.stderr);
 const proof=JSON.parse(readFileSync(file,'utf8')),order=new ConversationOrder();
 let httpReads=0;
 const f=browser(t,(path,body)=>{
  if(httpReads++===0)return {body:{value:proof.http_current}};
  assert.deepEqual(body.args,[proof.http_current.snapshot.revision,'',proof.http_current.snapshot.processEpoch]);
  return {body:{value:proof.http_confirmed}};
 });
 order.accept(order.request(),proof.sse_after_reset.snapshot.revision,proof.sse_after_reset.snapshot.processEpoch);
 f.events.addEventListener('conversation-reset',()=>order.reset());f.client.start();
 const read=async()=>{const ticket=order.request(),expected=order.revision,u=await f.client.transport('backend','ChatSnapshot',[expected,'',order.processEpoch]);if(!u.changed){order.accept(ticket,expected);return false}return order.accept(ticket,u.snapshot.revision,u.snapshot.processEpoch)};
 const stream=f.streams[0];stream.emit('reset',{});
 const currentAccepted=await read();assert.equal(currentAccepted,true,'uncached HTTP must accept B');
 stream.emit('snapshot',proof.sse_after_reset);
 const staleAccepted=await read();assert.equal(staleAccepted,false,'late initial SSE A cannot become switch authority');
 assert.equal(order.processEpoch,proof.http_current.snapshot.processEpoch);
 assert.equal(f.requests.length,2,'foreign cached epoch must be confirmed by an authoritative read');
 stream.emit('snapshot',proof.http_progress);
 const currentAgainAccepted=await read();assert.equal(currentAgainAccepted,true,'subsequent correct B must keep advancing');
 assert.equal(order.processEpoch,proof.http_progress.snapshot.processEpoch);
 assert.equal(order.revision,proof.http_progress.snapshot.revision);
 assert(f.requests.every(r=>r.path==='/api/v1/conversation/chat-snapshot'),'no Submit or effect request');
 const result={currentAccepted,staleAccepted,currentAgainAccepted,httpReads:f.requests.length,submits:0,currentEpoch:proof.http_current.snapshot.processEpoch,visibleEpoch:order.processEpoch};
 t.diagnostic(JSON.stringify(result));
 if(process.env.CAELIS_BOT_TEST_EPOCH_RESULT)writeFileSync(process.env.CAELIS_BOT_TEST_EPOCH_RESULT,JSON.stringify(result,null,2)+'\n');
});
