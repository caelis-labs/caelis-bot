import assert from 'node:assert/strict';
import {test,after} from 'node:test';
import {createServer} from 'vite';
const server=await createServer({server:{middlewareMode:true,hmr:false},appType:'custom'});after(()=>server.close());
const {remoteMachineStatus}=await server.ssrLoadModule('/src/settings/runtime/machine-status.ts');
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
