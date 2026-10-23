import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {stripTypeScriptTypes} from 'node:module';

// Run the actual browser transport with only TypeScript syntax removed. The
// browser APIs and authenticated host response are fixtures, never a live Bot.
const sourceURL=new URL('../frontend/src/web-transport.ts',import.meta.url);
const source=stripTypeScriptTypes(readFileSync(sourceURL,'utf8'),{mode:'transform'})
 .replace(/from '([.][^']+)'/g,(_,path)=>`from '${new URL(path.endsWith('.ts')?path:path+'.ts',sourceURL).href}'`);
const {createWebTransport}=await import('data:text/javascript;base64,'+Buffer.from(source+'\n//# sourceURL=web-transport.fixture.mjs').toString('base64'));

function browser(t,read){
 const requests=[],opened=[],events=new EventTarget(),saved=new Map();
 t.mock.method(globalThis,'fetch',async(path,init)=>{
  requests.push({path,body:JSON.parse(init.body)});
  const result=read(path);
  return new Response(JSON.stringify(result.body),{status:result.status??200,headers:{'Content-Type':'application/json'}});
 });
 const globals={window:{addEventListener:events.addEventListener.bind(events),removeEventListener:events.removeEventListener.bind(events),dispatchEvent:events.dispatchEvent.bind(events),open:(...args)=>opened.push(args)},
  document:{addEventListener(){},removeEventListener(){}},sessionStorage:{getItem:key=>saved.get(key)??null,setItem:(key,value)=>saved.set(key,value)}};
 const descriptors=Object.fromEntries(Object.keys(globals).map(name=>[name,Object.getOwnPropertyDescriptor(globalThis,name)]));
 for(const [name,value]of Object.entries(globals))Object.defineProperty(globalThis,name,{value,configurable:true,writable:true});
 const client=createWebTransport({csrf:'fixture-csrf',version:'FIXTURE',capabilities:{reason:''}});
 t.after(()=>{client.close();for(const [name,descriptor]of Object.entries(descriptors)){if(descriptor)Object.defineProperty(globalThis,name,descriptor);else delete globalThis[name]}});
 return {client,requests,opened};
}

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
