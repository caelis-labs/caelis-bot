import {test} from 'node:test';
import assert from 'node:assert/strict';
import {recoverRead} from '../frontend/src/read-recovery.ts';
test('a transient catalog read recovers without an error projection or effect replay',async()=>{
 let reads=0;const delays=[];
 const result=await recoverRead(async()=>{if(++reads<3)throw Error('PRIVATE_SENTINEL');return ['saved model'];},undefined,async ms=>{delays.push(ms)});
 assert.deepEqual(result,['saved model']);assert.equal(reads,3);assert.deepEqual(delays,[300,1200]);
});
test('permanent failures exhaust the read budget without leaking the cause',async()=>{
 let reads=0;
 await assert.rejects(recoverRead(async()=>{reads++;throw Error('PRIVATE_SENTINEL')},undefined,async()=>{}),e=>e.message==='read_unavailable');
 assert.equal(reads,3);
});
test('closing during recovery prevents later reads and delivery',async()=>{
 const controller=new AbortController();let reads=0;
 await assert.rejects(recoverRead(async()=>{reads++;throw Error('offline')},controller.signal,async()=>controller.abort()));
 assert.equal(reads,1);
 let resolve;const c=new AbortController();
 const pending=recoverRead(()=>new Promise(r=>resolve=r),c.signal);c.abort();resolve('stale');await assert.rejects(pending);
});
