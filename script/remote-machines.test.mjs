import assert from 'node:assert/strict';
import {test,after} from 'node:test';
import {createServer} from 'vite';
const server=await createServer({server:{middlewareMode:true,hmr:false},appType:'custom'});after(()=>server.close());
const {remoteMachineStatus}=await server.ssrLoadModule('/src/settings/runtime/machine-status.ts');
const {botAddress,botProfileAt}=await server.ssrLoadModule('/src/settings/runtime/bot-address.ts');
const ready={state:'ready',checkedAt:100000,issue:'',connectionType:'bot'};
test('only fresh authorization and native admission show online',()=>{
 assert.equal(remoteMachineStatus(ready,100001),'online');
 for(const checkedAt of [0,undefined,1,200000])assert.equal(remoteMachineStatus({...ready,checkedAt},100001),'unchecked');
 for(const issue of ['peer_authorization_revoked','peer_authentication_required','original_peer_proof_required'])assert.equal(remoteMachineStatus({...ready,issue},100001),'authorization');
 for(const issue of ['worker_not_ready','bot_worker_contract_incompatible','worker_sharing_disabled'])assert.equal(remoteMachineStatus({...ready,issue},100001),'setup');
 assert.equal(remoteMachineStatus({...ready,state:'offline'},100001),'offline');
 assert.equal(remoteMachineStatus({...ready,archived:true},100001),'offline');
 assert.equal(remoteMachineStatus({...ready,connectionType:'ssh'},100001),'online');
 assert.equal(remoteMachineStatus({...ready,connectionType:'ssh',state:'trust'},100001),'authorization');
});
test('IP connection keeps actual copied ports, optional port and unsafe inputs visible to host validation',()=>{
 assert.equal(botAddress('100.64.0.2'),'http://100.64.0.2:9317');
 assert.equal(botAddress('100.64.0.2','29317'),'http://100.64.0.2:29317');
 assert.equal(botAddress('http://100.64.0.2:39317'),'http://100.64.0.2:39317');
 assert.equal(botAddress('https://bot.example'),'https://bot.example');
 assert.equal(botAddress('https://secret@bot.example/private?token=1'),'https://secret@bot.example/private?token=1');
});
test('nondefault separate port reuses only the original Bot profile and proof route',()=>{
 const original={id:'original',connectionType:'bot',address:'http://100.64.0.2:29317'};
 assert.equal(botProfileAt([{id:'ssh',connectionType:'ssh',address:original.address},original],'100.64.0.2','29317')?.id,'original');
 assert.equal(botProfileAt([original],'100.64.0.2'),undefined);
});
