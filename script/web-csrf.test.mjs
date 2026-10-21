import {test} from 'node:test';
import assert from 'node:assert/strict';
import {createCSRFFetch} from '../frontend/src/web-csrf.ts';
import {createWebOperations} from '../frontend/src/web-operations.ts';
const response=(status,value)=>new Response(JSON.stringify(value),{status});
const denied=()=>response(403,{error:'csrf_denied'});

test('expired legacy CSRF reboots in place and retains exact unadmitted write ID',async()=>{
 const calls=[];
 const post=createCSRFFetch('old-grant',async(path,init)=>{
  calls.push({path,init});
  if(path==='/api/v1/bootstrap')return response(200,{csrf:'stable-local'});
  return new Headers(init.headers).get('X-CSRF-Token')==='stable-local'?response(200,{value:'saved'}):denied();
 });
 const saved=new Map();let ids=0;
 const invoke=createWebOperations({storage:{getItem:k=>saved.get(k),setItem:(k,v)=>saved.set(k,v)},identity:()=>`original-${++ids}`,failure:code=>Error(code),receipt:async()=>{throw Error('should not need receipt for confirmed admission')},post:async(path,body)=>{
  const r=await post(path,{method:'POST',headers:{'X-Caelis-Tab':'original-tab','X-Caelis-Edit-Epoch':'original-epoch'},body:JSON.stringify(body)});return r.json();
 }});
 assert.equal(await invoke('/settings',['retained draft'],false),'saved');
 assert.equal(calls.length,3);assert.equal(ids,1);
 assert.equal(calls[0].init.body,calls[2].init.body);
 assert.equal(new Headers(calls[2].init.headers).get('X-Caelis-Tab'),'original-tab');
 assert.equal(new Headers(calls[2].init.headers).get('X-Caelis-Edit-Epoch'),'original-epoch');
 assert.deepEqual(JSON.parse(saved.get('caelisbot.pending-operations')),{});
 await post('/claim',{method:'POST'});assert.equal(calls.length,4);
 assert.equal(new Headers(calls[3].init.headers).get('X-CSRF-Token'),'stable-local');
});

test('concurrent and late stale refusals share one bootstrap',async()=>{
 let release,bootstraps=0;const gate=new Promise(resolve=>release=resolve),pending=[];
 const post=createCSRFFetch('old',async(path,init)=>{
  if(path==='/api/v1/bootstrap'){bootstraps++;await gate;return response(200,{csrf:'new'});}
  if(new Headers(init.headers).get('X-CSRF-Token')==='new')return response(200,{});
  return new Promise(resolve=>pending.push(()=>resolve(denied())));
 });
 const a=post('/a',{}),b=post('/b',{}),c=post('/late',{});
 pending[0]();pending[1]();
 while(!bootstraps)await new Promise(resolve=>setImmediate(resolve));
 release();assert.equal((await a).status,200);assert.equal((await b).status,200);
 pending[2]();assert.equal((await c).status,200);assert.equal(bootstraps,1);
});

test('unknown/network/owner/origin errors are never replayed',async()=>{
 for(const code of ['origin_denied','operation_unconfirmed','operation_rejected','pair_required','not-json','network']){
  let calls=0;
  const post=createCSRFFetch('old',async()=>{calls++;if(code==='network')throw Error('connection lost');return code==='not-json'?new Response('denied',{status:403}):response(code==='pair_required'?401:403,{error:code});});
  if(code==='network')await assert.rejects(post('/write',{body:'original'}));else await post('/write',{body:'original'});
  assert.equal(calls,1,code);
 }
});

test('failed/unchanged refresh and persistent denial have bounded attempts',async()=>{
 for(const mode of ['offline','expired','unchanged','still-denied']){
  let calls=0,boots=0;
  const post=createCSRFFetch('old',async(path)=>{calls++;if(path!=='/api/v1/bootstrap')return denied();boots++;if(mode==='offline')throw Error('offline');return mode==='expired'?response(401,{error:'pair_required'}):response(200,{csrf:mode==='unchanged'?'old':'new'});});
  assert.equal((await post('/write',{})).status,403);assert.equal(boots,1);assert.equal(calls,mode==='still-denied'?3:2);
 }
});
