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
