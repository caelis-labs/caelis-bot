import {test} from 'node:test';
import assert from 'node:assert/strict';
import {PortraitCache,portraitClock,portraitFrame} from '../frontend/src/portrait-player.ts';
import {activityPortrait,completedReply} from '../frontend/src/avatar-presentation.ts';
import {readManifest,validateManifest} from './asset-pack.mjs';

const running={revision:1,connection:'ready',phase:'working',currentTurn:'one',items:[],approvals:[],reviews:[],quiet:false};
const reply={id:'answer',turnKey:'one',kind:'assistant',text:'hello',status:'inProgress'};
test('portrait and waiting bubble share native activity priorities',()=>{
 assert.equal(activityPortrait(running),'think');
 for(const kind of ['search','list','web'])assert.equal(activityPortrait({...running,activity:{kind}}),'scan');
 for(const kind of ['read','edit','execute','unrecognized'])assert.equal(activityPortrait({...running,activity:{kind}}),'focus');
 const stream={...running,activity:{kind:'read'},items:[reply]};
 assert.equal(activityPortrait(stream),'listen');
 assert.equal(activityPortrait({...stream,approvals:[{status:'pending'}]}),'waiting');
 assert.equal(activityPortrait({...stream,reviews:[{status:'inProgress'}]}),'focus');
 assert.equal(activityPortrait({...stream,phase:'interrupting'}),'companion');
 assert.equal(activityPortrait({...running,maintenance:'dreaming',quiet:true}),'dreaming');
 assert.equal(activityPortrait({...running,quiet:true}),'companion');
 assert.equal(activityPortrait({...running,items:[{...reply,turnKey:'old',text:'search the web'}]}),'think');
});
test('one live completion edge survives a cleared turn without celebrating history, failures or another run',()=>{
 const before={...running,items:[reply]},done={...before,phase:'completed',currentTurn:'',items:[{...reply,status:'completed'}]};
 assert.equal(completedReply(before,done),'answer');
 assert.equal(completedReply(null,done),null);assert.equal(completedReply(done,done),null);
 for(const change of [{quiet:true},{maintenance:'dreaming'},{connection:'offline'},{phase:'interrupting'},{currentTurn:'other'}])assert.equal(completedReply({...before,...change},done),null);
 for(const change of [{quiet:true},{maintenance:'dreaming'},{phase:'failed'},{currentTurn:'other'},{approvals:[{status:'pending'}]},{items:[{...reply,turnKey:'other',status:'completed'}]}])assert.equal(completedReply(before,{...done,...change}),null);
});
test('decoded cache is bounded, shares pending loads, retries failures and fences old completions',async()=>{
 const pending=new Map();let loads=0;
 const cache=new PortraitCache(url=>{loads++;return new Promise((resolve,reject)=>pending.set(url,{resolve,reject}));},2);
 const a=cache.get('a');assert.equal(cache.get('a'),a);assert.equal(loads,1);
 const b=cache.get('b'),c=cache.get('c');assert.equal(cache.size,2);
 pending.get('a').resolve('old a');await a;assert.equal(cache.size,2);
 pending.get('b').reject(Error('decode'));await assert.rejects(b);
 assert.equal(cache.size,1);const retry=cache.get('b');assert.notEqual(retry,b);
 pending.get('b').resolve('new b');pending.get('c').resolve('c');assert.deepEqual(await Promise.all([retry,c]),['new b','c']);
});
test('portrait clock owns one RAF, pauses hidden time and releases on replacement/unmount',()=>{
 let id=0;const queued=new Map(),drawn=[];
 const scheduler={request(fn){queued.set(++id,fn);return id;},cancel(id){queued.delete(id);}};
 const tick=time=>{const callbacks=[...queued.values()];queued.clear();callbacks.forEach(fn=>fn(time));};
 const clock=portraitClock(elapsed=>drawn.push(elapsed),scheduler);
 clock.setRunning(true);clock.setRunning(true);assert.equal(queued.size,1);tick(0);tick(50);assert.equal(drawn.at(-1),.05);
 clock.setRunning(false);assert.equal(queued.size,0);tick(50000);clock.setRunning(true);tick(60000);assert.equal(drawn.at(-1),.05);
 clock.dispose();assert.equal(queued.size,0);clock.setRunning(true);assert.equal(queued.size,0);
 const sheet={frameSize:128,columns:12,frameCount:120,fps:20};
 assert.deepEqual(portraitFrame(sheet,.6),{frame:12,x:0,y:128});assert.equal(portraitFrame(sheet,6).frame,0);
});
test('portrait contract rejects code paths, missing neutral, oversized decode, wrong source or license; v2 stays supported',()=>{
 const mutate=fn=>{const pack=readManifest();fn(pack.characters[0].variants[0].portrait,pack);return()=>validateManifest(pack);};
 for(const fn of [p=>p.poster='../../source.png',p=>p.clips.companion='https://example.com/x.webp',p=>delete p.clips.companion,p=>p.frameCount=240,p=>p.fps=120,p=>p.sourceSHA256='0'.repeat(64),(p,m)=>m.files.find(f=>f.path===p.poster).license='Apache-2.0'])assert.throws(mutate(fn));
 const old=readManifest();old.contractVersion=2;for(const c of old.characters)for(const v of c.variants)delete v.portrait;old.files=old.files.filter(f=>!f.path.includes('/portraits/'));validateManifest(old);
});
