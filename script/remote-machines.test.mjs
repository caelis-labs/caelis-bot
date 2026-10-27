import assert from 'node:assert/strict';
import {test,after} from 'node:test';
import {createServer} from 'vite';
import React from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
const server=await createServer({server:{middlewareMode:true,hmr:false},appType:'custom'});after(()=>server.close());
const {remoteMachineStatus,remoteMachineLabel}=await server.ssrLoadModule('/src/settings/runtime/machine-status.ts');
const {botAddress,botAddressPort,botProfileAt}=await server.ssrLoadModule('/src/settings/runtime/bot-address.ts');
const {pairingProblem,connectionProblem,incomingProblem}=await server.ssrLoadModule('/src/settings/runtime/pairing-errors.ts');
const {peerSourceLabel}=await server.ssrLoadModule('/src/settings/runtime/peer-source-label.ts');
const {MachineConnectionList}=await server.ssrLoadModule('/src/settings/runtime/MachineConnectionList.tsx');
const {mutateMachine,pendingMachineMutation}=await server.ssrLoadModule('/src/settings/runtime/machine-mutation.ts');
const {sshAddress,sshTarget}=await server.ssrLoadModule('/src/settings/runtime/SSHConnection.tsx');
const storage=new Map();globalThis.localStorage={getItem:key=>storage.get(key)||null,setItem:(key,v)=>storage.set(key,v),removeItem:key=>storage.delete(key)};
const ready={state:'ready',checkedAt:100000,issue:'',connectionType:'bot',pairingStatus:'approved'};
test('only fresh authorization and native admission show online',()=>{
 assert.equal(remoteMachineStatus(ready,100001),'online');
 for(const checkedAt of [0,undefined,1,200000])assert.equal(remoteMachineStatus({...ready,checkedAt},100001),'unchecked');
 for(const issue of ['peer_authorization_revoked','peer_authentication_required','original_peer_proof_required'])assert.equal(remoteMachineStatus({...ready,issue},100001),'authorization');
 for(const issue of ['worker_not_ready','bot_worker_contract_incompatible'])assert.equal(remoteMachineStatus({...ready,issue},100001),'setup');
 assert.equal(remoteMachineStatus({...ready,state:'offline'},100001),'offline');
 assert.equal(remoteMachineStatus({...ready,archived:true},100001),'offline');
 for(const pairingStatus of ['pending','unknown','rejected','expired','revoked'])assert.equal(remoteMachineStatus({...ready,pairingStatus},100001),'authorization');
});
test('stale availability keeps its observation meaning without claiming current online status',()=>{
 const stale={...ready,checkedAt:Date.now()-61000};
 assert.equal(remoteMachineStatus(stale),'unchecked');
 assert.equal(remoteMachineLabel(stale),'settings.remoteLastOnline');
 assert.equal(remoteMachineLabel({...ready,checkedAt:0}),'settings.remoteUnchecked');
});
test('IP connection keeps explicit ports, optional port and unsafe inputs visible to host validation',()=>{
 assert.equal(botAddress('100.64.0.2'),'http://100.64.0.2:9317');
 assert.equal(botAddress('100.64.0.2','29317'),'http://100.64.0.2:29317');
 assert.equal(botAddress('http://100.64.0.2:39317'),'http://100.64.0.2:39317');
 assert.equal(botAddress('https://bot.example'),'https://bot.example');
 assert.equal(botAddress('https://secret@bot.example/private?token=1'),'https://secret@bot.example/private?token=1');
});
test('visible port matches bare host, pasted endpoint and explicit protocol defaults',()=>{
 assert.equal(botAddressPort(''),'9317');
 assert.equal(botAddressPort('fedora'),'9317');
 assert.equal(botAddressPort('http://127.0.0.1:19329'),'19329');
 assert.equal(botAddressPort('https://bot.example'),'443');
 assert.equal(botAddressPort('http://bot.example'),'80');
});
test('nondefault separate port reuses only the original Bot profile and proof route',()=>{
 const original={id:'original',connectionType:'bot',address:'http://100.64.0.2:29317'};
 assert.equal(botProfileAt([original],'100.64.0.2'),undefined);
});
test('pairing diagnoses classify only safe reasons and route actual settings problems',()=>{
 assert.deepEqual(pairingProblem('worker_effort_unsupported'),{key:'settings.pairEffortUnsupported',settings:true});
 assert.deepEqual(pairingProblem('worker_login_required'),{key:'settings.pairLoginRequired',settings:true});
 assert.equal(pairingProblem('worker_runtime_unavailable').key,'settings.pairRuntimeUnavailable');
 assert.equal(pairingProblem('worker_readiness_timeout').settings,false);
 assert.equal(pairingProblem('private native path/token').key,'settings.remoteDecisionUnconfirmed');
 for(const reason of ['constructor','toString','__proto__'])assert.deepEqual(pairingProblem(reason),{key:'settings.remoteDecisionUnconfirmed',settings:false});
});

test('pending, unknown and known terminal pairing states never collapse to online or each other',()=>{
 for(const [pairingStatus,key] of Object.entries({pending:'remotePending',unknown:'remoteUnconfirmed',rejected:'remoteRejected',expired:'remoteExpired',revoked:'remoteRevoked'}))assert.equal(remoteMachineLabel({...ready,pairingStatus}),'settings.'+key);
 assert.equal(connectionProblem('private native path/token').key,'settings.remoteCheckConnection');
 assert.equal(incomingProblem('worker_peer_state_unsupported'),'settings.remoteStateVersion');
 assert.equal(incomingProblem('worker_peer_state_permission'),'settings.remoteStatePermission');
 assert.equal(incomingProblem('worker_listener_unavailable'),'settings.remoteReceptionUnavailable');
});

