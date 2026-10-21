// Playwright CLI run-code function. Start on desktop-settings-preview.html from
// script/build_and_run.sh --settings-preview. All Web API responses below are
// synthetic; the real Web transport, routing and shared components run unchanged.
async(page)=>{
 const base=new URL(page.url()).origin,out='output/playwright/ui-findings';
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,route])=>[route.path,name.split('.').at(-1)]));
 const check=(value,label)=>{if(!value)throw Error(label)},errors=[];
 let claims=0,reads=0;const documents=[];page.on('request',r=>{if(r.resourceType()==='document')documents.push(r.url())});
 page.on('pageerror',error=>errors.push(error.name+': '+error.message));
 await page.addInitScript({content:source.replace('export const Call=','window.fixtureCall=')+`
 Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>{window.fixtureFeedback=text}}});
 `});
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'FIXTURE-shared-ui',capabilities:{web:true}}});
  if(path==='/api/v1/composer/claim'){claims++;return route.fulfill({json:{editing:true,epoch:'fixture'}});}
  if(path==='/api/v1/events')return route.fulfill({contentType:'text/event-stream',body:': synthetic fixture\n\n'});
  if(path==='/api/v1/tasks/list')return route.fulfill({json:{value:[{id:'original-task',title:'保留的后台工作',status:'completed',result:'合成结果：任务仍属于原执行者。'}]}});
  if(path==='/api/v1/tasks/conversation')return route.fulfill({json:{value:{status:'completed',messages:[{id:'original-result',role:'assistant',text:'合成结果：任务仍属于原执行者。'}]}}});
  const method=names[path];
  if(method==='SetupOverview'){reads++;if(reads%2===0)await new Promise(r=>setTimeout(r,700));return route.fulfill({json:{value:{active:'caelis',onboarding:true,hasRuntimeChoice:false,pending:''}}});}
  if(method==='BotInitialization')return route.fulfill({json:{value:{required:true,status:'required'}}});
  if(method==='ApplySetup')return route.fulfill({json:{value:{settings:{runtime:'caelis',cliPath:'',caelisStore:''},installation:{installed:true,bundled:true,version:'FIXTURE'},state:'models',models:[],selectedModel:''}}});
  if(method==='RuntimeConnectionCatalog')return route.fulfill({json:{value:{choices:route.request().postDataJSON().args[0]==='api-key'?[{id:'magpie',name:'Magpie',description:'Synthetic gateway'}]:[],unavailable:''}}});
  if(method==='SetupCatalog')return route.fulfill({json:{value:[]}});
  if(method==='DiagnosticReport')return route.fulfill({json:{value:btoa(JSON.stringify({connection:'ready',platform:{os:'linux',arch:'arm64'},private:'PRIVATE_SENTINEL',url:'https://private/?token=SECRET'}))}});
  if(!method)return route.fulfill({status:404,json:{error:'fixture_unavailable'}});
  const args=route.request().postDataJSON()?.args??[];
  try{
   const value=await page.evaluate(async({method,args})=>window.fixtureCall.ByName(method,...args),{method,args});
   return route.fulfill({json:{value}});
  }catch{return route.fulfill({status:503,json:{error:'fixture_read_unavailable'}})}
 });

 await page.setViewportSize({width:640,height:760});
 await page.goto(base+'/web.html#/setup');
 await page.getByRole('button',{name:/默认 推荐/}).waitFor();
 const initial=await page.evaluate(()=>performance.timeOrigin);
 await page.waitForTimeout(9500);
 await page.getByRole('button',{name:/默认 推荐/}).click();
 await page.getByRole('button',{name:/Magpie/}).waitFor();
 await page.screenshot({path:out+'/setup-step2.png'});
 // Multiple delayed route checks cross a user step change. Existing epoch guard
 // must not remount the guide or change the locally selected step.
 await page.evaluate(()=>{window.dispatchEvent(new PopStateEvent('popstate'));window.dispatchEvent(new HashChangeEvent('hashchange'))});
 await page.getByRole('button',{name:/Magpie/}).click();
 const input=page.getByRole('combobox',{name:'网关地址'});await input.waitFor();
 await input.fill('http://fixture.invalid:3425/v1');
 await page.evaluate(()=>{window.setupInput=document.querySelector('input');window.setupRemoved=false;new MutationObserver(()=>{if(!setupInput.isConnected)window.setupRemoved=true}).observe(document.body,{subtree:true,childList:true})});
 // Network offline/reconnect (no 401) and SSE reconnection must preserve edits.
 await page.context().setOffline(true);await page.waitForTimeout(1500);await page.context().setOffline(false);
 await page.waitForTimeout(11000);
 const observed=await page.evaluate(()=>({timeOrigin:performance.timeOrigin,value:window.setupInput.value,connected:window.setupInput.isConnected,removed:window.setupRemoved,focused:document.activeElement===window.setupInput,hash:location.hash,settingsEntry:!!document.querySelector('.chat-settings')}));
 check(observed.timeOrigin===initial&&documents.length===1,'unexpected document reload');
 check(observed.connected&&!observed.removed&&observed.focused&&observed.value==='http://fixture.invalid:3425/v1','step/input/focus lost '+JSON.stringify(observed));
 check(claims>=3,'periodic renewal did not run');check(!observed.settingsEntry,'setup exposed settings');
 await page.screenshot({path:out+'/setup-after-refresh-reconnect.png'});
 return {fixtureOnly:true,renewals:claims,overviewReads:reads,documents:documents.length,observed,errors};
}
