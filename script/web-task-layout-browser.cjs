// Synthetic API fixture; production Web transport, task pane and shared chat components.
async(page)=>{
 const base=new URL(page.url()).origin,errors=[],calls=[],out='output/playwright/ui-findings/layout-refinement';
 const check=(v,m)=>{if(!v)throw Error(m)};let empty=false,failRead=false,delayRead=false,delayed=0;
 page.on('pageerror',e=>errors.push(e.message));
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,r])=>[r.path,name.split('.').at(-1)]));
 await page.addInitScript({content:source.replace('export const Call=','window.fixtureCall=')});
 const tasks=[{id:'task-one',title:'整理本周调研与参考资料',status:'running',createdAt:'2026-10-20T09:00:00Z'},{id:'task-two',title:'检查页面布局与图片交付',status:'completed',createdAt:'2026-10-20T08:30:00Z'},{id:'task-three',title:'核对定时提醒',status:'unknown',createdAt:'2026-10-20T07:20:00Z'},...Array.from({length:7},(_,i)=>({id:'task-'+(i+4),title:'保留的工作 '+(i+4),status:'completed',createdAt:'2026-10-19T12:00:00Z'}))];
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;calls.push(path);
  if(path==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'FIXTURE-task-layout',capabilities:{web:true}}});
  if(path==='/api/v1/composer/claim')return route.fulfill({json:{editing:true,epoch:'fixture'}});
  if(path==='/api/v1/events')return route.fulfill({contentType:'text/event-stream',body:': synthetic\n\n'});
  if(path==='/api/v1/tasks/list')return route.fulfill({json:{value:empty?[]:tasks}});
  if(path==='/api/v1/tasks/conversation'){
   const id=route.request().postDataJSON().args[0];if(failRead&&id==='task-three')return route.fulfill({status:503,json:{error:'fixture_read_unavailable'}});if(delayRead&&id==='task-one'){delayed++;await new Promise(r=>setTimeout(r,1000));}return route.fulfill({json:{value:{status:tasks.find(x=>x.id===id)?.status,messages:[{id:id+'-input',role:'user',text:'请核对已有资料，整理主要结论和需要继续确认的内容。仅阅读原任务记录。'},{id:id+'-reply',role:'assistant',text:tasks.find(t=>t.id===id).title+'：已完成资料核对。\n\n### 当前进展\n\n- 原始材料和引用均已保留。\n- 页面布局按桌面与移动端分别检查。\n- 待确认项已单独列出。\n\n| 检查项 | 结果 |\n| --- | --- |\n| 消息连续性 | 已通过 |\n| 附件记录 | 保留 |\n\n下一步会继续核对交付说明。'},{id:id+'-internal',role:'tool',text:'Internal tool record must stay out of the reading pane'},{id:id+'-followup',role:'user',text:'请完整保留这些要求，并按照原顺序继续说明。'},{id:id+'-final',role:'assistant',text:'## 最后核对\n\n原记录保持不变，结论按原消息顺序显示。'}]}}});
  }
  const args=route.request().postDataJSON()?.args??[],method=names[path];
  if(!method)return route.fulfill({status:404,json:{error:'fixture_unavailable'}});
  const value=await page.evaluate(async({method,args})=>window.fixtureCall.ByName(method,...args),{method,args});
  const snapshot=value?.snapshot||value;
  if(Array.isArray(snapshot?.items))snapshot.items.push(...tasks.slice(0,2).map(t=>({id:'link-'+t.id,kind:'assistant',text:t.title,task:{id:t.id,status:t.status},status:'completed',artifacts:[]})));
  return route.fulfill({json:{value}});
 });
 await page.setViewportSize({width:1600,height:1000});await page.goto(base+'/web.html#/chat');
 await page.getByRole('textbox',{name:'写下想法',exact:true}).waitFor();

 await page.getByText('fixture-notes.txt',{exact:true}).waitFor();
 await page.waitForFunction(()=>document.querySelector('textarea')?.value.includes('retained draft'));
 const retained=()=>page.evaluate(()=>({draft:document.querySelector('textarea')?.value,files:document.querySelectorAll('[aria-label="移除 fixture-notes.txt"]').length,scroll:document.querySelector('.chat-scroll')?.scrollTop,same:window.retainedComposer===document.querySelector('textarea')}));
 await page.evaluate(()=>{window.retainedComposer=document.querySelector('textarea');document.querySelector('.chat-scroll').scrollTop=140});
 await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
 const before=await retained();
 const pane=page.getByRole('complementary',{name:'后台任务'});
 const select=async(name)=>{await pane.getByRole('button',{name:'选择任务',exact:true}).click();await page.getByRole('menuitemradio',{name,exact:true}).click();};
 check(!await pane.isVisible(),'task pane opened without selection');
 await page.getByRole('button',{name:tasks[0].title,exact:true}).click();
 await pane.getByText('整理本周调研与参考资料：已完成资料核对。',{exact:true}).waitFor();
 await page.getByRole('button',{name:tasks[0].title,exact:true}).click();
 await pane.waitFor({state:'hidden'});check(!await pane.isVisible(),'same task bubble did not close pane');
 await page.getByRole('button',{name:tasks[0].title,exact:true}).click();
 await page.getByRole('button',{name:tasks[1].title,exact:true}).click();
 await pane.getByText('检查页面布局与图片交付：已完成资料核对。',{exact:true}).waitFor();
 await select(tasks[0].title);
 await pane.getByText('整理本周调研与参考资料：已完成资料核对。',{exact:true}).waitFor();
 check(await pane.getByRole('button',{name:'关闭任务面板',exact:true}).isVisible(),'wide close missing');
 const bar=await page.locator('.chat-scroll').evaluate(n=>({width:getComputedStyle(n).scrollbarWidth,colors:getComputedStyle(n).scrollbarColor}));
 check(bar.width==='thin'&&bar.colors.endsWith('rgba(0, 0, 0, 0)'),'chat scrollbar track is opaque');
 const visibleMessages=await pane.locator('.web-task-message').evaluateAll(nodes=>nodes.map(n=>({role:n.classList.contains('user')?'user':'assistant',text:n.textContent})));
 check(visibleMessages.map(m=>m.role).join(',')==='user,assistant,user,assistant','message order changed or non-dialogue role rendered');
 check(visibleMessages[0].text.includes('请核对已有资料')&&visibleMessages[3].text.includes('最后核对'),'result-first ordering or request missing');
 check(await pane.locator('details,.bot-avatar,.web-task-author,.web-task-meta,time').count()===0,'task body contains collapsed requirement, avatar or status/time labels');
 check(await pane.getByText('请完整保留这些要求，并按照原顺序继续说明。',{exact:true}).isVisible(),'request hidden');
 const stableBefore=await retained();
 await page.screenshot({path:out+'/tasks-after-1600.png'});
 await pane.getByRole('button',{name:'选择任务',exact:true}).click();
 check(await page.getByRole('menuitemradio').count()===10,'missing task choices');
 await page.screenshot({path:out+'/tasks-choices-1600.png'});await page.keyboard.press('Escape');
 // Late response from the previous task must not replace the new selection.
 delayRead=true;
 while(!delayed)await page.waitForTimeout(100);
 await select('检查页面布局与图片交付');
 await pane.getByText('检查页面布局与图片交付：已完成资料核对。',{exact:true}).waitFor();await page.waitForTimeout(1200);delayRead=false;
 check(await pane.getByText('整理本周调研与参考资料：已完成资料核对。',{exact:true}).count()===0,'late read replaced selected result');
 failRead=true;await select('核对定时提醒');await pane.getByText('暂时无法读取这项工作的记录。',{exact:true}).waitFor();
 failRead=false;await pane.getByRole('button',{name:'重新读取',exact:true}).click();await pane.getByText('核对定时提醒：已完成资料核对。',{exact:true}).waitFor();
 await select('检查页面布局与图片交付');
 await page.getByRole('button',{name:'设置',exact:true}).click();await page.getByRole('button',{name:'关闭设置',exact:true}).click();
 const afterSettings=await retained();check(JSON.stringify(afterSettings)===JSON.stringify(stableBefore),'settings lost chat state '+JSON.stringify({before:stableBefore,after:afterSettings}));
 const widths=[];
 for(const width of [2048,1200,900,390]){
  await page.setViewportSize({width,height:900});
  await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
  if(!await pane.isVisible()){await page.getByRole('button',{name:'后台任务',exact:true}).click();await pane.waitFor();}
  check(await pane.getByRole('button',{name:'选择任务',exact:true}).innerText()==='检查页面布局与图片交付','task selection lost when returning from settings/chat');
  check(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'horizontal overflow '+width);
  if(width>=1000){
   const workspace=await page.locator('.web-workspace').boundingBox(),chat=await page.locator('.chat-content').boundingBox(),task=await pane.boundingBox();
   check(workspace.x===0&&workspace.width===width,'split workspace wastes outer width '+width);
   check(chat.x<=Math.max(24,width*.05)&&task.width>=width*.42,'split reading widths are not useful '+JSON.stringify({width,chat,task}));
  }
  await page.screenshot({path:out+'/tasks-after-'+width+'.png'});
  if(width<1000){await pane.getByRole('button',{name:'关闭任务面板',exact:true}).click();await pane.waitFor({state:'hidden'});await page.getByRole('textbox',{name:'写下想法',exact:true}).waitFor();}
  const value=await retained();check(value.same&&value.draft===before.draft&&value.files===1,'narrow task switch lost draft/files/composer '+width);
  widths.push(width);
 }
 // Same-width switch must preserve exact scroll, not reset to the tail.
 await page.evaluate(()=>document.querySelector('.chat-scroll').scrollTop=120);const narrow=await retained();
 await page.getByRole('button',{name:'后台任务',exact:true}).click();await pane.getByRole('button',{name:'关闭任务面板',exact:true}).click();await pane.waitFor({state:'hidden'});
 check(JSON.stringify(await retained())===JSON.stringify(narrow),'narrow task switch moved chat scroll');
 empty=true;await page.setViewportSize({width:1600,height:1000});await page.waitForFunction(()=>!document.querySelector('.web-workspace')?.classList.contains('has-tasks'));
 const centered=await page.locator('.web-chat').boundingBox();check(Math.abs(centered.x+(centered.width/2)-800)<2,'empty chat not centered');
 await page.screenshot({path:out+'/tasks-empty-1600.png'});
 check(!calls.some(p=>/tasks\/(start|send|interrupt|cancel|report|ack)/.test(p)),'task execution endpoint used');
 check(errors.length===0,errors.join('\n'));
 return {fixture:true,originalMessageOrder:true,fullRequirements:true,noAvatarsOrStatusLabels:true,sameBubbleToggles:true,otherBubbleSwitches:true,closeOnlyHides:true,taskCount:tasks.length,widths:[1600,...widths],delayedPreviousTaskIgnored:true,localReadRetry:true,draftFilesComposerAndScrollRetained:true,noTaskCentered:true,taskCalls:[...new Set(calls.filter(p=>p.includes('/tasks/')))],errors};
}
