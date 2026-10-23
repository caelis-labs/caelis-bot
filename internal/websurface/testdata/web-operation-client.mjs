import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {stripTypeScriptTypes} from 'node:module';

const configuration=JSON.parse(readFileSync(0,'utf8'));
const sourceURL=new URL('../../../frontend/src/web-transport.ts',import.meta.url);
const source=stripTypeScriptTypes(readFileSync(sourceURL,'utf8'),{mode:'transform'})
 .replace(/from '([.][^']+)'/g,(_,path)=>`from '${new URL(path.endsWith('.ts')?path:path+'.ts',sourceURL).href}'`);
const {createWebTransport}=await import('data:text/javascript;base64,'+Buffer.from(source).toString('base64'));
const send=globalThis.fetch,events=new EventTarget(),saved=new Map(),requests=[];
globalThis.fetch=async(path,init={})=>{
 const headers=new Headers(init.headers);headers.set('Origin',configuration.origin);headers.set('Cookie',configuration.cookie);
 if(init.method==='POST')requests.push({path,body:JSON.parse(init.body)});
 return await send(new URL(path,configuration.origin),{...init,headers});
};
globalThis.window={addEventListener:events.addEventListener.bind(events),removeEventListener:events.removeEventListener.bind(events),dispatchEvent:events.dispatchEvent.bind(events)};
globalThis.document={addEventListener(){},removeEventListener(){}};
globalThis.sessionStorage={getItem:key=>saved.get(key)??null,setItem:(key,value)=>saved.set(key,value)};
const client=createWebTransport({csrf:configuration.csrf,version:'FIXTURE',capabilities:{reason:''}});
try{
 const base={id:'',name:'Fixture B',address:'http://127.0.0.1:12345',code:'wrong-fixture'};
 await assert.rejects(client.transport('backend','ConnectBot',[base]),error=>{
  assert.equal(error.code,'operation_rejected');assert.equal(error.detail,'pairing_code_invalid');assert.match(error.message,/pairing_code_invalid/);return true;
 });
 const connected=await client.transport('backend','ConnectBot',[{...base,code:'correct-fixture'}]);
 assert.equal(connected.id,'original-fixture-peer');
 await assert.rejects(client.transport('backend','SaveWorkerSharing',[{listen:'invalid-fixture',address:''}]),error=>{
  assert.equal(error.code,'operation_rejected');assert.equal(error.detail,'worker_listener_invalid');return true;
 });
 const listener=await client.transport('backend','SaveWorkerSharing',[{listen:'127.0.0.1:0',address:''}]);
 assert.equal(listener.listen,'127.0.0.1:0');
 const lost=await client.transport('backend','ConnectBot',[{...base,code:'lost-fixture'}]);
 assert.equal(lost.id,connected.id);
 const writes=()=>requests.filter(r=>r.path==='/api/v1/machines/connect-bot');
 assert.equal(writes().length,3,'lost response must query the original Web operation, never resend');
 assert.notEqual(writes()[0].body.id,writes()[1].body.id,'a corrected known refusal has a new explicit intent');
 await assert.rejects(client.transport('backend','ConnectBot',[{...base,code:'uncertain-fixture'}]),error=>{
  assert.equal(error.code,'operation_unconfirmed');assert.equal(error.detail,undefined);return true;
 });
 const before=JSON.parse(saved.get('caelisbot.pending-operations'));
 await assert.rejects(client.transport('backend','ConnectBot',[{...base,code:'correct-fixture'}]),error=>error.code==='operation_unconfirmed');
 assert.equal(writes().length,4,'edited content cannot replay or replace an unknown operation');
 assert.deepEqual(JSON.parse(saved.get('caelisbot.pending-operations')),before,'unknown retains its original Web operation ID');
 assert(![...saved.values()].join('').includes('fixture'),'browser storage must not retain request arguments or pairing material');
 console.log('actual TypeScript transport + authenticated HTTP handler: correction, original lost-response receipt, and unknown proof passed');
}finally{client.close();}
