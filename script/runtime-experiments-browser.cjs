// Playwright CLI run-code. Start on desktop-settings-preview.html served by
// script/build_and_run.sh --settings-preview. Shared UI/transport, synthetic API.
async(page)=>{
 const base=new URL(page.url()).origin,out='output/playwright/experiments-20261021';
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,route])=>[route.path,name.split('.').at(-1)]));
 const check=(value,label)=>{if(!value)throw Error(label)},errors=[];
 page.on('pageerror',error=>errors.push(error.message));
 await page.addInitScript({content:source.replace('export const Call=','window.fixtureCall=')});
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'FIXTURE-experiments',capabilities:{web:true}}});
  if(path==='/api/v1/composer/claim')return route.fulfill({json:{editing:true,epoch:'fixture'}});
  if(path==='/api/v1/events')return route.fulfill({contentType:'text/event-stream',body:': synthetic fixture\n\n'});
  if(path==='/api/v1/tasks/list')return route.fulfill({json:{value:[]}});
  const method=names[path];
  if(!method)return route.fulfill({status:404,json:{error:'fixture_unavailable'}});
  const args=route.request().postDataJSON()?.args??[];
  try{
   const value=await page.evaluate(async({method,args})=>window.fixtureCall.ByName(method,...args),{method,args});
   return route.fulfill({json:{value}});
  }catch{return route.fulfill({status:503,json:{error:'fixture_read_unavailable'}})}
 });
 await page.setViewportSize({width:1040,height:780});
 await page.goto(base+'/web.html#/settings/connections');
 const open=async()=>{await page.getByRole('button',{name:'默认 连接模型服务 配置就绪',exact:true}).click();await page.getByRole('heading',{name:'实验特性',exact:true}).waitFor();};
 const back=()=>page.getByRole('button',{name:'连接与模型',exact:true}).last().click();
 const count=()=>page.evaluate(()=>window.fixtureCalls.filter(m=>m==='ChangeRuntimeExperiments').length);
 const section=page.locator('.runtime-experiments'),compact=page.getByRole('switch',{name:'异步压缩',exact:true}),refresh=page.getByRole('switch',{name:'MCP 动态刷新',exact:true}),retirement=page.getByRole('switch',{name:'延迟工具卸载',exact:true});
 await open();
 const peerSections=()=>page.evaluate(()=>{
  const sections=[...document.querySelectorAll('.codex-runtime-panel>.setting-section')];
  const advanced=sections.find(s=>s.querySelector('h2')?.textContent==='高级配置');
  const experiments=sections.find(s=>s.querySelector('h2')?.textContent==='实验特性');
  return !!advanced&&!!experiments&&advanced!==experiments&&
   advanced.querySelectorAll('[role=switch]').length===0&&experiments.querySelectorAll('[role=switch]').length===3&&
   !experiments.querySelector('h3')&&!experiments.textContent.includes('默认关闭');
 });
 check(await peerSections(),'experiments must be a separate peer section');
 check(await count()===0,'page entry wrote defaults');
 check(!await compact.isChecked()&&!await refresh.isChecked()&&!await retirement.isChecked(),'Core defaults not reflected');
 await compact.click();await section.getByText('已保存，下次运行时激活时生效。',{exact:true}).waitFor();
 check(await compact.isChecked()&&!await refresh.isChecked()&&!await retirement.isChecked(),'switches coupled');
 await page.screenshot({path:out+'/web-wide-fixture.png'});
 await back();await open();
 check(await compact.isChecked()&&await count()===1,'reentry reset user preference');
 // A second client changes the Host after this page loaded. CAS must adopt
 // the current revision, not overwrite it with the stale tab's choice.
 await page.evaluate(()=>window.fixtureSetExperiments({mcp_catalog_refresh:true}));
 await retirement.click();await section.getByText('设置已被其他客户端修改，请核对后再试。',{exact:true}).waitFor();
 check(await refresh.isChecked()&&!await retirement.isChecked(),'stale tab overrode newer Host settings');
 await page.evaluate(()=>window.fixtureExperimentalReject=true);
 await retirement.click();await section.getByText('未保存，请重新检查后重试。',{exact:true}).waitFor();
 check(!await retirement.isChecked(),'rejected save appeared successful');
 await page.evaluate(()=>{window.fixtureExperimentalReject=false;window.fixtureExperimentalPending=true});
 await retirement.click();await section.getByText('正在核对保存结果，暂时不能继续修改。',{exact:true}).waitFor();
 const pendingCount=await count();
 check(!await compact.isEnabled(),'unknown save allowed another write');
 await back();await open();
 await section.getByRole('button',{name:'重新检测',exact:true}).click();
 check(await count()===pendingCount,'remount/read replayed unknown save');
 await page.evaluate(()=>{window.fixtureExperimentalPending=false;window.fixtureSetExperiments({deferred_tool_retirement:true},'saved')});
 await section.getByRole('button',{name:'重新检测',exact:true}).click();
 await section.getByText('已保存，下次运行时激活时生效。',{exact:true}).waitFor();
 check(await retirement.isChecked()&&await count()===pendingCount,'original receipt did not settle read-only');
 // A late save may settle on the server while this panel is elsewhere.
 await page.evaluate(()=>window.fixtureExperimentalDelay=650);
 await compact.click();await back();
 await page.waitForFunction(async()=>(await window.fixtureCall.ByName('RuntimeExperiments')).features.find(f=>f.id==='async_compact').enabled===false);
 await open();
 await page.waitForFunction(()=>document.querySelector('#experiment-async_compact')?.checked===false);
 check(await refresh.isChecked()===true&&await retirement.isChecked()===true,'late read reset independent flags');
 await back();await page.evaluate(()=>window.fixtureExperimentalReadFailure=true);await open();
 await section.getByText('未能确认保存结果，请重新检查。',{exact:true}).waitFor();
 check(await page.locator('.surface-recovery').count()===0,'local read error replaced whole page');
 await page.evaluate(()=>window.fixtureExperimentalReadFailure=false);
 await section.getByRole('button',{name:'重新检测',exact:true}).click();
 await compact.waitFor();
 await page.setViewportSize({width:390,height:780});
 check(await peerSections(),'narrow section hierarchy changed');
 await page.screenshot({path:out+'/web-narrow-fixture.png'});
 check(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'narrow settings overflowed');
 check(await page.evaluate(()=>[...document.querySelectorAll('.runtime-experiments .setting-row')].every(row=>{const a=row.querySelector('label').getBoundingClientRect(),b=row.querySelector('input').getBoundingClientRect();return Math.abs(a.y-b.y)<10&&b.x>a.x+a.width})), 'narrow switches detached from labels');
 check(errors.length===0,'renderer crashed '+errors.join(','));
 return {fixtureOnly:true,pageEntryWrites:0,peerSections:true,independentSwitches:true,reentryPreserved:true,staleTabRejected:true,rejectionHonest:true,pendingReadOnly:true,lateResponseFenced:true,localReadRecovery:true,widths:[1040,390],mutationCount:await count(),pageErrors:errors};
}
