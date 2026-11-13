import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {captureDocumentReader,captureImage,confirmCapture} from '../frontend/src/capture-recovery.ts';
const flush=()=>new Promise(resolve=>setImmediate(resolve));
const doc=(presentation=1,outcome='draft')=>({id:'original',presentation,outcome,image:'/owned/image',width:40,height:30});
const deferred=()=>{let resolve;const promise=new Promise(r=>resolve=r);return {promise,resolve};};
async function retry(t){t.mock.timers.tick(300);await flush();t.mock.timers.tick(1200);await flush();}
function images(t,load){
 const previous=globalThis.Image,created=[];
 globalThis.Image=class {set src(value){if(value){created.push(this);queueMicrotask(()=>load(this,created.length));}}};
 t.after(()=>{if(previous===undefined)delete globalThis.Image;else globalThis.Image=previous;});
 return created;
}
test('document reads recover within three attempts without transient errors',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});let calls=0;const accepted=[],errors=[];
 const reader=captureDocumentReader('original',async()=>{if(++calls<3)throw Error('private');return doc();},d=>accepted.push(d),e=>errors.push(e));
 const pending=reader.refresh();await flush();await retry(t);await pending;
 assert.equal(calls,3);assert.deepEqual(accepted,[doc()]);assert.deepEqual(errors,[false]);reader.stop();
});
test('persistent read failure stops even when more receipt events arrive',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});let calls=0;const errors=[];
 const reader=captureDocumentReader('original',async()=>{calls++;throw Error('private');},()=>assert.fail('failed read accepted'),e=>errors.push(e));
 const rejection=assert.rejects(reader.refresh(),/capture_unavailable/);await flush();await retry(t);await rejection;
 t.mock.timers.tick(60_000);await assert.rejects(reader.refresh());assert.equal(calls,3);assert.deepEqual(errors,[true]);reader.stop();
});
test('receipt refreshes serialize and discard stale reads, including an old draft before an unknown send receipt',async()=>{
 const old=deferred();let calls=0;const accepted=[];
 const reader=captureDocumentReader('original',()=>++calls===1?old.promise:Promise.resolve(doc(2,'unknown')),d=>accepted.push(d),()=>{});
 const first=reader.refresh(),second=reader.refresh();assert.equal(first,second);assert.equal(calls,1);
 old.resolve(doc(1));await second;assert.equal(calls,2);assert.deepEqual(accepted,[doc(2,'unknown')]);
 await reader.refresh();assert.equal(accepted.at(-1).outcome,'unknown');reader.stop();
});
test('an older presentation and a cancelled late read cannot replace current pixels or receipt',async()=>{
 const late=deferred(),accepted=[];let calls=0;
 const reader=captureDocumentReader('original',()=>Promise.resolve(++calls===1?doc(3):doc(2)),d=>accepted.push(d),()=>{});
 await reader.refresh();await reader.refresh();assert.deepEqual(accepted,[doc(3)]);reader.stop();
 const cancelled=captureDocumentReader('original',()=>late.promise,d=>accepted.push(d),()=>assert.fail('cancel became error'));
 const rejection=assert.rejects(cancelled.refresh());cancelled.stop();await rejection;late.resolve(doc(9));await flush();assert.deepEqual(accepted,[doc(3)]);
});
test('image decode errors recover, clean old handlers and keep the original URL',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});const created=images(t,(image,n)=>n<3?image.onerror?.():image.onload?.());
 const controller=new AbortController(),pending=captureImage('/owned/image',controller.signal);await flush();await retry(t);
 assert.equal(await pending,created[2]);assert.equal(created.length,3);assert.ok(created.every(im=>im.onload===null&&im.onerror===null));
});
test('hung image loads stop at the finite budget; cancellation prevents old decode completion',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});const created=images(t,()=>{});
 const controller=new AbortController(),rejection=assert.rejects(captureImage('/owned/image',controller.signal),/capture_unavailable/);
 for(const delay of [3000,300,3000,1200,3000]){t.mock.timers.tick(delay);await flush();}await rejection;
 t.mock.timers.tick(60_000);assert.equal(created.length,3);
 const next=new AbortController(),pending=assert.rejects(captureImage('/replacement/image',next.signal));await flush();const late=created.at(-1).onload;
 next.abort();await pending;late();await flush();assert.equal(created.length,4);assert.equal(created.at(-1).onload,null);
});
test('lost Ready reply reconciles the same id/presentation through the native idempotent claim',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});const claimed=new Set(),calls=[];let reveals=0;
 const ready=async()=>{const key='original:7';calls.push(key);if(!claimed.has(key)){claimed.add(key);reveals++;throw Error('reply lost after claim');}};
 const pending=confirmCapture(ready,new AbortController().signal);await flush();t.mock.timers.tick(300);await pending;
 assert.deepEqual(calls,['original:7','original:7']);assert.equal(reveals,1);
});
test('presentation replacement cancels old Ready and ignores its late success',async()=>{
 const old=deferred(),controller=new AbortController();let acknowledged=0;
 const pending=confirmCapture(()=>old.promise,controller.signal).then(()=>acknowledged=1);
 const rejected=assert.rejects(pending);controller.abort();await rejected;
 await confirmCapture(async()=>{},new AbortController().signal);acknowledged=2;
 old.resolve();await flush();assert.equal(acknowledged,2);
});
test('Ready failure exhausts three attempts; cancellation during backoff prevents another claim',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});let calls=0;
 const rejected=assert.rejects(confirmCapture(async()=>{calls++;throw Error('offline');},new AbortController().signal),/capture_unavailable/);
 await flush();await retry(t);await rejected;t.mock.timers.tick(60_000);assert.equal(calls,3);
 const controller=new AbortController(),cancelled=assert.rejects(confirmCapture(async()=>{calls++;throw Error('offline');},controller.signal));
 await flush();controller.abort();await cancelled;t.mock.timers.tick(60_000);await flush();assert.equal(calls,4);
});
test('editor keeps action errors and unknown-send locks independent of preparation recovery',()=>{
 const source=readFileSync(new URL('../frontend/src/WindowsCapture.tsx',import.meta.url),'utf8');
 assert.match(source,/locked=busy\|\|doc\?\.outcome==='unknown'\|\|doc\?\.outcome==='accepted'/);
 assert.match(source,/error=actionError\|\|/);assert.match(source,/setDoc\(current=>current\?\{\.\.\.current,outcome:'unknown'\}/);
 assert.match(source,/confirmCapture\([\s\S]*?\.then\(\(\)=>\{\s*if\(signal.aborted\)return;\s*acknowledged.current=presentation/);
 assert.match(source,/return\(\)=>\{controller.abort\(\);window.clearTimeout\(timer\);\}/);
 assert.match(source,/loaded\.presentation===doc.presentation/);
});

test('a document for another screenshot never replaces the original capture',async t=>{
 t.mock.timers.enable({apis:['setTimeout']});let calls=0;
 const reader=captureDocumentReader('original',async()=>{calls++;return {...doc(),id:'other'};},()=>assert.fail('foreign capture accepted'),()=>{});
 const rejected=assert.rejects(reader.refresh());await flush();await retry(t);await rejected;assert.equal(calls,3);reader.stop();
});
