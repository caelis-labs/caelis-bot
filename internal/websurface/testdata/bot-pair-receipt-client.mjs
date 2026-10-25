import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs'; import {stripTypeScriptTypes} from 'node:module';
const c=JSON.parse(readFileSync(0,'utf8'));
const sourceURL=new URL('../../../frontend/src/web-transport.ts',import.meta.url);
const source=stripTypeScriptTypes(readFileSync(sourceURL,'utf8'),{mode:'transform'}).replace(/from '([.][^']+)'/g,(_,path)=>`from '${new URL(path.endsWith('.ts')?path:path+'.ts',sourceURL).href}'`);
const {createWebTransport}=await import('data:text/javascript;base64,'+Buffer.from(source).toString('base64'));
const send=globalThis.fetch,events=new EventTarget(),saved=new Map();
let writes=0;
globalThis.fetch=async(path,init={})=>{const h=new Headers(init.headers);h.set('Origin',c.origin);h.set('Cookie',c.cookie);if(path==='/api/v1/machines/connect-bot')writes++;return send(new URL(path,c.origin),{...init,headers:h});};
globalThis.window={addEventListener:events.addEventListener.bind(events),removeEventListener:events.removeEventListener.bind(events),dispatchEvent:events.dispatchEvent.bind(events)};
globalThis.document={addEventListener(){},removeEventListener(){}};
globalThis.sessionStorage={getItem:k=>saved.get(k)??null,setItem:(k,v)=>saved.set(k,v)};
const client=createWebTransport({csrf:c.csrf,version:'FIXTURE',capabilities:{reason:''}});
try{
 const args={id:'',name:'Original B',address:c.peer,code:c.code};
 await assert.rejects(client.transport('backend','ConnectBot',[args]),e=>e.code==='operation_unconfirmed');
 const recovered=await(await send(new URL('/audit/original-proof-recovery',c.origin))).json();assert.equal(recovered.state,'ready');
 console.log('B accepted pair once; original stored proof independently reconciled to ready.');
 let blocked;try{await client.transport('backend','ConnectBot',[{...args,id:recovered.id,code:'',reconnect:true}]);}catch(e){blocked=e.code}
 console.log(JSON.stringify({blocked,writes,originalPending:JSON.parse(saved.get('caelisbot.pending-operations'))}));
 assert.notEqual(blocked,'operation_unconfirmed','Web cannot reconcile original pairing after lower owner confirmed its proof; every future ConnectBot remains blocked');
 const value=await client.transport('backend','ConnectBot',[{...args,id:recovered.id,code:'',reconnect:true}]);
 assert.equal(value.state,'ready');assert.equal(writes,2);
 assert.deepEqual(JSON.parse(saved.get('caelisbot.pending-operations')),{});
 console.log('Original Web operation settled from original proof; new explicit action works without replaying pair.');
}finally{client.close()}
