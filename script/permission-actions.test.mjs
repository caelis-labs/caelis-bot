import test from 'node:test';
import assert from 'node:assert/strict';
import {changeSystemPermission} from '../frontend/src/permission-actions.ts';

test('off switch preserves requested or authorized outcomes without opening settings',async()=>{
 for(const state of ['requested','authorized']) {
  const calls=[];
  const result=await changeSystemPermission(async(method,id)=>{calls.push([method,id]);return {state};},'screenCapture',false);
  assert.deepEqual(calls,[['RequestSystemPermission','screenCapture']]);
  assert.deepEqual(result,{state}); // No optimistic local grant or immediate Settings jump.
 }
});
test('completed refusal opens settings once, without granting or resetting access',async()=>{
 for(const id of ['accessibility','screenCapture','automation','notifications']) {
  const calls=[];
  const result=await changeSystemPermission(async(method,arg)=>{calls.push([method,arg]);return {state:'settingsRequired'};},id,false);
  assert.deepEqual(calls,[['RequestSystemPermission',id],['OpenSystemPermissionSettings',id]]);
  assert.deepEqual(result,{state:'settingsOpened'});
 }
});
test('pending native consent is not interrupted by settings navigation',async()=>{
 const calls=[];
 let finish;
 const pending=new Promise(resolve=>{finish=resolve;});
 const change=changeSystemPermission(async(method,id)=>{calls.push([method,id]);return pending;},'screenCapture',false);
 await Promise.resolve();
 assert.deepEqual(calls,[['RequestSystemPermission','screenCapture']]);
 finish({state:'authorized'});
 assert.deepEqual(await change,{state:'authorized'});
 assert.equal(calls.length,1);
});
test('settings navigation failure remains visible instead of claiming success',async()=>{
 const calls=[];
 await assert.rejects(changeSystemPermission(async(method,id)=>{
  calls.push([method,id]);
  if(method==='OpenSystemPermissionSettings')throw Error('settings unavailable');
  return {state:'settingsRequired'};
 },'screenCapture',false),/settings unavailable/);
 assert.equal(calls.length,2);
});
test('on switch opens OS settings for revocation and never resets permissions',async()=>{
 for(const id of ['accessibility','screenCapture','automation','notifications']) {
  const calls=[];
  const result=await changeSystemPermission(async(...args)=>{calls.push(args);},id,true);
  assert.deepEqual(calls,[['OpenSystemPermissionSettings',id]]);
  assert.deepEqual(result,{state:'settingsOpened'});
 }
});
test('native failure propagates without fallback request or system settings jump',async()=>{
 const calls=[];
 await assert.rejects(changeSystemPermission(async(...args)=>{calls.push(args);throw Error('request failed');},'screenCapture',false));
 assert.deepEqual(calls,[['RequestSystemPermission','screenCapture']]);
});