test('same-name inbound grants remain distinguishable, including shared short prefixes',()=>{
 const sources=[{id:'12345678a'+ '0'.repeat(55),name:'fedora'},{id:'12345678b'+'0'.repeat(55),name:'fedora'},{id:'fed01234'+'0'.repeat(56),name:'fedora'}];
 const labels=sources.map(source=>peerSourceLabel(source,sources,'Connected machine'));
 assert.deepEqual(labels,['fedora · 12345678a','fedora · 12345678b','fedora · fed01234']);
 assert.deepEqual(sources.map(source=>peerSourceLabel(source,[...sources].reverse(),'Connected machine')),labels,'identity labels do not depend on row order');
 const html=renderToStaticMarkup(React.createElement(MachineConnectionList,{machines:[],sources,requests:[],unread:{},sourcesUnread:false,onMachine:()=>{},onMachineMenu:()=>{},onSource:()=>{},onSourceMenu:()=>{},onRequest:()=>{}}));
 for(const label of labels){assert.ok(html.includes(`<strong>${label}</strong>`));assert.ok(html.includes(`aria-label="Manage ${label}"`),'menu identifies the same original grant');}
});

test('SSH destinations keep the two ports and only an opaque key reference',()=>{
 const value={host:'2001:db8::1',port:19329,sshPort:2222,user:'root',authentication:'key',identity:'a'.repeat(64)};
 const address=sshAddress(value);assert.deepEqual(sshTarget(address),value);assert.ok(!address.includes('/Users/'));
 assert.ok(address.includes('authentication=key&identity='),'canonical query order matches the service');
});
test('definite local rejection stays a failure and does not trigger readback',async()=>{
 const methods=[];await assert.rejects(mutateMachine(async(method,input)=>{methods.push(method);return {id:input.id,status:'failed',code:'invalid_bot_name'}},{id:'invalid-name',machineId:'invalid',action:'save',name:''}),error=>error.code==='invalid_bot_name'&&!error.uncertain);
 assert.deepEqual(methods,['ManageMachine']);assert.equal(pendingMachineMutation('invalid','save'),undefined);
});
test('lost renderer response reads the original committed receipt without another write',async()=>{
 const calls=[];const machine={id:'one',name:'Renamed'};const result=await mutateMachine(async(method,input)=>{
  calls.push([method,input]);if(method==='ManageMachine'){assert.equal(pendingMachineMutation('one','save'),'save-once','record before dispatch');throw Error('bridge response lost')}
  return {id:input,status:'succeeded',machine};
 },{id:'save-once',machineId:'one',action:'save',name:'Renamed'});
 assert.equal(result.machine,machine);assert.deepEqual(calls.map(v=>v[0]),['ManageMachine','MachineMutation']);assert.equal(calls[1][1],'save-once');assert.equal(pendingMachineMutation('one','save'),undefined);
});
test('unknown survives reopening and checks the original remove instead of submitting again',async()=>{
 let writes=0,readable=false;const reads=[];const call=async(method,input)=>{if(method==='ManageMachine'){writes++;throw Error('lost')}reads.push(input);return {id:input,status:readable?'succeeded':'unknown',machine:{id:'two',archived:readable}}};
 await assert.rejects(mutateMachine(call,{id:'remove-original',machineId:'two',action:'remove'}),error=>error.uncertain);
 assert.equal(pendingMachineMutation('two','remove'),'remove-original');readable=true;
 const result=await mutateMachine(call,{id:'must-not-send',machineId:'two',action:'remove'});
 assert.equal(writes,1);assert.ok(reads.every(id=>id==='remove-original'));assert.equal(result.machine.archived,true);
});
test('legacy entries and saved endpoints do not masquerade as unsent requests',()=>{
 assert.equal(remoteMachineLabel({state:'legacy',pairingStatus:''}),'settings.remoteLegacy');
 assert.equal(remoteMachineLabel({...ready,savedAddress:'http://new.test:9317'}),'settings.remoteSavedAddress');
});

test('legacy bare Host remains SSH while explicit saved addresses determine the next channel',()=>{
 assert.deepEqual(sshTarget('fedora',true),{host:'fedora',port:9317});
 assert.equal(sshTarget('fedora'),undefined);
 assert.equal(sshTarget('http://fedora.test:9317',true),undefined);
 assert.deepEqual(sshTarget('ssh://fedora:19329',true),{host:'fedora',port:19329});
});

test('invalid Bot ports never silently change the target',()=>{
 for(const port of ['0','65536','99999','-1','1.5','nope'])for(const host of ['fedora','http://fedora','http://fedora:19329'])assert.throws(()=>botAddress(host,port),/invalid_bot_port/,`${host} ${port}`);
 for(const value of ['http://fedora:65536','fedora:99999','https://fedora:0'])assert.throws(()=>botAddress(value),/invalid_bot_port/);
 for(const port of ['1','65535'])assert.equal(new URL(botAddress('http://fedora:19329',port)).port,port);
 assert.equal(new URL(botAddress('http://fedora:19329')).port,'19329');
});
