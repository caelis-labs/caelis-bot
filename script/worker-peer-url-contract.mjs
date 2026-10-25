// A reproducible protocol-gap probe, not remote URL feature acceptance.
// Uses the same production Apps/React/native adapter as the vertical journey.
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {join} from 'node:path';

export async function host(page,path,args=[]) {
 return page.evaluate(async({path,args})=>{
  const boot=await (await fetch('/api/v1/bootstrap',{method:'POST',headers:{'X-Caelis-Request':'1'}})).json();
  const response=await fetch(path,{method:'POST',headers:{'Content-Type':'application/json','X-Caelis-Request':'1','X-CSRF-Token':boot.csrf},body:JSON.stringify({args})});
  return {status:response.status,body:await response.json()};
 },{path,args});
}
export async function urlContractJourney({a,A,B,context,send,nativeStates,until,shot,evidence,relay}) {
 const urlRoute='/api/v1/conversation/approval-url';
 const pending=async(data,id)=>{
  for(const state of await nativeStates(data)) {
   for(const p of Object.values(state.pending))if(p.fixtureKind==='url'&&(!id||p.id===id)&&(!id?!p.action:true))return {state,p};
  }
 };
 async function create(environment,title,data) {
  await a.goto(A.url+'/#chat');
  await send(a,{action:'delegate',environment,title,scenario:'url',wait:false});
  const task=await until(title+' original task',async()=>{
   const ledger=JSON.parse(await readFile(join(A.data,'tasks.json'),'utf8'));
   return Object.values(ledger.records).find(v=>v.view.title===title);
  });
  const original=await until(title+' native URL',()=>pending(data));
  const approval=await until(title+' A original request',async()=>{
   const r=await host(a,'/api/v1/conversation/snapshot');
   assert.equal(r.status,200);
   return r.body.value.approvals.find(v=>v.taskTitle===title&&v.status==='pending');
  });
  const card=a.locator('.approval:visible').filter({hasText:title});
  await until(title+' visible A card',async()=>{
   const dock=a.locator('.approval-dock-trigger');
   if(await dock.isVisible()&&await dock.getAttribute('aria-expanded')==='false')await dock.click();
   return await card.isVisible();
  });
  return {task,original,approval,card};
 }
 // Positive control: local supported native URL, real browser page + original
 // native consent. GET alone and double clicks do not complete work twice.
 const local=await create('local','Local native URL positive control',A.data);
 const [popup]=await Promise.all([context.waitForEvent('page'),local.card.locator(':scope > button.text-action').click()]);await popup.waitForLoadState();
 assert.equal(popup.url(),local.original.p.params.url);
 let native=await pending(A.data,local.original.p.id);
 assert.equal(native.p.challengeCompleted,false);assert.equal(native.state.effectCounts['interaction:'+native.p.id]||0,0);
 const originalURL=new URL(popup.url());
 const denied=await context.request.post(popup.url(),{headers:{Origin:A.url},form:{nonce:native.p.fixtureNonce}});
 assert.equal(denied.status(),403,'foreign Origin must not authorize native challenge');
 const [post]=await Promise.all([popup.waitForResponse(r=>r.request().method()==='POST'),popup.getByRole('button',{name:'Complete synthetic challenge',exact:true}).click()]);assert.equal(post.status(),200,JSON.stringify({origin:post.request().headers().origin,body:await post.text()}));
 await popup.getByText('Synthetic challenge recorded by original native owner.',{exact:false}).waitFor();
 native=await pending(A.data,local.original.p.id);assert.equal(native.p.challengeCompleted,true);
 assert.equal(native.state.effectCounts['interaction:'+native.p.id]||0,0,'web POST is not native consent');
 // Synthetic challenge repeat: native effect remains one even if its own page
 // is submitted twice. No product decision is synthesized by this HTTP probe.
 const repeated=await context.request.post(popup.url(),{headers:{Origin:originalURL.origin},form:{nonce:native.p.fixtureNonce}});
 assert.equal(repeated.status(),200);
 await local.card.locator('.approval-choices .primary-decision').click();
 await until('local native confirmed continuation exactly once',async()=>{
  const v=await pending(A.data,local.original.p.id);
  return v?.state.effectCounts['interaction:'+v.p.id]===1;
 });
 await until('original native URL turn completed',async()=>{const v=await pending(A.data,local.original.p.id);return v?.state.threads[v.p.thread].turns.some(t=>t.id===v.p.turn&&t.status==='completed');});
 const localDone=await pending(A.data,local.original.p.id);
 assert.equal(localDone.state.effectCounts['challenge-post:'+localDone.p.id],1);
 assert.equal(localDone.state.effectCounts['native-reply:'+localDone.p.id],1);
 assert.equal((await context.request.get(originalURL.href)).status(),410,'completed challenge revoked');
 assert.equal((await host(a,urlRoute,[local.approval.id])).status,503,'resolved original URL not resurrected');
 evidence.steps.push('Local positive control: actual A native URL browser GET no effect; foreign Origin refused; nonce POST and original consent independently required; native continuation/effects once; completed URL revoked');
 await popup.close();await shot(a,'A-local-URL-native-confirmed');
 // Remote negative control: same native URL mode/configuration. No browser URL
 // rewrite, direct B operation, compatibility form, or invented native method.
 const remote=await create('B synthetic','B local URL protocol gap',B.data);
 const pageCount=context.pages().length;
 for(let attempt=0;attempt<2;attempt++) {
  const [response]=await Promise.all([a.waitForResponse(r=>r.url()===A.url+urlRoute),remote.card.locator(':scope > button.text-action').click()]);assert.equal(response.status(),503);assert.equal((await response.json()).error,'remote_native_url_not_reachable');
 }
 await remote.card.getByText('这台机器暂时无法访问此链接，原请求仍待处理。',{exact:true}).waitFor();
 assert.equal(context.pages().length,pageCount,'B localhost leaked into A browser');
 native=await pending(B.data,remote.original.p.id);
 assert.equal(native.p.action,undefined);assert.equal(native.p.challengeCompleted,false);
 assert.equal(native.state.effectCounts['native-reply:'+native.p.id]||0,0);
 assert.equal(native.state.effectCounts['interaction:'+native.p.id]||0,0);
 assert.equal(native.state.effectCounts['challenge-post:'+native.p.id]||0,0);
 await shot(a,'A-remote-URL-blocked');
 evidence.steps.push('Actual A/B/React: identical native URL request is blocked remotely; repeated Open causes no popup, native consent, HTTP challenge effect or continuation. This is a confirmed local-to-remote regression, not feature acceptance');
 // A reload and a transient failed original-owner read cannot mint new work.
 // Drop one real URL response after B produced it; retry only this safe read.
 relay.drop('url');
 const failed=await host(a,urlRoute,[remote.approval.id]);assert.equal(failed.status,503);
 const retried=await host(a,urlRoute,[remote.approval.id]);assert.equal(retried.status,503);
 await a.reload();
 await until('same original approval after A UI reconnect',async()=>{
  const r=await host(a,'/api/v1/conversation/snapshot');
  return r.status===200&&r.body.value.approvals.some(v=>v.id===remote.approval.id&&v.status==='pending');
 });
 await until('decline original remote request on A',async()=>{
  const dock=a.locator('.approval-dock-trigger');if(await dock.isVisible()&&await dock.getAttribute('aria-expanded')==='false')await dock.click();
  const card=a.locator('.approval:visible').filter({hasText:'B local URL protocol gap'});
  const decline=card.locator('.approval-choices button').filter({hasText:/拒绝|Decline/});
  if(await decline.isVisible()){await decline.click();return true;}return false;
 });
 await until('B original decline exactly once',async()=>{
  const v=await pending(B.data,remote.original.p.id);return v?.p.action==='decline'&&v.state.effectCounts['native-reply:'+v.p.id]===1;
 });
 assert.equal((await host(a,urlRoute,[remote.approval.id])).status,503);
 const remoteDone=await pending(B.data,remote.original.p.id);
 assert.equal(remoteDone.state.effectCounts['interaction:'+remoteDone.p.id]||0,0);
 evidence.urlContract={local:{nativeReplies:1,challengeEffects:1,continuations:1},remote:{nativeReplies:1,decision:'decline',challengeEffects:0,continuations:0,originalTask:remote.task.view.id,originalRequest:remote.approval.id},boundaries:'real UI reconnect, lost safe URL read, repeated open, original decline/revocation; no account/native-GUI or remote successful URL completion'};
 evidence.gates.remoteLoopbackURL='BLOCKED: native URL contract has no remote browser/callback handoff; actual A/B regression reproduced';
 evidence.result='URL protocol-gap probe passed; remote feature gate remains BLOCKED';
}
