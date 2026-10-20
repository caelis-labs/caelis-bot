// Playwright CLI run-code function; no dependency or account setup.
// Run against the disposable, uninitialized Rocky demo at 127.0.0.1:19317.
// Error/empty catalogs and completed setup below are browser-only fixtures.
async(page)=>{
 const out='output/playwright/ui-alignment-20261020';
 await page.unrouteAll();const errors=[];page.on('pageerror',e=>errors.push(e.stack));
 const check=(v,s)=>{if(!v)throw Error(s)};
 const result={realSetupGuards:[],gateway:[],settingsFixture:[],pageErrors:errors};
 for(const url of ['http://127.0.0.1:19317/#/settings/general','http://127.0.0.1:19317/#/chat','http://127.0.0.1:19317/#/task/original-task','http://127.0.0.1:19317/?surface=settings&section=general']){
  await page.goto(url);await page.waitForURL(/#\/setup$/);await page.getByRole('heading',{name:'选择一种开始方式'}).waitFor();
  check(!await page.getByRole('button',{name:'设置',exact:true}).count(),'setup exposes gear');
  check(!await page.getByRole('button',{name:'返回聊天',exact:true}).count(),'setup exposes settings return');
  check(page.url().endsWith('#/setup'),'setup URL not canonical');result.realSetupGuards.push(url);
 }
 await page.evaluate(()=>{location.hash='#/settings/general'});await page.waitForURL(/#\/setup$/);
 await page.goBack();await page.getByRole('heading',{name:'选择一种开始方式'}).waitFor();
 await page.goForward();await page.getByRole('heading',{name:'选择一种开始方式'}).waitFor();
 await page.reload();await page.getByRole('heading',{name:'选择一种开始方式'}).waitFor();
 await page.keyboard.press('Escape');check(!await page.getByRole('button',{name:'设置',exact:true}).count(),'Escape bypass');
 await page.setViewportSize({width:600,height:660});await page.screenshot({path:out+'/after/web-setup-600.png'});
 await page.getByRole('button',{name:'默认 推荐 由 Caelis 驱动，自由连接模型服务。',exact:true}).click();
 await page.getByRole('button',{name:'Magpie 连接本机或远程模型网关',exact:true}).click();
 await page.locator('input[type=url]').waitFor();
 await page.locator('.provider-config-footer button.primary').waitFor();
 await page.waitForFunction(()=>!document.querySelector('.provider-config-footer button.primary')?.disabled);
 await page.screenshot({path:out+'/after/web-magpie-live-models-600.png'});
 // Fail only the safe model-catalog read; the backend and all form state remain real.
 let mode='error',readCount=0;
 await page.route('**/api/v1/connections/setup-catalog',async route=>{
  const body=route.request().postDataJSON();
  if(body.args?.[0]?.action!=='models'){await route.continue();return}
  readCount++;
  if(mode==='error')await route.fulfill({status:503,json:{error:'read_unavailable'}});
  else if(mode==='empty')await route.fulfill({status:200,json:{value:null}});
  else await route.continue();
 });
 await page.getByRole('button',{name:'刷新模型',exact:true}).click();
 await page.locator('.gateway-read-notice').waitFor();
 check(!await page.locator('.surface-recovery').count(),'model read became window recovery');
 check(await page.locator('input[type=url]').inputValue()==='http://127.0.0.1:3425/v1','form lost');
 check(readCount===3,'bounded reads');
 check(!await page.getByRole('button',{name:'复制反馈信息',exact:true}).count(),'local gateway error exposes generic feedback card');
 await page.screenshot({path:out+'/after/web-magpie-unavailable-600.png'});
 result.gateway.push({mode,readCount,formPreserved:true});
 mode='empty';readCount=0;await page.locator('.gateway-read-notice button').first().click();
 await page.getByText('网关没有返回可用模型，请在网关中连接模型后重试。',{exact:false}).waitFor();
 check(!await page.locator('.surface-recovery').count(),'empty catalog became window recovery');
 result.gateway.push({mode,readCount});await page.screenshot({path:out+'/after/web-magpie-empty-600.png'});
 mode='live';await page.locator('.gateway-read-notice button').first().click();
 await page.waitForFunction(()=>!document.querySelector('.provider-config-footer button.primary')?.disabled);
 check(!await page.locator('.gateway-read-notice').count(),'successful retry did not clear notice');
 result.gateway.push({mode,restored:true});await page.unroute('**/api/v1/connections/setup-catalog');
 await page.getByRole('button',{name:'返回',exact:true}).click();await page.getByRole('heading',{name:'连接模型',exact:true}).waitFor();
 check(!await page.getByRole('button',{name:'设置',exact:true}).count(),'normal Back escaped guide');
 // Complete-state projection is browser-only: no initialization or model write.
 await page.route('**/api/v1/connections/setup-overview',r=>r.fulfill({json:{value:{active:'codex',hasRuntimeChoice:true,onboarding:false,pending:''}}}));
 await page.route('**/api/v1/connections/bot-initialization',r=>r.fulfill({json:{value:{required:false,status:'accepted'}}}));
 for(const width of [390,900,1600]){
  await page.setViewportSize({width,height:900});
  for(const section of ['general','appearance','connections','chatConnections','machines','plugins','updates']){
   await page.goto('http://127.0.0.1:19317/#/settings/'+section);
   await page.locator('.settings-page[data-section='+section+']:not([hidden])').first().waitFor();
   if(section==='connections')await page.waitForFunction(()=>[...document.querySelectorAll('button')].some(b=>b.textContent==='重新检测'&&!b.disabled));
   if(section==='plugins')await page.getByText('Brave Search',{exact:true}).waitFor();
   await page.waitForTimeout(section==='appearance'?1200:350);
   const metrics=await page.evaluate(()=>{
    const content=document.querySelector('.settings-content').getBoundingClientRect();
    const p=[...document.querySelectorAll('.settings-page')].find(n=>!n.hidden).getBoundingClientRect();
    const nav=document.querySelector('aside nav').getBoundingClientRect(),active=document.querySelector('aside nav [aria-current=page]').getBoundingClientRect();
    const close=document.querySelector('.settings-navigation').getBoundingClientRect(),icon=document.querySelector('.settings-navigation svg').getBoundingClientRect();
    return {contentX:content.x,contentWidth:content.width,x:p.x,width:p.width,left:p.x-content.x,right:content.right-p.right,activeNavVisible:active.left>=nav.left-1&&active.right<=nav.right+1,closeIconContained:icon.left>=close.left&&icon.right<=close.right&&close.right<=innerWidth,overflow:document.documentElement.scrollWidth>innerWidth};
   });
   check(!metrics.overflow,'horizontal overflow '+section+' '+width);
   check(metrics.activeNavVisible,'active navigation outside viewport '+section+' '+width);
   check(metrics.closeIconContained,'close icon clipped '+section+' '+width);
   if(width===1600)check(Math.abs(metrics.left-metrics.right)<3,'unbalanced wide grid '+section);
   await page.screenshot({path:out+'/after/web-settings-'+section+'-'+width+'-fixture.png'});
   result.settingsFixture.push({section,viewportWidth:width,...metrics});
  }
 }
 await page.setViewportSize({width:900,height:800});
 await page.getByRole('button',{name:'关闭设置',exact:true}).click();await page.waitForURL('**/#/chat');
 await page.getByRole('button',{name:'设置',exact:true}).click();await page.waitForURL('**/#/settings/general');
 await page.goBack();await page.waitForURL('**/#/chat');await page.goForward();await page.waitForURL('**/#/settings/general');
 await page.reload();await page.locator('.settings-page[data-section=general]:not([hidden])').first().waitFor();
 result.completedNavigation=true;
 await page.unroute('**/api/v1/connections/setup-overview');await page.unroute('**/api/v1/connections/bot-initialization');
 await page.getByRole('button',{name:'关闭设置',exact:true}).click();await page.getByRole('heading',{name:'选择一种开始方式'}).waitFor();
 result.settingsReturnRechecksSetup=true;
 await page.goto('http://127.0.0.1:19317/#/settings/general');await page.getByRole('heading',{name:'选择一种开始方式'}).waitFor();
 for(const status of [503,429]){
  await page.route('**/api/v1/bootstrap',r=>r.fulfill({status,json:{error:status===429?'device_limit':'unavailable'}}));
  await page.reload();await page.getByRole('heading',{name:'连接暂未完成'}).waitFor();
  check(!await page.getByLabel('配对码',{exact:true}).count(),'non-auth HTTP failure requested pairing');
  await page.screenshot({path:out+'/after/web-bootstrap-'+status+'.png'});
  await page.unroute('**/api/v1/bootstrap');await page.getByRole('button',{name:'重试',exact:true}).click();
  await page.getByRole('heading',{name:'选择一种开始方式'}).waitFor();
 }
 result.bootstrapHTTPRecovery=true;
 check(errors.length===0,'renderer exception: '+errors.join('\n'));
 return result;
}
