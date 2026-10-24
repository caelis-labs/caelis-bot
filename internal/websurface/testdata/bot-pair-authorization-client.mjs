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
let client=createWebTransport({csrf:c.csrf,version:'FIXTURE',capabilities:{reason:''}});
try{
 const args={id:'',name:'Original B',address:c.peer,code:c.code};
 let original;
 try{await client.transport('backend','ConnectBot',[args]);assert.fail('first pair must be unknown')}catch(e){assert.equal(e.code,'operation_unconfirmed');original=e.botRecovery;}
 assert.ok(original?.canAuthorize);assert.ok(original.operationId&&original.profileId);
 const pending=JSON.parse(saved.get('caelisbot.pending-operations'));assert.equal(pending['/api/v1/machines/connect-bot'],original.operationId);
 const fresh=await(await send(new URL('/audit/original-proof-recovery',c.origin))).json();assert.equal(fresh.grants,0);assert.equal(fresh.id,original.profileId);
 // Changing ordinary inputs cannot bypass the original receipt.
 await assert.rejects(client.transport('backend','ConnectBot',[{...args,id:fresh.id,code:fresh.code,reconnect:true}]),e=>e.code==='operation_unconfirmed');
 assert.equal(writes,1);
 const request=code=>({originalOperationId:original.operationId,connection:{...args,id:fresh.id,code,reconnect:true}});
 await assert.rejects(client.transport('backend','AuthorizeBotConnection',[request(c.code)]),e=>e.code==='operation_rejected'&&e.detail==='new_pairing_code_required');
 await assert.rejects(client.transport('backend','AuthorizeBotConnection',[request('wrong-fresh-code')]),e=>e.code==='operation_rejected'&&e.detail==='pairing_code_invalid');
 if(c.mode!=='normal')await(await send(new URL('/audit/original-proof-recovery?arm='+c.mode,c.origin))).json().then(v=>fresh.code=v.code);
 let accepted;
 if(c.mode==='before'){
  let child;
  try{await client.transport('backend','AuthorizeBotConnection',[request(fresh.code)]);assert.fail('new authorization response unknown')}catch(e){assert.equal(e.code,'operation_unconfirmed');child=e.botRecovery;}
  assert.ok(child?.operationId&&child.operationId!==original.operationId);
  const fresh2=await(await send(new URL('/audit/original-proof-recovery',c.origin))).json();assert.equal(fresh2.grants,0);
  // A different code/body still cannot replay this uncertain new intent.
  await assert.rejects(client.transport('backend','AuthorizeBotConnection',[request(fresh2.code)]),e=>e.code==='operation_unconfirmed');
  await assert.rejects(client.transport('backend','AuthorizeBotConnection',[{...request(fresh.code),originalOperationId:child.operationId}]),e=>e.detail==='new_pairing_code_required');
  accepted=await client.transport('backend','AuthorizeBotConnection',[{...request(fresh2.code),originalOperationId:child.operationId}]);
 } else accepted=await client.transport('backend','AuthorizeBotConnection',[request(fresh.code)]);assert.equal(accepted.state,'ready');assert.equal(accepted.id,original.profileId);
 const status=await(await globalThis.fetch('/api/v1/operations/'+original.operationId)).json();
 assert.equal(status.error,'operation_unconfirmed');assert.ok(status.followingAuthorization); // old effect never falsely settled
 assert.equal(JSON.parse(saved.get('caelisbot.pending-operations'))['/api/v1/machines/connect-bot'],original.operationId);
 client.close();client=createWebTransport({csrf:c.csrf,version:'FIXTURE',capabilities:{reason:''}});
 const renamed=await client.transport('backend','ConnectBot',[{id:fresh.id,address:c.peer,name:'Renamed B',code:''}]);assert.equal(renamed.name,'Renamed B');
 await assert.rejects(client.transport('backend','ConnectBot',[args]),e=>e.code==='operation_rejected'&&e.detail==='new_pairing_code_required');
 await assert.rejects(client.transport('backend','ConnectBot',[{id:fresh.id,address:c.peer,name:'Renamed B',code:'known-refused-later-code'}]),e=>e.detail==='pairing_code_invalid');
 assert.equal(JSON.parse(saved.get('caelisbot.pending-operations'))['/api/v1/machines/connect-bot'],original.operationId);
 assert(![...saved.values()].join('').includes(c.code));assert(![...saved.values()].join('').includes(fresh.code));
 console.log('Unknown no-grant pairing retained; same code refused; wrong fresh code corrected; explicit fresh authorization and reload/follow-up rename passed.');
}finally{client.close()}
