// Actual React utility UI, synthetic settings transport; no account or runtime.
import assert from 'node:assert/strict';
import {createServer} from 'vite';
import {mkdir,writeFile} from 'node:fs/promises';
import {resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {execFileSync} from 'node:child_process';
import {fixtureRuntime} from './desktop-settings-fixture.mjs';
const {chromium}=await import(pathToFileURL(process.env.WORKER_PEER_PLAYWRIGHT_MODULE).href);
const output=resolve(process.env.REMOTE_UI_OUTPUT||`output/playwright/remote-ui-${Date.now()}`);await mkdir(output,{recursive:true});
const server=await createServer({cacheDir:'.cache/remote-ui-vite',server:{port:0,hmr:false},plugins:[{name:'settings-fixture',configureServer(s){s.middlewares.use((req,res,next)=>{if(req.url?.split('?')[0]!=='/wails/runtime.js')return next();res.setHeader('Content-Type','application/javascript');res.end(fixtureRuntime)})}}]});
let browser,debugPage;const evidence={source:execFileSync('git',['rev-parse','HEAD'],{encoding:'utf8'}).trim(),dirty:!!execFileSync('git',['status','--porcelain','--untracked-files=no'],{encoding:'utf8'}).trim(),level:'Chromium/actual React with synthetic transport; native WKWebView images recorded separately',images:[],assertions:[]};
try{
 await server.listen();const base=server.resolvedUrls.local[0];
 browser=await chromium.launch({executablePath:'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true});
 for(const theme of ['light','dark'])for(const [size,width,height] of [['large',1280,900],['compact',900,680]]){
  const context=await browser.newContext({viewport:{width,height}}),page=await context.newPage();debugPage=page;const errors=[];page.on('pageerror',e=>errors.push(e.message));
  async function visit(state,extra={}){await page.goto(base+'desktop-settings-preview.html?'+new URLSearchParams({surface:'settings',section:'machines',theme,machines:state,...extra}));await page.locator('.remote-add').waitFor();if(state!=='loading')await page.waitForFunction(()=>!document.querySelector('.remote-empty[role=status]'));}
  async function shot(name){
   await page.screenshot({path:resolve(output,`${theme}-${size}-${name}.png`)});
   const metrics=await page.evaluate(()=>{const r=e=>{const b=e?.getBoundingClientRect();return b?{x:b.x,y:b.y,w:b.width,h:b.height}:null};return{viewport:{w:innerWidth,h:innerHeight},overflow:document.documentElement.scrollWidth>innerWidth,toolbar:r(document.querySelector('.remote-toolbar')),list:r(document.querySelector('.remote-machine-list,.remote-empty')),dialog:r(document.querySelector('.remote-bot-dialog,.remote-requests-dialog')),address:r(document.querySelector('.remote-endpoint-fields label:first-child')),port:r(document.querySelector('.remote-endpoint-fields label:last-child')),duplicatedTitle:document.querySelectorAll('.machine-section h1,.machine-section h2').length}});
   assert.equal(metrics.overflow,false);assert.equal(metrics.duplicatedTitle,0);
   if(metrics.dialog){assert.ok(metrics.dialog.x>=0&&metrics.dialog.y>=0&&metrics.dialog.x+metrics.dialog.w<=width+1&&metrics.dialog.y+metrics.dialog.h<=height+1);}
   if(metrics.address){assert.equal(metrics.address.y,metrics.port.y);assert.ok(metrics.port.w<130&&metrics.address.w>metrics.port.w*2);}
   if(metrics.list)assert.ok(Math.abs(metrics.list.x-metrics.toolbar.x)<1);
   evidence.images.push({theme,size,state:name,path:`${theme}-${size}-${name}.png`,metrics});
  }
  await visit('loading');await shot('loading');
  await visit('empty');assert.equal(await page.locator('.remote-inbox-action').count(),0);assert.equal(await page.locator('.remote-secondary').count(),0);await shot('empty');
  await page.getByRole('button',{name:'添加机器',exact:true}).click();await page.locator('.remote-bot-dialog').waitFor();assert.equal(await page.locator('.remote-name').count(),0);assert.equal(await page.getByText('对方批准后即可使用。反向连接需另行授权。',{exact:true}).count(),0);assert.equal(await page.getByLabel('机器地址').inputValue(),'');await shot('add');
  await page.getByLabel('机器地址').fill('100.64.0.5');await page.getByRole('button',{name:'发送连接请求',exact:true}).click();await page.getByText('请求已送达，请在对方 Bot 中批准。',{exact:true}).waitFor();await shot('pending');
  const before=await page.evaluate(()=>fixtureCalls.filter(v=>v==='InspectMachine').length);await page.waitForTimeout(250);assert.equal(await page.evaluate(()=>fixtureCalls.filter(v=>v==='InspectMachine').length),before);
  await visit('list');await shot('list');
  await visit('unknown');assert.equal(await page.getByText('请求已送达，请在对方 Bot 中批准。',{exact:true}).count(),0);await page.locator('.machine-row').first().click();assert.equal(await page.getByRole('button',{name:'发送连接请求',exact:true}).count(),0);await shot('unknown');await page.evaluate(()=>{fixtureUndispatched=true});await page.locator('.remote-bot-dialog').getByRole('button',{name:'重新检查',exact:true}).click();await page.getByRole('button',{name:'发送连接请求',exact:true}).waitFor();assert.equal(await page.evaluate(()=>fixtureCalls.filter(v=>v==='ConnectBot').length),0,'reading unsent proof must not become a submit click');
  await visit('unsent');await page.locator('.machine-row').first().click();await page.getByRole('button',{name:'发送连接请求',exact:true}).waitFor();await page.getByText('此请求尚未发出。可更正地址后发送。',{exact:true}).waitFor();
  await visit('empty',{inbox:'pending'});await page.locator('.remote-inbox-action').click();await page.locator('.remote-requests-dialog').waitFor();await shot('incoming');await page.getByRole('button',{name:'允许连接',exact:true}).click();await page.getByText('暂无连接请求。',{exact:true}).waitFor();await page.getByRole('button',{name:'关闭面板',exact:true}).click();assert.equal(await page.locator('.remote-inbox-action').count(),0);
  await visit('empty',{inbox:'pending',pairingError:'worker_login_required'});await page.locator('.remote-inbox-action').click();await page.getByRole('button',{name:'允许连接',exact:true}).click();await page.getByRole('button',{name:'打开连接与模型设置',exact:true}).waitFor();await shot('approval-recovery');
  await visit('list');await page.evaluate(()=>{fixtureInspectFailure=true});await page.getByRole('button',{name:'重新检查',exact:true}).click();await page.getByText('未能检查',{exact:true}).first().waitFor();assert.equal(await page.locator('.machine-status.ready').count(),0);await page.locator('.machine-row').first().click();await page.evaluate(()=>{fixtureInspectFailure=false});await page.locator('.remote-bot-dialog').getByRole('button',{name:'保存连接',exact:true}).waitFor();await page.locator('.runtime-close').click();await page.getByRole('button',{name:'重新检查',exact:true}).click();await page.locator('.machine-status.ready').waitFor();
  await visit('error');await page.getByText('未能读取机器列表。',{exact:true}).waitFor();assert.equal(await page.getByText('尚未添加远程机器。点击 + 添加。',{exact:true}).count(),0);await shot('list-error');await page.evaluate(()=>{fixtureRemoteFailure=false});await page.getByRole('button',{name:'重试',exact:true}).click();await page.getByText('尚未添加远程机器。点击 + 添加。',{exact:true}).waitFor();
  await visit('empty',{receiveIssue:'worker_peer_state_unsupported'});await page.getByText('此版本无法读取已有的配对记录。',{exact:true}).waitFor();await shot('receive-error');
  assert.deepEqual(errors,[]);await context.close();
 }
 const clockContext=await browser.newContext({viewport:{width:900,height:680}}),clockPage=await clockContext.newPage();await clockPage.clock.install();await clockPage.goto(base+'desktop-settings-preview.html?surface=settings&section=machines&machines=list');await clockPage.locator('.machine-status.ready').waitFor();const checks=await clockPage.evaluate(()=>fixtureCalls.filter(v=>v==='InspectMachine').length);await clockPage.clock.runFor(60100);assert.equal(await clockPage.locator('.machine-status.ready').count(),0,'expired readiness must repaint without network polling');assert.equal(await clockPage.evaluate(()=>fixtureCalls.filter(v=>v==='InspectMachine').length),checks);await clockContext.close();
 evidence.assertions=['40 light/dark large/compact state images; geometry/overflow','only pending inbox shown; disappears after decision','plus accessibility and empty real input','no permanent delivery claim; only successful pending state','unknown has original Check and no resubmit','no pending polling','read failure distinct from empty and explicit retry recovers','readiness has direct settings navigation; no raw error'];
 evidence.result='passed';
}catch(error){if(debugPage&&!debugPage.isClosed()){await debugPage.screenshot({path:resolve(output,'failure.png')});await writeFile(resolve(output,'failure.json'),JSON.stringify(await debugPage.evaluate(()=>({text:document.body.innerText,calls:fixtureCalls,undispatched:window.fixtureUndispatched,machines:fixtureMachines})),null,2));}throw error;}finally{await browser?.close();await server.close();await writeFile(resolve(output,'evidence.json'),JSON.stringify(evidence,null,2));console.log('Remote UI evidence:',output);}
