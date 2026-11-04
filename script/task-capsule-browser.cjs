// Run on script/input-menu-preview.mjs /acceptance. Production chat UI,
// synthetic task presentations and a no-op OpenTaskTerminal only.
async page=>{
 const out='output/playwright/task-capsules',results=[],errors=[];
 const check=(ok,message)=>{if(!ok)throw Error(message)};
 page.on('pageerror',error=>errors.push(error.message));
 await page.goto(new URL('/acceptance?view=chat',page.url()).href);
 await page.waitForFunction(()=>document.querySelector('.composer-editor')?.contentEditable==='true');
 const cases=[['original','对齐 ACP SDK 并发布','completed'],['working','聊天光标对齐修复','working'],['approval','确认发布范围','waiting_approval'],['failed','检查合成构建结果','failed'],['interrupted','已停止的合成任务','interrupted'],['unknown','状态待核对的合成任务','unknown'],['long','长标题：'+ '保留任务身份和完整打开交互 · '.repeat(16),'queued']].map(([id,title,status])=>({id,title,status}));
 await page.evaluate(tasks=>{fixtureTaskData(tasks);fixtureMenuData(0,0,0)},cases);
 await page.locator('.task-message-action').first().waitFor();
 for(const theme of ['light','dark'])for(const width of [390,1040]){
  await page.setViewportSize({width,height:800});await page.evaluate(theme=>document.documentElement.dataset.mode=theme,theme);
  const geometry=await page.locator('.task-message-action').evaluateAll(buttons=>buttons.map(button=>{
   const r=button.getBoundingClientRect(),title=button.querySelector('.task-message-title'),t=title.getBoundingClientRect();
   return {state:button.dataset.state,title:button.title,text:button.innerText,height:r.height,x:r.x,right:r.right,titleWidth:t.width,truncated:title.scrollWidth>title.clientWidth,radius:getComputedStyle(button).borderRadius,background:getComputedStyle(button).backgroundColor,wrapper:getComputedStyle(button.parentElement).padding,hit:button.contains(document.elementFromPoint(r.x+r.width/2,r.y+r.height/2))};
  }));
  check(geometry.length===cases.length&&geometry.every(g=>g.height>=34&&g.height<=40&&g.titleWidth>0&&g.right<=width&&g.hit&&g.wrapper==='0px'),'capsule geometry '+JSON.stringify({width,theme,geometry}));
  check(geometry[0].state==='native.taskFinished'&&geometry[1].state==='native.taskRunning'&&geometry[5].state==='native.taskUnknown','item completion must not override native task status');
  check(geometry.at(-1).title.includes(cases.at(-1).title)&&geometry.at(-1).truncated,'long title lost full hover text or overflowed');
  check(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'horizontal overflow');
  // Exercise the existing action with the exact native task ID, once per layout.
  const before=await page.evaluate(()=>fixturePayloads.filter(p=>p.method==='OpenTaskTerminal').length);
  await page.locator('.task-message-action').first().click();
  check(await page.evaluate(n=>{const calls=fixturePayloads.filter(p=>p.method==='OpenTaskTerminal');return calls.length===n+1&&calls.at(-1).args[0]==='original'},before),'capsule lost original task ID');
  await page.screenshot({path:`${out}/${theme}-${width}.png`});
  results.push({theme,width,geometry});
 }
 check(await page.evaluate(()=>!fixtureCalls.includes('Submit')),'fixture submitted');
 check(!errors.length,errors.join('\n'));return {fixtureOnly:true,submits:0,results};
}
