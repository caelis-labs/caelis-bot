// Run on script/input-menu-preview.mjs with playwright-cli run-code --filename.
// Synthetic protocol edge cases complement the separately recorded live Worker.
async page=>{
 const base=new URL(page.url()).origin,out='output/playwright/ui245',errors=[];
 const check=(value,message)=>{if(!value)throw Error(message)};
 page.on('pageerror',error=>errors.push(error.message));
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([name,r])=>[r.path,name.split('.').at(-1)]));
 await page.addInitScript({content:source.replace('export const Call=','window.fixtureCall=')});
 const tasks=[...Array.from({length:18},(_,i)=>({id:'past-'+i,title:'历史记录 '+i,status:'completed',createdAt:'2026-10-28T12:00:00Z'})),{id:'current',title:'当前阅读验证：核对公开资料中的长标题与文件路径，不挤占状态和返回入口',status:'running',createdAt:'2026-10-28T11:00:00Z'},{id:'unknown',title:'等待原回执',status:'unknown',createdAt:'2026-10-28T10:00:00Z'}];
 const task=tasks.find(t=>t.id==='current');
 let text=Array.from({length:60},(_,i)=>`段落 ${i+1}：原始内容保持顺序，可上翻阅读，不被后续输出打断。`).join('\n\n'),itemStatus='inProgress';
 let listFails=true;
 const calls=[];
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;calls.push(path);
  if(path==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'FIXTURE-worker-reading',capabilities:{web:true},fixture:true}});
  if(path==='/api/v1/composer/claim')return route.fulfill({json:{editing:true,epoch:'fixture'}});
  if(path==='/api/v1/events')return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'});
  if(path==='/api/v1/tasks/list'&&listFails)return route.fulfill({status:503,json:{error:'fixture_read_failure'}});
  if(path==='/api/v1/tasks/list')return route.fulfill({json:{value:tasks}});
  const args=route.request().postDataJSON()?.args??[],selected=tasks.find(t=>t.id===args[0]);
  if(path==='/api/v1/tasks/worker-observation')return route.fulfill({json:{value:{task:selected,items:args[0]==='current'?[{id:'live',historyId:'reply',kind:'assistant',text,status:itemStatus}]:[],revision:'fixture'}}});
  if(path==='/api/v1/tasks/conversation')return route.fulfill({json:{value:{status:selected?.status,messages:args[0]==='current'?[]:[{id:'past-reply',role:'assistant',text:'保留的完整历史内容'}]}}});
  const method=names[path];if(!method)return route.fulfill({status:404,json:{error:'fixture_unavailable'}});
  const value=await page.evaluate(async({method,args})=>fixtureCall.ByName(method,...args),{method,args});
  return route.fulfill({json:{value}});
 });
 await page.setViewportSize({width:1440,height:900});await page.emulateMedia({colorScheme:'dark',reducedMotion:'no-preference'});
 await page.goto(base+'/web.html#/chat');await page.getByRole('button',{name:'后台任务',exact:true}).click();
 const pane=page.locator('.web-task-pane'),scroll=pane.locator('.worker-chat-scroll'),body=pane.locator('[data-worker-message="reply"] .markdown-body');
 listFails=false;
 await pane.locator('.text-action').click();
 await body.waitFor();
 check((await body.innerText()).includes('段落 60'),'initial observation replayed history');
 // Recovery chooses current work without a task in the navigation URL. That
 // visible selection must survive completion and subsequent list refreshes.
 const listRead=()=>page.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1/tasks/list'&&r.ok());
 let refreshed=listRead();task.status='completed';itemStatus='completed';await refreshed;
 await page.waitForTimeout(100);
 check(await body.count()===1,'completion replaced the task selected after list recovery');
 await pane.getByRole('button',{name:'关闭任务面板',exact:true}).click();
 refreshed=listRead();task.status='running';itemStatus='inProgress';await refreshed;
 await page.waitForTimeout(100);
 await page.getByRole('button',{name:'后台任务',exact:true}).click();await body.waitFor();
 check(page.url().endsWith('/current'),'initial choice did not prefer current work');
 await pane.getByRole('button',{name:'选择任务',exact:true}).click();
 check(await page.getByRole('menuitemradio').count()===2,'completed tasks crowd default menu');
 await page.getByRole('menuitem',{name:'查看历史任务（18）',exact:true}).click();
 check(await page.getByRole('menuitemradio').count()===20,'history inaccessible');
 await page.getByRole('textbox',{name:'搜索任务',exact:true}).fill('历史记录 17');
 check(await page.getByRole('menuitemradio').count()===1,'history search failed');
 await page.getByRole('menuitem',{name:'收起历史任务',exact:true}).click();
 check(await page.getByRole('menuitemradio').count()===2,'collapsing history must clear its search');
 await page.keyboard.press('Escape');
 await scroll.evaluate(node=>{node.scrollTop=180;window.readingNode=node;window.replyNode=document.querySelector('[data-worker-message="reply"]')});
 await page.waitForTimeout(100);const before=await scroll.evaluate(n=>n.scrollTop);
 const sample=async duration=>page.evaluate(duration=>new Promise(resolve=>{
  const start=performance.now(),values=[];function frame(now){const node=document.querySelector('.worker-chat-scroll'),body=document.querySelector('[data-worker-message="reply"] .markdown-body');values.push({ms:Math.round(now-start),length:body.textContent.length,top:node.scrollTop,gap:node.scrollHeight-node.clientHeight-node.scrollTop});if(now-start<duration)requestAnimationFrame(frame);else resolve(values)}requestAnimationFrame(frame);
 }),duration);
 text+='\n\n'+('新增字素 👩🏽‍💻 保持连续。'.repeat(22));
 const reading=await sample(6000);
 check(reading.every(v=>Math.abs(v.top-before)<=1),'stream stole history position '+JSON.stringify({before,first:reading[0],last:reading.at(-1),firstMoved:reading.find(v=>Math.abs(v.top-before)>1)}));
 check(new Set(reading.map(v=>v.length)).size>8,'stream updates still arrive in whole polling batches');
 check(await page.evaluate(()=>replyNode===document.querySelector('[data-worker-message="reply"]')),'message identity was replaced');
 // Switching to retained history and back restores this task's reading intent
 // and displays its cached prefix directly, rather than replaying its history.
 await pane.getByRole('button',{name:'选择任务',exact:true}).click();
 await page.getByRole('menuitem',{name:'查看历史任务（18）',exact:true}).click();
 await page.getByRole('textbox',{name:'搜索任务',exact:true}).fill('历史记录 17');
 await page.getByRole('menuitemradio',{name:'历史记录 17',exact:true}).click();
 await pane.locator('[data-worker-message="past-reply"]').waitFor();
 await pane.getByRole('button',{name:'选择任务',exact:true}).click();
 await page.getByRole('menuitem',{name:'收起历史任务',exact:true}).click();
 check(await page.getByRole('menuitemradio').count()===3,'collapsing history hid the selected historical task');
 await page.getByRole('menuitemradio',{name:task.title,exact:true}).click();await body.waitFor();
 check(Math.abs(await scroll.evaluate(n=>n.scrollTop)-before)<=1,'switching tasks lost the original reading position');
 check((await body.textContent()).replace(/\s/g,'')===text.replace(/\s/g,''),'switching tasks replayed or lost the cached prefix');
 await page.evaluate(()=>{window.replyNode=document.querySelector('[data-worker-message="reply"]')});
 const beforeCorrection=(await body.textContent()).length;
 text+='\n\n'+('尚在呈现的旧增量。'.repeat(250));
 await page.waitForFunction(length=>document.querySelector('[data-worker-message="reply"] .markdown-body').textContent.length>length,beforeCorrection);
 check((await body.textContent()).replace(/\s/g,'')!==text.replace(/\s/g,''),'correction fixture did not create a reveal backlog');
 text=Array.from({length:60},(_,i)=>`修正段落 ${i+1}：权威正文替换此前内容，不保留旧的呈现积压。`).join('\n\n');
 await page.waitForFunction(expected=>document.querySelector('[data-worker-message="reply"] .markdown-body').textContent.replace(/\s/g,'')===expected.replace(/\s/g,''),text);
 check(await page.evaluate(()=>replyNode===document.querySelector('[data-worker-message="reply"]')),'correction replaced the native message identity');
 await scroll.evaluate(n=>{n.scrollTop=n.scrollHeight});await page.waitForTimeout(100);
 text+='\n\n'+('继续在底部自然跟随。'.repeat(20));
 const following=await sample(4500);
 check(following.at(-1).gap<=2,'manual return to bottom did not resume following');
 await page.emulateMedia({reducedMotion:'reduce'});
 await page.waitForFunction(expected=>document.querySelector('[data-worker-message="reply"] .markdown-body')?.textContent.replace(/\s/g,'')===expected.replace(/\s/g,''),text);
 const reducedBefore=(await body.textContent()).length;
 text+='\n\n'+('减少动态效果直接呈现。'.repeat(30));
 const reduced=await sample(4000);
 const reducedAfter=(await body.textContent()).length;
 check(reducedAfter>reducedBefore&&reduced.every(v=>v.length===reducedBefore||v.length===reducedAfter),'reduced-motion output was paced');
 check((await body.textContent()).replace(/\s/g,'')===text.replace(/\s/g,''),'reduced-motion output missing text');
 await page.emulateMedia({reducedMotion:'no-preference'});
 // End with a backlog: completion must expose all authoritative text immediately.
 // The task can end while its last item still carries an in-progress label.
 text+='\n\n'+('完成正文不重播。'.repeat(150));task.status='completed';
 await page.waitForFunction(()=>document.querySelector('[data-worker-message="reply"] .markdown-body')?.textContent.endsWith('完成正文不重播。'));
 await page.waitForTimeout(2700);
 check(page.url().endsWith('/current'),'completion changed selected task');
 check(await page.evaluate(()=>replyNode===document.querySelector('[data-worker-message="reply"]')),'completion remounted selected content');
 await pane.getByRole('button',{name:'选择任务',exact:true}).click();
 check(await page.getByRole('menuitemradio').count()===2,'selected completion or unknown work lost');
 await page.keyboard.press('Escape');
 for(const mode of ['light','dark'])for(const width of [1440,390]){
  await page.emulateMedia({colorScheme:mode});await page.setViewportSize({width,height:900});
  check(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'horizontal overflow');
  await page.screenshot({path:`${out}/fixture-${mode}-${width}.png`});
 }
 await pane.getByRole('button',{name:'关闭任务面板',exact:true}).click();
 await page.getByRole('textbox',{name:'写下想法',exact:true}).waitFor();
 check(!calls.some(path=>/tasks\/(start|send|interrupt|cancel|report|ack)/.test(path)),'observer called execution endpoint');
 check(errors.length===0,errors.join('\n'));
 return {fixture:true,defaultCurrent:2,retainedHistory:18,historySearch:true,recoveredSelectionOnCompletion:true,taskSwitchRestoresReading:true,correctionFlushesBacklog:true,reducedMotionDirect:true,terminalTaskFlushesLiveItem:true,stableSelectionOnCompletion:true,readingTop:before,readingFrames:reading.length,readingStages:new Set(reading.map(v=>v.length)).size,followingFrames:following.length,finalBottomGap:following.at(-1).gap,errors};
}
