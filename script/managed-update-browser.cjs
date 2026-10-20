// Playwright CLI run-code. Synthetic owner responses; actual shared UI/transport.
async(page)=>{
 const base=new URL(page.url()).origin,out='output/playwright/managed-updates';
 const source=await(await page.request.get(base+'/wails/runtime.js')).text();
 const routes=await page.evaluate(async()=>(await import('/src/web-routes.ts')).webRoutes);
 const names=Object.fromEntries(Object.entries(routes).map(([key,v])=>[v.path,key.split('.').at(-1)]));
 const errors=[],commands=[],receipts={};let drop=false;
 page.on('pageerror',e=>errors.push(e.message));
 let status={protocol:1,mode:'managed',state:'ready',current:{version:'0.17.1-qa.10',source:'a'.repeat(40),kind:'qa'},bootId:'original',offerId:'offer',latest:'0.17.1-qa.11',revision:1,receivedBytes:0,totalBytes:0,automatic:true,scheduled:false,testArtifact:true};
 await page.addInitScript({content:source.replace('export const Call=','window.fixtureCall=')});
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname;
  if(path==='/api/v1/bootstrap')return route.fulfill({json:{csrf:'fixture',version:'FIXTURE-update-ui',capabilities:{web:true}}});
  if(path==='/api/v1/events')return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'});
  if(path==='/api/v1/updates/status')return route.fulfill({json:status});
  if(path.startsWith('/api/v1/updates/receipts/')){const r=receipts[path.split('/').at(-1)];return route.fulfill(r?{json:r}:{status:409,json:{error:'receipt_not_found'}})}
  if(path==='/api/v1/updates/command'){
   const c=route.request().postDataJSON();commands.push(c);receipts[c.id]={id:c.id,state:'accepted',transactionId:'tx'};
   if(c.action==='install')status={...status,state:'waiting_idle',reason:'channel_handoff_required',transactionId:'tx',revision:2};
   if(c.action==='cancel')status={...status,state:'ready',reason:'',revision:3};
   if(drop){drop=false;return route.abort()};return route.fulfill({json:receipts[c.id]});
  }
  const method=names[path];if(!method)return route.fulfill({status:404,json:{error:'fixture_only'}});
  const args=route.request().postDataJSON()?.args??[];
  try{const value=await page.evaluate(({method,args})=>window.fixtureCall.ByName(method,...args),{method,args});return route.fulfill({json:{value}})}catch{return route.fulfill({status:503,json:{error:'fixture_only'}})}
 });
 await page.goto(base+'/web.html#/settings/updates');
 await page.getByRole('button',{name:'安装并重启',exact:true}).waitFor();
 for(const width of [1440,760,390]){await page.setViewportSize({width,height:820});await page.screenshot({path:`${out}/ready-${width}.png`});if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth))throw Error('horizontal overflow '+width)}
 await page.getByRole('button',{name:'空闲时安装',exact:true}).click();
 await page.getByText('消息渠道还有待完成的交互，完成后会继续安装。',{exact:true}).waitFor();
 await page.screenshot({path:out+'/channel-pending-390.png'});
 await page.getByRole('button',{name:'取消',exact:true}).click();await page.getByRole('button',{name:'安装并重启',exact:true}).waitFor();
 drop=true;await page.getByRole('button',{name:'安装并重启',exact:true}).click();await page.getByText('消息渠道还有待完成的交互，完成后会继续安装。',{exact:true}).waitFor();await page.reload();await page.getByText('已安排安装，尚未重启。',{exact:false}).waitFor();
 if(commands.filter(c=>c.action==='install').length!==2)throw Error('lost reply/reload repeated install');
 status={...status,state:'downloading',reason:'',receivedBytes:1048576,totalBytes:4194304};await page.getByRole('progressbar').waitFor();await page.waitForFunction(()=>document.querySelector('progress')?.value===25);await page.screenshot({path:out+'/download-390.png'});
 status={...status,totalBytes:0};await page.waitForFunction(()=>!document.querySelector('progress')?.hasAttribute('value'));
 status={...status,state:'rolled_back',reason:'new_version_unhealthy'};await page.getByText('已恢复到上方显示的原版本，用户数据保留。',{exact:true}).waitFor();await page.screenshot({path:out+'/rollback-390.png'});
 status={...status,state:'installed',reason:'',bootId:'new',current:{...status.current,version:'0.17.1-qa.11'}};await page.getByRole('button',{name:'重新载入',exact:true}).waitFor();
 status={...status,mode:'external',state:'idle',reason:'external_installation'};await page.getByText('此安装由服务主机管理，请在主机更新。',{exact:true}).waitFor();
 if(await page.getByRole('button',{name:'安装并重启',exact:true}).count())throw Error('fake unmanaged install');await page.screenshot({path:out+'/external-390.png'});
 if(errors.length)throw Error(errors.join(';'));
 return {fixture:true,widths:[1440,760,390],channelReason:true,realByteProjection:true,indeterminateWithoutTotal:true,rollback:true,unmanagedNoInstall:true,installCommands:commands.filter(c=>c.action==='install').length,unknownReplyReloadDidNotReplay:true,pageErrors:errors};
}
