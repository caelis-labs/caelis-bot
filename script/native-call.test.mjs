import {test} from 'node:test';
import assert from 'node:assert/strict';
import {nativeCall} from '../frontend/src/native-call.ts';

test('a human-driven picker survives the RPC deadline and returns its original selection once',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 let finish,calls=0,settled=false;
 const selection=[{id:'original-selected-file'}];
 const pending=nativeCall('desktop','PickFiles',()=>{calls++;return new Promise(resolve=>finish=resolve);});
 pending.then(()=>settled=true);
 t.mock.timers.tick(120_000);await Promise.resolve();
 assert.equal(settled,false);assert.equal(calls,1);
 finish(selection);assert.equal(await pending,selection);assert.equal(calls,1);
});
test('delayed picker cancellation keeps its native result without reopening the chooser',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 let finish,calls=0;
 const pending=nativeCall('desktop','PickFiles',()=>{calls++;return new Promise(resolve=>finish=resolve);});
 t.mock.timers.tick(60_000);finish([]);
 assert.deepEqual(await pending,[]);assert.equal(calls,1);
});
test('other host effects remain bounded and an unknown outcome is never replayed',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 let calls=0,finish;
 const pending=nativeCall('backend','Submit',()=>{calls++;return new Promise(resolve=>finish=resolve);});
 const rejection=assert.rejects(pending,/not been confirmed/);
 t.mock.timers.tick(40_000);await rejection;
 finish('late result');await Promise.resolve();assert.equal(calls,1);
});
test('preview reads retain the bounded read deadline',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 const pending=nativeCall('desktop','DraftImagePreview',()=>new Promise(()=>{}));
 const rejection=assert.rejects(pending,/not been confirmed/);
 t.mock.timers.tick(5_000);await rejection;
});
test('runtime preparation waits for the native installation result beyond the ordinary deadline',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 let finish,calls=0,settled=false;
 const pending=nativeCall('backend','ApplySetup',()=>{calls++;return new Promise(resolve=>finish=resolve);});
 pending.then(()=>settled=true);
 t.mock.timers.tick(120_000);await Promise.resolve();
 assert.equal(settled,false);assert.equal(calls,1);
 finish({state:'models'});assert.deepEqual(await pending,{state:'models'});
});
test('fresh installation stays pending while a slow transfer is still active',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});
 let finish,settled=false;
 const pending=nativeCall('backend','ApplySetup',()=>new Promise(resolve=>finish=resolve),[{action:'install',settings:{runtime:'caelis'}}]);
 pending.then(()=>settled=true);
 t.mock.timers.tick(2*60*60*1000);await Promise.resolve();
 assert.equal(settled,false);
 finish({state:'models'});assert.deepEqual(await pending,{state:'models'});
});
