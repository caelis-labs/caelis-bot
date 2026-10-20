import test from 'node:test';
import assert from 'node:assert/strict';
import {inspectedModels,loadConnections,workerPickerValue,followingWorkerModel,encodeModel} from '../frontend/src/settings/runtime/connections-loader.ts';

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
 assert.equal(events[0],'selection');assert.equal(events.filter(e=>Array.isArray(e))[0][0],'caelis');
 release(ready);await pending;assert.equal(events.filter(e=>Array.isArray(e))[1][0],'codex');assert.ok(!calls.includes('SetupModels'));
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

test('legacy empty Worker models use the named follow-conversation choice for both runtimes',()=>{
 for(const runtime of ['caelis','codex']) {
  const value=workerPickerValue(runtime,{model:'',effort:'high',serviceTier:'priority'},{});
  assert.deepEqual(value,followingWorkerModel);
  assert.notEqual(value.model,encodeModel(runtime,''));
 }
});

test('confirmed unavailable Worker choices fall back without treating loading or offline as removal',()=>{
 const saved={model:'removed',effort:'high',serviceTier:''};
 const view={state:ready,models:[option]};
 assert.deepEqual(workerPickerValue('caelis',saved,{caelis:view}),followingWorkerModel);
 assert.equal(workerPickerValue('caelis',saved,{}).model,encodeModel('caelis','removed'));
 assert.equal(workerPickerValue('caelis',saved,{caelis:{...view,state:{state:'offline'}}}).model,encodeModel('caelis','removed'));
 assert.equal(workerPickerValue('caelis',{...saved,model:'test'},{caelis:view}).model,encodeModel('caelis','test'));
 assert.deepEqual(workerPickerValue('caelis',{...saved,model:'test',effort:'unsupported'},{caelis:view}),followingWorkerModel);
});

test('settings reads back a repaired Worker default after catalog inspection',async()=>{
 let inspected=false;
 const choices=[];
 await loadConnections(async(method,id)=>{
  if(method==='LocalWorkerSelection')return inspected?{runtime:'conversation',work:{model:''}}:{runtime:'codex',work:{model:'removed'}};
  if(method==='SetupProfile')return {runtime:id};
  if(method==='InspectSetup'){inspected=true;return ready;}
  return {};
 },values=>values[2]&&choices.push(values[2].value.runtime),()=>{});
 assert.deepEqual(choices,['codex','conversation']);
});

test('one slow preference cannot hide the other saved selections',async()=>{
 let release;const gate=new Promise(resolve=>{release=resolve});const seen=[];
 const work=loadConnections(async method=>method==='LocalWorkerSelection'?gate:method==='ExecutionSettings'?{model:'saved'}:{active:'codex',hasRuntimeChoice:true},v=>seen.push(v),()=>{},false);
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(seen.at(-1)[1].value.model,'saved');assert.equal(seen.at(-1)[2],undefined);
 release({runtime:'conversation',work:{model:''}});await work;
 assert.equal(seen.at(-1)[2].value.runtime,'conversation');
});
test('retry inspects only the failing runtime and never activates or reconnects',async()=>{
 const calls=[],updates=[];
 await loadConnections(async(method,id)=>{calls.push([method,id]);if(method==='SetupProfile')return {runtime:id};if(method==='InspectSetup')return ready;return {};},()=>{},id=>updates.push(id),'codex');
 assert.deepEqual(updates,['codex']);assert.deepEqual(calls.filter(([m])=>m==='SetupProfile'),[['SetupProfile','codex']]);
 assert.ok(calls.every(([m])=>!['Reconnect','ActivateRuntime','ApplySetup'].includes(m)));
});
