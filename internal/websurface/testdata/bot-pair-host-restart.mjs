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
 const input={id:'',name:'B original',address:c.peer,code:c.code};
 let original,connected;
 if(c.mode==='before'){
  try{await client.transport('backend','ConnectBot',[input]);assert.fail('before-B loss must be unknown')}catch(e){original=e.botRecovery;assert.ok(original?.canAuthorize,'first ordinary pair host exit lost its original proof/recovery');}
  let status=await(await send(new URL('/audit/status',c.origin))).json();assert.equal(status.grants,0);assert.equal(status.quiescent,true,'historical remote uncertainty blocked local update');
  connected=await client.transport('backend','AuthorizeBotConnection',[{originalOperationId:original.operationId,connection:{...input,id:original.profileId,code:status.code,reconnect:true}}]);
 }else{
  connected=await client.transport('backend','ConnectBot',[input]);assert.equal(connected.state,'ready','accepted original pair did not reconcile after host exit');
 }
 assert.equal(connected.state,'ready');assert.equal(writes,1,'ordinary original pair was replayed');
 const before=await(await send(new URL('/audit/status',c.origin))).json();assert.equal(before.grants,1);assert.equal(before.quiescent,true);
 const fresh=await(await send(new URL('/audit/revoke',c.origin))).json();
 const later=await client.transport('backend','ConnectBot',[{...input,id:connected.id,code:fresh.code,reconnect:true}]);assert.equal(later.state,'ready');assert.equal(later.id,connected.id);
 for(const path of ['/audit/status','/audit/reload']){const after=await(await send(new URL(path,c.origin))).json();assert.equal(after.quiescent,true,'later legitimate pairing reactivated a historical update block');assert.equal(after.grants,1);}
 if(original){const receipt=await(await globalThis.fetch('/api/v1/operations/'+original.operationId)).json();assert.equal(receipt.error,'operation_unconfirmed');assert.equal(JSON.parse(saved.get('caelisbot.pending-operations'))['/api/v1/machines/connect-bot'],original.operationId);}
 console.log('first pair '+c.mode+' host-exit receipt recovery and revoke/fresh-pair epoch/reload isolation passed');
}finally{client.close()}
