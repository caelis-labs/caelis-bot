// Playwright CLI run-code function. Start on desktop-settings-preview.html from
// script/build_and_run.sh --settings-preview. All Web API responses below are
// synthetic; the real Web transport, routing and shared components run unchanged.
async(page)=>{
 const base=new URL(page.url()).origin,out='output/playwright/ui-findings';
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,route])=>[route.path,name.split('.').at(-1)]));
 const check=(value,label)=>{if(!value)throw Error(label)},errors=[];
 page.on('pageerror',error=>errors.push(error.name+': '+error.message));
 await page.addInitScript({content:source.replace('export const Call=','window.fixtureCall=')+`
 window.fixtureComposerState=(count,working)=>{files.splice(0,files.length,...Array.from({length:count},(_,i)=>({id:'fixture-'+i,name:'file-'+i+'.txt',size:20,type:'text/plain',image:false,unavailable:false})));snapshot.canInterrupt=working;snapshot.canSend=!working;snapshot.canSteer=working;snapshot.phase=working?'thinking':'idle';snapshot.revision++;window.dispatchEvent(new Event('files-changed'));};
 Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>{window.fixtureFeedback=text}}});
 `});
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'FIXTURE-shared-ui',capabilities:{web:true}}});
  if(path==='/api/v1/composer/claim')return route.fulfill({json:{editing:true,epoch:'fixture'}});
  if(path==='/api/v1/events')return route.fulfill({contentType:'text/event-stream',body:': synthetic fixture\n\n'});
  if(path==='/api/v1/tasks/list')return route.fulfill({json:{value:[{id:'original-task',title:'保留的后台工作',status:'completed',result:'合成结果：任务仍属于原执行者。'}]}});
  if(path==='/api/v1/tasks/conversation')return route.fulfill({json:{value:{status:'completed',messages:[{id:'original-result',role:'assistant',text:'合成结果：任务仍属于原执行者。'}]}}});
  const method=names[path];
  if(method==='DiagnosticReport')return route.fulfill({json:{value:btoa(JSON.stringify({connection:'ready',platform:{os:'linux',arch:'arm64'},private:'PRIVATE_SENTINEL',url:'https://private/?token=SECRET'}))}});
  if(!method)return route.fulfill({status:404,json:{error:'fixture_unavailable'}});
  const args=route.request().postDataJSON()?.args??[];
  try{
   const value=await page.evaluate(async({method,args})=>window.fixtureCall.ByName(method,...args),{method,args});
   return route.fulfill({json:{value}});
  }catch{return route.fulfill({status:503,json:{error:'fixture_read_unavailable'}})}
 });

 const result=[];
 const retained=()=>page.evaluate(()=>({draft:document.querySelector('textarea')?.value,files:document.querySelectorAll('[aria-label="移除 fixture-notes.txt"]').length,scroll:document.querySelector('.chat-scroll')?.scrollTop,same:window.fixtureComposer===document.querySelector('textarea')}));
 await page.setViewportSize({width:1280,height:850});
 await page.goto(base+'/web.html#/chat');
 await page.getByRole('textbox',{name:'写下想法',exact:true}).waitFor();
 await page.waitForFunction(()=>document.querySelector('textarea')?.value.includes('retained draft'));
 await page.evaluate(()=>{document.querySelector('.chat-scroll').scrollTop=140;window.fixtureComposer=document.querySelector('textarea')});
 await page.waitForTimeout(100);
 const before=await retained();
 for(const width of [1280,680,390]){
  await page.setViewportSize({width,height:850});
  const trigger=page.getByRole('button',{name:'设置',exact:true});
  await trigger.click();
  await page.getByRole('button',{name:'关闭设置',exact:true}).waitFor();
  check(await page.locator('.web-workspace').evaluate(n=>n.inert),'background not inert');
  await page.screenshot({path:out+'/settings-'+width+'.png'});
  await page.getByRole('button',{name:'消息渠道',exact:true}).click();
  await page.getByRole('button',{name:/Telegram/}).click();
  await page.getByRole('navigation',{name:'设置分类'}).last().getByText('Telegram',{exact:true}).waitFor();
  check(await page.locator('.settings-breadcrumbs').innerText()==='设置\n/\n消息渠道\n/\nTelegram','channel trail incomplete');
  await page.screenshot({path:out+'/telegram-'+width+'.png'});
  await page.locator('.settings-breadcrumbs').getByRole('button',{name:'消息渠道'}).click();
  await page.getByRole('button',{name:'关闭设置',exact:true}).click();
  await page.waitForURL(/#\/chat$/);
  check(await trigger.evaluate(n=>document.activeElement===n),'close focus not restored');
  result.push({width,retained:await retained()});
 }
 await page.setViewportSize({width:1280,height:850});
 await page.getByRole('button',{name:'设置',exact:true}).click();
 await page.waitForURL(/#\/settings\/general$/);await page.goBack();await page.waitForURL(/#\/chat$/);
 check((await retained()).same,'back remounted composer');
 // Long draft scrolls independently of fixed controls; no send/model task.
 await page.getByRole('textbox',{name:'写下想法',exact:true}).fill(Array.from({length:70},(_,i)=>'长文本 '+i).join('\n'));
 for(const width of [1280,680,390]){
  await page.setViewportSize({width,height:700});
  const geometry=await page.evaluate(()=>{
   const scroll=document.querySelector('.composer-scroll'),action=document.querySelector('.composer-actions .add'),send=document.querySelector('[aria-label="发送"]'),card=document.querySelector('.compose-area');
   scroll.scrollTop=scroll.scrollHeight;
   const a=action.getBoundingClientRect(),b=scroll.getBoundingClientRect(),c=card.getBoundingClientRect(),d=send.getBoundingClientRect();
   return {width:innerWidth,scrolling:scroll.scrollHeight>scroll.clientHeight,separate:a.right<=b.left&&b.right<=d.left,buttonVisible:d.bottom<=c.bottom&&d.right<=c.right,bottom:innerHeight-c.bottom,overflow:document.documentElement.scrollWidth>innerWidth};
  });
  check(geometry.scrolling&&geometry.separate&&geometry.buttonVisible&&!geometry.overflow,'composer geometry '+JSON.stringify(geometry));
  result.push(geometry);await page.screenshot({path:out+'/composer-'+width+'.png'});
 }
 check(errors.length===0,'renderer error '+errors.join(','));
 return {fixtureOnly:true,before,result,errors};
}
