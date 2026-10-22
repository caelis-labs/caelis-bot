// Run on the input-menu-preview server in a fresh Playwright browser context.
async page=>{
 const base=new URL(page.url()).origin;
 await page.goto(base+'/acceptance?view=streaming');await page.waitForFunction(()=>window.uiFixtureReady);
 const fixtureModule=new URL('completed-stream-fixture.tsx',new URL(await page.locator('script[src*="input-menu-fixture"]').getAttribute('src'),base)).href;
 const mounted=await page.evaluate(()=>runStreamingRegression());
 if(!mounted.ok)throw Error(JSON.stringify(mounted));
 await page.screenshot({path:'output/playwright/completed-stream-browser.png'});
 // Exercise the actual /web.html HTTP transport and retained chat component.
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,route])=>[route.path,name.split('.').at(-1)]));
 await page.addInitScript({content:'(()=>{if(location.protocol!=="http:")return;'+source.replace('export const Call=','window.fixtureCall=')+`\nconst publish=window.fixtureStreamSnapshot;window.fixtureStreamSnapshot=value=>{sessionStorage.setItem('stream-restore',JSON.stringify(value));return publish(value)};const restored=sessionStorage.getItem('stream-restore');if(restored)publish(JSON.parse(restored));})();`});
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'FIXTURE-stream',capabilities:{web:true}}});
  if(path==='/api/v1/composer/claim')return route.fulfill({json:{editing:true,epoch:'fixture'}});
  if(path==='/api/v1/events')return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'});
  if(path==='/api/v1/tasks/list')return route.fulfill({json:{value:[]}});
  const method=names[path];if(!method)return route.fulfill({status:404,json:{error:'fixture_unavailable'}});
  const value=await page.evaluate(async({method,args})=>fixtureCall.ByName(method,...args),{method,args:route.request().postDataJSON()?.args??[]});
  return route.fulfill({json:{value}});
 });
 await page.goto(base+'/web.html?view=streaming#/chat');
 await page.waitForFunction(()=>document.querySelector('.composer-editor')?.contentEditable==='true');
 await page.evaluate(async url=>{await import(url)},fixtureModule);
 const long='HTTP Web 完整正文 👩🏽‍💻 '+ '顺序一致，没有丢字。'.repeat(80)+' END';
 const publish=async(id,text,status)=>{
  await page.evaluate(value=>fixtureStreamSnapshot(value),{id,text,status});
  await page.waitForFunction(({id,status})=>fixtureStreamObserved?.status===status&&!!document.querySelector(`[data-message-id="${id}"] .markdown-body`),{id,status},{polling:50,timeout:6000});
  await page.waitForTimeout(40);
  return page.locator(`[data-message-id="${id}"] .markdown-body`).textContent();
 };
 await publish('history','history','completed');await page.evaluate(()=>streamHold(true));
 if(await publish('web-final',long,'completed')!==long)throw Error('completed HTTP arrival replayed');
 const partial=await publish('web-stream',long,'inProgress');if(partial.length>=long.length)throw Error('HTTP ongoing stream was not paced');
 if(await publish('web-stream',long,'completed')!==long)throw Error('HTTP completion retained frame backlog');
 const other=await page.context().newPage();await other.goto('about:blank');await other.bringToFront();
 const hidden=await page.evaluate(()=>document.hidden);
 if(await publish('web-hidden',long+' hidden','completed')!==long+' hidden')throw Error('background tab completion incomplete');
 await page.bringToFront();await other.close();
 if(await page.locator('[data-message-id="web-hidden"] .markdown-body').textContent()!==long+' hidden')throw Error('tab restore replayed');
 await page.reload();await page.waitForSelector('[data-message-id="web-hidden"] .markdown-body');
 if(await page.locator('[data-message-id="web-hidden"] .markdown-body').textContent()!==long+' hidden')throw Error('history reload replayed');
 await page.screenshot({path:'output/playwright/completed-stream-web.png'});
 return {mounted,web:{completedArrival:true,sameTextCompletion:true,partialLength:partial.length,tabSwitch:true,documentWasHidden:hidden,historyReload:true}};
}
