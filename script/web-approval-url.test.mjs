import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
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
  const result=read(path);
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
 const f=browser(t,()=>{throw Error('unexpected HTTP read');});
 f.client.start();const stream=f.streams[0],order=new ConversationOrder();
 const snapshot={revision:120,botStatus:'',processEpoch:'old',items:[]};
 const read=()=>f.client.transport('backend','ChatSnapshot',[order.revision,'',order.processEpoch]);
 stream.emit('snapshot',{changed:true,snapshot});
 let update=await read();assert.equal(order.accept(order.request(),update.snapshot.revision,update.snapshot.processEpoch),true);
 assert.equal((await read()).changed,false);
 stream.emit('snapshot',{changed:true,snapshot:{...snapshot,processEpoch:'new',items:[{text:'first reply'}]}});
 update=await read();assert.equal(update.changed,true);
 assert.equal(order.accept(order.request(),update.snapshot.revision,update.snapshot.processEpoch),true);
 assert.equal(update.snapshot.items[0].text,'first reply');
 assert.equal((await read()).changed,false);
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
