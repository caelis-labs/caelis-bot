// Run through Playwright CLI on the isolated desktop-settings preview server.
// Production shared menu and both transports; all plugin data is synthetic.
async(page)=>{
 const base=new URL(page.url()).origin,out='output/playwright/ui-findings/attachment-popover',results=[],errors=[];
 const check=(v,m)=>{if(!v)throw Error(m)};
 page.on('pageerror',e=>errors.push(e.message));
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,r])=>[r.path,name.split('.').at(-1)]));
 await page.addInitScript({content:source.replace('export const Call=','window.fixtureCall=')});
 await page.route('**/api/v1/**',async route=>{
  const p=new URL(route.request().url()).pathname;
  if(p==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'SYNTHETIC-menu',capabilities:{web:true}}});
  if(p==='/api/v1/composer/claim')return route.fulfill({json:{editing:true,epoch:'fixture'}});
  if(p==='/api/v1/events')return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'});
  if(p==='/api/v1/tasks/list')return route.fulfill({json:{value:[{id:'menu-layout-task',title:'菜单分屏布局检查',status:'completed'}]}});
  if(p==='/api/v1/tasks/conversation')return route.fulfill({json:{value:{status:'completed',messages:[{id:'task-answer',role:'assistant',text:'合成任务记录，无外部工作。'}]}}});
  const method=names[p];if(!method)return route.fulfill({status:404,json:{error:'fixture_unavailable'}});
  const args=route.request().postDataJSON()?.args??[];
  const value=await page.evaluate(async({method,args})=>window.fixtureCall.ByName(method,...args),{method,args});
  if(method==='ChatSnapshot')value.snapshot.items.push({id:'link-menu-task',kind:'assistant',text:'菜单分屏布局检查',status:'completed',artifacts:[],task:{id:'menu-layout-task',status:'completed'}});
  return route.fulfill({json:{value}});
 });
 const measure=async(label)=>{
  await page.locator('.attachment-menu').evaluate(async node=>{await Promise.all(node.getAnimations().map(animation=>animation.finished));});
  await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
  const g=await page.evaluate(()=>{
   const menu=document.querySelector('.attachment-menu'),card=document.querySelector('.compose-area'),button=document.querySelector('.add');
   const m=menu.getBoundingClientRect(),c=card.getBoundingClientRect(),a=button.getBoundingClientRect();
   const row=[...menu.querySelectorAll('button')].find(b=>!b.disabled),r=row.getBoundingClientRect();
   const sc=document.querySelector('.composer-scroll').getBoundingClientRect(),send=document.querySelector('.send'),sr=send.getBoundingClientRect();
   return {scrollRight:c.right-sc.right,actionsBelow:sr.top>=sc.bottom,sendHit:send.contains(document.elementFromPoint(sr.x+sr.width/2,sr.y+sr.height/2)),menu:m.toJSON(),card:c.toJSON(),rowHit:row.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)),addHit:button.contains(document.elementFromPoint(a.x+a.width/2,a.y+a.height/2)),scroll:menu.scrollHeight>menu.clientHeight,overflow:document.documentElement.scrollWidth>innerWidth,viewport:innerWidth};
  });
  check(g.menu.width>200&&Math.abs(g.menu.left-g.card.left)<2&&Math.abs(g.menu.width-g.card.width)<2,label+' width/alignment '+JSON.stringify(g));
  check(g.menu.top>=11&&g.menu.bottom<=g.card.top-6,label+' overlaps composer or viewport');
  check(g.rowHit&&g.addHit&&g.sendHit&&!g.overflow,label+' hit target/overflow');
  if(label.endsWith('-long')||label.endsWith('-scrolled'))check(g.scrollRight<=14&&g.actionsBelow,label+' scrollbar/action boundary');results.push({label,...g});
 };
 for(const surface of ['desktop','web']){
  await page.goto(base+(surface==='desktop'?'/desktop-settings-preview.html?surface=chat':'/web.html#/chat'));
  const add=page.getByRole('button',{name:'添加附件或引用',exact:true}),input=page.getByRole('textbox',{name:'写下想法',exact:true});
  await add.waitFor();await page.waitForFunction(()=>!document.querySelector('.add')?.disabled);
  for(const width of [1280,680,390]){
   await page.setViewportSize({width,height:700});
   await page.evaluate(()=>window.fixtureMenuData(24,3,8));
   await input.fill('保留的输入 '+Array.from({length:35},(_,i)=>'长文本 '+i).join('\n'));
   await add.click();await page.getByRole('menuitem',{name:/Plugin 0/}).waitFor();
   await measure(surface+'-'+width+'-long');
   await page.locator('.chat-scroll').evaluate(n=>{n.scrollTop=0});
   await page.locator('.composer-scroll').evaluate(n=>{n.scrollTop=n.scrollHeight});
   await measure(surface+'-'+width+'-scrolled');
   await page.screenshot({path:out+'/'+surface+'-'+width+'.png'});
   const last=page.getByRole('menuitem',{name:/Native 2/});await last.scrollIntoViewIfNeeded();await last.click();
   check(await page.locator('.attachment-menu').count()===0,'selection did not close');
   check(await input.evaluate(n=>document.activeElement===n&&n.innerText.startsWith('保留的输入')),'selection lost draft/focus');
   await add.click();await page.keyboard.press('Escape');check(await add.evaluate(n=>document.activeElement===n),'Escape focus');
   await add.click();await page.locator('.chat-scroll').click({position:{x:8,y:8}});check(await page.locator('.attachment-menu').count()===0,'outside did not close');
   await page.evaluate(()=>window.fixtureMenuData(0,0,0));await input.fill('短文案');await add.click();
   await page.waitForFunction(()=>document.querySelectorAll('.attachment-menu [role=menuitem]').length===1);
   await measure(surface+'-'+width+'-empty');
   await page.keyboard.press('ArrowDown');check(await page.getByRole('menuitem',{name:'添加文件',exact:true}).evaluate(n=>document.activeElement===n),'keyboard item focus');
   if(surface==='desktop'){await page.getByRole('menuitem',{name:'添加文件',exact:true}).click();check(await page.evaluate(()=>fixtureCalls.includes('PickFiles')),'file picker not invoked');}else{await page.keyboard.press('Escape');}
   await input.fill('短文案');
  }
  if(surface==='web'){
   await page.setViewportSize({width:1600,height:850});await page.getByRole('button',{name:'菜单分屏布局检查',exact:true}).click();
   await page.evaluate(()=>window.fixtureMenuData(8,3,2));await add.click();await page.getByRole('menuitem',{name:/Plugin 0/}).waitFor();await measure('web-split');await page.screenshot({path:out+'/web-split.png'});if(surface==='desktop'){await page.getByRole('menuitem',{name:'添加文件',exact:true}).click();check(await page.evaluate(()=>fixtureCalls.includes('PickFiles')),'file picker not invoked');}else{await page.keyboard.press('Escape');}
   await input.fill('短文案');
  }
 }
 check(errors.length===0,errors.join('\n'));
 return {results,errors};
}
