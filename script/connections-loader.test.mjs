import test from 'node:test';
import assert from 'node:assert/strict';
import {inspectedModels,loadConnections} from '../frontend/src/settings/runtime/connections-loader.ts';

const option={model:'test',name:'Test',efforts:['low','high'],defaultEffort:'high',serviceTiers:[],imageInput:true};
const ready={state:'ready',models:[{value:'test',noAuth:false,parameters:option}]};
test('return navigation reads fresh selections without repeating a recent runtime probe',async()=>{
 const calls=[];
 await loadConnections(async method=>{calls.push(method);return {};},()=>{},()=>assert.fail('unexpected inspection'),false);
 assert.deepEqual(calls,['SetupOverview','ExecutionSettings','LocalWorkerSelection']);
});
test('inspection reuses authoritative parameters and excludes unauthenticated models',()=>{
 assert.deepEqual(inspectedModels({...ready,models:[...ready.models,{value:'unavailable',noAuth:true}]}),[option]);
 assert.deepEqual(inspectedModels({...ready,state:'incompatible'}),[]);
 assert.equal(inspectedModels({...ready,models:[{value:'old',noAuth:false}]}),null);
});
test('selection renders before runtime inspection; each runtime completes independently',async()=>{
 const calls=[],events=[];let release;
 const codex=new Promise(resolve=>{release=resolve;});
 const call=async(method,id)=>{
  calls.push(method);
  if(method==='SetupProfile')return {runtime:id};
  if(method==='InspectSetup')return id.runtime==='codex'?codex:ready;
  if(method==='SetupModels')throw Error('unnecessary duplicate inspection');
  return {};
 };
 const pending=loadConnections(call,()=>events.push('selection'),(id,result)=>events.push([id,result]));
 await new Promise(resolve=>setImmediate(resolve));
 assert.deepEqual(calls.slice(0,3),['SetupOverview','ExecutionSettings','LocalWorkerSelection']);
 assert.equal(events[0],'selection');assert.equal(events[1][0],'caelis');assert.equal(events.length,2);
 release(ready);await pending;assert.equal(events[2][0],'codex');assert.ok(!calls.includes('SetupModels'));
});
test('selection failures remain explicit and an older adapter gets a catalog fallback',async()=>{
 let values;const runtime=[];let fallbacks=0;
 await loadConnections(async(method,id)=>{
  if(method==='ExecutionSettings')throw Error('offline');
  if(method==='SetupProfile')return {runtime:id};
  if(method==='InspectSetup'){if(id.runtime==='codex')throw Error('probe failed');return {...ready,models:[{value:'old',noAuth:false}]};}
  if(method==='SetupModels'){fallbacks++;return [option];}return {};
 },v=>{values=v;},(id,result)=>runtime.push([id,result]));
 assert.equal(values[1].status,'rejected');assert.equal(fallbacks,1);
 assert.ok(runtime.find(([id])=>id==='codex')[1] instanceof Error);
 assert.deepEqual(runtime.find(([id])=>id==='caelis')[1].models,[option]);
});
