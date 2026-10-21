// Production WebApp and transport, synthetic auth expiry/backend only.
async page=>{
 const base=new URL(page.url()).origin;
 await page.unrouteAll();
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,r])=>[r.path,name.split('.').at(-1)]));
 await page.addInitScript({content:'(()=>{'+source.replace('export const Call=','window.fixtureCall=')+'})();'+';sessionStorage.setItem("fixture-loads",String(Number(sessionStorage.getItem("fixture-loads")||0)+1));'});
 let token='legacy-grant',bootstraps=0,claims=0,denied=0;const effects=[];
 await page.route('**/api/v1/**',async route=>{
  const req=route.request(),path=new URL(req.url()).pathname;
  if(path==='/api/v1/bootstrap'){bootstraps++;return route.fulfill({json:{csrf:token,version:'FIXTURE-csrf',capabilities:{web:true}}});}
  if(req.method()==='POST'&&req.headers()['x-csrf-token']!==token){denied++;return route.fulfill({status:403,json:{error:'csrf_denied'}});}
  if(path==='/api/v1/composer/claim'){claims++;return route.fulfill({json:{editing:true,epoch:'retained-epoch'}});}
  if(path==='/api/v1/events')return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'});
  if(path==='/api/v1/tasks/list')return route.fulfill({json:{value:[]}});
  const method=names[path];if(!method)return route.fulfill({status:404,json:{error:'fixture_unavailable'}});
  const body=req.postDataJSON();
  if(method==='SaveDraft'||method==='Submit')effects.push({method,...body});
  const value=await page.evaluate(async({method,args})=>fixtureCall.ByName(method,...args),{method,args:body?.args??[]});
  return route.fulfill({json:{value}});
 });
 await page.goto(base+'/web.html#/chat');
 const editor=page.getByRole('textbox',{name:'写下想法',exact:true});await editor.waitFor();
 await page.waitForFunction(()=>document.querySelector('.composer-editor')?.contentEditable==='true');
 await editor.fill('迁移前草稿');await page.waitForFunction(async()=> (await import('/src/desktop.ts')).backend('Draft').then(d=>d.text==='迁移前草稿'));
 await editor.click();
 const before=await page.evaluate(()=>{window.keptComposer=document.querySelector('.composer-editor');return{loads:sessionStorage.getItem('fixture-loads'),files:document.querySelector('.attachments')?.textContent,hash:location.hash}});
 token='stable-local';
 await editor.pressSequentially(' 保留输入');
 await page.waitForFunction(async()=> (await import('/src/desktop.ts')).backend('Draft').then(d=>d.text==='迁移前草稿 保留输入'));
 const initialClaims=claims;
 await page.waitForTimeout(10500); // Cross the actual ten-second composer renewal.
 const after=await page.evaluate(()=>({loads:sessionStorage.getItem('fixture-loads'),sameNode:keptComposer===document.querySelector('.composer-editor'),focus:document.activeElement===keptComposer,text:keptComposer.innerText,inert:!!keptComposer.closest('[inert]'),files:document.querySelector('.attachments')?.textContent,hash:location.hash,editingNotice:document.body.innerText.includes('另一页面正在编辑')}));
 if(bootstraps!==2||denied<1||claims<=initialClaims||after.loads!==before.loads||!after.sameNode||!after.focus||after.inert||after.editingNotice||after.files!==before.files||after.hash!==before.hash||after.text!=='迁移前草稿 保留输入')throw Error(JSON.stringify({bootstraps,claims,denied,before,after}));
 if(effects.some(e=>e.method==='Submit')||new Set(effects.map(e=>e.id)).size!==effects.length)throw Error('replayed write');
 await page.screenshot({path:'output/playwright/ui-findings/review-regressions/csrf-restored.png'});
 return {bootstraps,claims,denied,acceptedWrites:effects.length,before,after,submitCount:0};
}
