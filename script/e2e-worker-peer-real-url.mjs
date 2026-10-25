// Actual A/B Apps, HTTP, React and installed Codex. Only model/MCP are controlled.
import assert from 'node:assert/strict';
import {spawn,execFileSync} from 'node:child_process';
import {mkdtemp,mkdir,writeFile,readFile,readdir,rm,realpath} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {resolve,join,dirname} from 'node:path';
import {pathToFileURL} from 'node:url';
import {createHash} from 'node:crypto';
import {host} from './worker-peer-url-contract.mjs';
import {faultRelay} from './worker-peer-e2e-relay.mjs';
const repo=resolve(import.meta.dirname,'..'),binary=await realpath(process.env.CAELIS_BOT_TEST_CODEX);
assert.ok(['darwin','linux'].includes(process.platform),'real URL acceptance currently supports macOS/Linux headless hosts; no Windows GUI claim');
const version=execFileSync(binary,['--version'],{encoding:'utf8'}).trim();assert.match(version,/^codex-cli /,'real Codex required');
const {chromium}=await import(pathToFileURL(resolve(process.env.WORKER_PEER_PLAYWRIGHT_MODULE)).href);
const root=await realpath(await mkdtemp(join(tmpdir(),'bot2bot-real-url-'))),artifacts=resolve(repo,'output/playwright','worker-peer-real-url-'+Date.now());await mkdir(artifacts,{recursive:true});
const evidence={sha:execFileSync('git',['rev-parse','HEAD'],{cwd:repo,encoding:'utf8'}).trim(),sourceTree:process.env.WORKER_PEER_SOURCE_TREE||execFileSync('git',['rev-parse','HEAD^{tree}'],{cwd:repo,encoding:'utf8'}).trim(),worktreeDirty:!!execFileSync('git',['status','--porcelain','--untracked-files=normal'],{cwd:repo,encoding:'utf8'}).trim(),platform:process.platform,arch:process.arch,level:'actual isolated A/B App + HTTP/React + real installed Codex; mock model/MCP, no user account or third-party OAuth',native:{version,sha256:createHash('sha256').update(await readFile(binary)).digest('hex')},steps:[],cases:{}};
const children=[],apps=[],ownedSockets=new Set();let mock,browser,relay,a,b;
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
async function until(label,fn,ms=30000){const end=Date.now()+ms;let last;while(Date.now()<end){try{const v=await fn();if(v)return v}catch(e){last=e}await sleep(100)}throw new Error(label+': '+(last?.message??'timed out'))}
async function stop(p){if(p.exitCode!==null||p.signalCode!==null)return;p.kill('SIGTERM');await until('owned process exit',()=>p.exitCode!==null||p.signalCode!==null,10000).catch(async()=>{p.kill('SIGKILL');await until('owned forced process exit',()=>p.exitCode!==null||p.signalCode!==null,5000)})}
async function state(){return await (await fetch(mock.url+'/state')).json()}
async function control(path,data){const r=await fetch(mock.url+path,{method:'POST',body:JSON.stringify(data)});assert.equal(r.status,200);return r.json()}
async function launch(name){
 const data=join(root,name),home=join(data,'home'),codexHome=join(data,'codex-home');await mkdir(codexHome,{recursive:true,mode:0o700});await mkdir(home,{mode:0o700});
 const json=(n,v)=>writeFile(join(data,n),JSON.stringify(v),{mode:0o600});
 await json('runtime.json',{version:1,runtime:'codex',cliPath:binary});await mkdir(join(data,'runtime-profiles'),{mode:0o700});
 await writeFile(join(data,'runtime-profiles/caelis.json'),JSON.stringify({version:1,runtime:'caelis',cliPath:join(data,'absent-caelis'),caelisStore:join(data,'empty-caelis-store')}),{mode:0o600});
 await json('execution.json',{model:'mock-url-model',effort:'medium',serviceTier:'',approvalMode:name==='A'?'ask':'auto'});
 await json('bot-initialization.json',{version:1,id:'isolated-introduction-'+name,status:'accepted',runtime:'codex'});
 const catalog=join(codexHome,'mock-models.json');
 // Native custom-provider catalog metadata, consumed by the real model/list.
 // This is not a mock of any Bot or Codex product API.
 await writeFile(catalog,JSON.stringify({models:[{slug:'mock-url-model',display_name:'Isolated URL model',description:'Controlled local responses',default_reasoning_level:'medium',supported_reasoning_levels:[{effort:'medium',description:'Synthetic'}],shell_type:'disabled',visibility:'list',supported_in_api:true,priority:0,support_verbosity:false,truncation_policy:{mode:'tokens',limit:10000},experimental_supported_tools:[],base_instructions:'Use the provided isolated MCP tool. This is a synthetic account-free acceptance test.'}]}),{mode:0o600});
 let config=`model_catalog_json = "${catalog}"\nmodel = "mock-url-model"\nmodel_provider = "isolated_url_model"\n[model_providers.isolated_url_model]\nname = "Isolated model"\nbase_url = "${mock.url}/${name}/v1"\nwire_api = "responses"\nrequires_openai_auth = false\nrequest_max_retries = 0\nstream_max_retries = 0\nsupports_websockets = false\n`;
 if(name==='B')config+=`[mcp_servers.isolated_url_mock]\ncommand = "${process.env.WORKER_PEER_PYTHON||'/usr/bin/python3'}"\nargs = ${JSON.stringify(['-u',join(repo,'internal/workerurl/testdata/native_url_mcp.py'),'--mcp',mock.url])}\nstartup_timeout_sec = 10\ntool_timeout_sec = 60\ndefault_tools_approval_mode = "approve"\n`;
 await writeFile(join(codexHome,'config.toml'),config,{mode:0o600});
 // Deliberate allowlist: never inherit user tokens, login/MCP configuration or auth env.
 const env={PATH:process.env.PATH,HOME:home,CODEX_HOME:codexHome,CODEX_BIN:binary,CAELIS_CODEX_SOCKET:'',TMPDIR:root,LANG:'en_US.UTF-8'};
 const child=spawn(join(repo,'.cache/worker-peer-e2e/bin/real-url-bot'),['serve','--data-dir',data,'--listen','127.0.0.1:0'],{cwd:data,env,stdio:['ignore','pipe','pipe']});children.push(child);
 let log='';child.stdout.on('data',v=>log+=v);child.stderr.on('data',v=>log+=v);
 const url=await until('App '+name+' listener',()=>{assert.equal(child.exitCode,null,log);return log.match(/CaelisBot: (http:\/\/[^\s]+)/)?.[1]});
 const app={name,data,url,child,log:()=>log};apps.push(app);return app;
}
async function processCWD(pid){
 if(process.platform==='linux')return await realpath('/proc/'+pid+'/cwd');
 return execFileSync('lsof',['-a','-p',String(pid),'-d','cwd','-Fn'],{encoding:'utf8'}).split('\n').find(v=>v.startsWith('n'))?.slice(1);
}
async function bindings(B){
 const found=[];async function walk(dir){for(const d of await readdir(dir,{withFileTypes:true}).catch(()=>[])){if(d.isDirectory())await walk(join(dir,d.name));else if(d.name==='worker-bindings.json')found.push({path:join(dir,d.name),value:JSON.parse(await readFile(join(dir,d.name),'utf8'))})}}await walk(join(B.data,'WorkerPeers','sources'));return found;
}
async function create(A,B,kind){
 // Completion callbacks are real native turns. Wait for their existing product
 // receipts before the next explicit test prompt; never steer an unobserved turn.
 await until('previous original task reports delivered',async()=>{
  let d;try{d=JSON.parse(await readFile(join(A.data,'tasks.json'),'utf8'))}catch{return true}
  return Object.values(d.records).every(v=>['completed','failed','interrupted','cancelled'].includes(v.view.status)&&['delivered','observed'].includes(v.reportState));
 });
 let settled=0;await until('resident accepts a new explicit turn',async()=>{const r=await host(a,'/api/v1/conversation/snapshot');const v=r.body.value;settled=v.canSend&&!v.canInterrupt&&!v.currentTurn?settled+1:0;return settled>=2});
 if(new URL(a.url()).hash!=='#chat')await a.evaluate(()=>{location.hash='#chat'});
 const input=a.locator('[contenteditable=true]').first();const text='URL_CASE='+kind+' Please delegate this isolated URL challenge to the paired B environment.';
 await until('owned composer and native draft ready',async()=>{
  const takeover=a.getByRole('button',{name:'在此继续编辑',exact:true});if(await takeover.isVisible())await takeover.click();
  if(!await input.isVisible()||await input.evaluate(node=>!!node.closest('[inert]')))return false;
  if(await input.innerText()!==text)await input.fill(text); // reversible draft only; no execution replay
  return await a.getByRole('button',{name:'发送',exact:true}).isEnabled();
 });
 await a.getByRole('button',{name:'发送',exact:true}).click();
 const reviewed=new Set();
 const record=await until(kind+' original task',async()=>{
  let d;try{d=JSON.parse(await readFile(join(A.data,'tasks.json'),'utf8'))}catch{}
  const task=d&&Object.values(d.records).find(v=>v.view.title==='Real URL '+kind);if(task)return task;
  const snapshot=(await host(a,'/api/v1/conversation/snapshot')).body.value;
  const original=snapshot.approvals.find(v=>v.status==='pending'&&v.owner!=='task'&&!reviewed.has(v.id));
  if(original){const dock=a.locator('.approval-dock-trigger');if(await dock.isVisible()&&await dock.getAttribute('aria-expanded')==='false')await dock.click();const choice=a.locator(`[data-approval-id="${original.id}"]:visible .approval-choices .primary-decision`);if(await choice.isVisible()){const response=a.waitForResponse(r=>r.url().endsWith('/api/v1/conversation/decide')&&r.request().postDataJSON().args?.[0]?.id===original.id);await choice.click();assert.equal((await response).status(),200);reviewed.add(original.id);evidence.steps.push('A user approved original resident delegation through React (ask policy)')}}
  return false;
 });
 const native=await until(kind+' actual MCP native request',async()=>{const v=(await state()).cases[kind];return v&&v});
 const approval=await until(kind+' A original approval',async()=>{const r=await host(a,'/api/v1/conversation/snapshot');return r.body.value.approvals.find(v=>v.taskTitle==='Real URL '+kind&&v.status==='pending')});
 const card=a.locator(`[data-approval-id="${approval.id}"]:visible`);await until(kind+' visible card',async()=>{const dock=a.locator('.approval-dock-trigger');if(await dock.isVisible()&&await dock.getAttribute('aria-expanded')==='false')await dock.click();return card.isVisible()});
 const binding=await until('B original native binding',async()=>{for(const x of await bindings(B)){const task=x.value.tasks?.[record.view.id];if(task?.approvalEvidence?.length)return task}});
 const scoped=JSON.parse(Buffer.from(approval.id.slice('worker-approval:'.length),'base64url').toString());assert.equal(scoped.Task,record.view.id);assert.equal(scoped.Native,binding.approvalEvidence[0].view.id);assert.equal(binding.view.id,record.view.id);
 evidence.cases[kind]={task:record.view.id,thread:binding.thread,turn:binding.run,owner:binding.owner,nativeRequest:binding.approvalEvidence[0].nativeId,elicitationID:native.elicitationID,sourceScopedRequest:approval.id};
 return {record,native,approval,card,binding};
}
async function openPage(c){await c.card.locator(':scope > button.text-action').click();const dialog=a.getByRole('dialog',{name:'网站验证',exact:true});await dialog.waitFor();return dialog}
async function closePage(dialog){await dialog.locator('button.runtime-close').click();await dialog.waitFor({state:'hidden'})}
async function consent(c,choice){
 const dock=a.locator('.approval-dock-trigger');if(await dock.isVisible()&&await dock.getAttribute('aria-expanded')==='false')await dock.click();await c.card.waitFor({state:'visible'});
 const response=a.waitForResponse(r=>r.url().endsWith('/api/v1/conversation/decide')&&r.request().postDataJSON().args?.[0]?.id===c.approval.id);
 if(choice==='accept')await c.card.locator('.approval-choices .primary-decision').click();else await c.card.locator('.approval-choices button').filter({hasText:'拒绝'}).first().click();
 assert.equal((await response).status(),200,'original native decision HTTP receipt');
 await until('original decision settled in A',async()=>{const r=await host(a,'/api/v1/conversation/snapshot');return !r.body.value.approvals.some(v=>v.id===c.approval.id&&v.status==='pending')});
}
try{
 const provider=spawn(process.env.WORKER_PEER_PYTHON||'/usr/bin/python3',['-u',join(repo,'internal/workerurl/testdata/native_url_mcp.py')],{stdio:['ignore','pipe','pipe']});children.push(provider);let first='',err='';provider.stdout.on('data',v=>first+=v);provider.stderr.on('data',v=>err+=v);
 mock=await until('temporary MCP/model listener',()=>{assert.equal(provider.exitCode,null,err);try{return JSON.parse(first.split('\n')[0])}catch{}});
 const A=await launch('A'),B=await launch('B');
 browser=await chromium.launch({executablePath:process.env.WORKER_PEER_CHROME||'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true});
 const ac=await browser.newContext({viewport:{width:1280,height:1000}}),bc=await browser.newContext({viewport:{width:1280,height:1000}});a=await ac.newPage();b=await bc.newPage();
 const pageErrors=[];for(const p of [a,b])p.on('pageerror',v=>pageErrors.push(v.message));
 await a.goto(A.url);await b.goto(B.url);
 for(const page of [a,b])await until('actual Codex resident ready',async()=>{const r=await host(page,'/api/v1/conversation/snapshot');return r.body.value.connection==='ready'});
 await a.goto(A.url+'/#settings/machines');await b.goto(B.url+'/#settings/machines');
 await b.getByRole('button',{name:'添加机器',exact:true}).click();await b.getByRole('button',{name:/^IP \/ Bot/}).click();
 await writeFile(join(artifacts,'B-worker-readiness.json'),JSON.stringify(await host(b,'/api/v1/connections/local-worker-selection'),null,2));
 await b.getByRole('button',{name:'生成 6 位授权码',exact:true}).click();await until('pairing information or explicit refusal',async()=>{if(await b.getByRole('alert').isVisible())throw new Error(await b.getByRole('alert').textContent());return await b.locator('.remote-pairing-info code').isVisible()});const code=await b.locator('.remote-pairing-info code').textContent();assert.match(code,/^[0-9]{6}$/);relay=await faultRelay('http://127.0.0.1:'+new URL(await b.getByLabel('Bot 地址',{exact:true}).inputValue()).port);
 await a.getByRole('button',{name:'添加机器',exact:true}).click();await a.getByRole('button',{name:/^IP \/ Bot/}).click();await a.getByLabel('连接信息或地址',{exact:true}).fill(relay.url);await a.getByLabel('配对码',{exact:true}).fill(code);await a.locator('.remote-name summary').click();await a.getByLabel('机器名称',{exact:true}).fill('B real Codex');await a.getByRole('button',{name:'连接',exact:true}).click();await a.locator('.machine-row').filter({hasText:'B real Codex'}).waitFor();
 const profiles=(await host(a,'/api/v1/machines/machines')).body.value;const profile=profiles.find(v=>v.name==='B real Codex');assert.ok(profile);await control('/environment',{id:profile.id});
 evidence.steps.push('Actual A/B Apps and real Codex ready; paired via real React; model output calls actual bot_delegate');
 const success=await create(A,B,'SUCCESS');const dialog=await openPage(success);await dialog.getByLabel('Mock confirmation').fill('synthetic-confirmation');await dialog.getByRole('button',{name:'Complete original challenge',exact:true}).click();await dialog.getByText('网站已接收此提交。请处理原请求以继续。',{exact:true}).waitFor();
 assert.equal((await state()).cases.SUCCESS.nativeEffects,0,'web receipt must not approve native');
 const opID=await a.evaluate(id=>sessionStorage.getItem('caelisbot.url-submit:'+id),success.approval.id);assert.ok(opID);
 const offered=(await host(a,'/api/v1/conversation/approval-page',[success.approval.id])).body.value;
 const duplicate=await host(a,'/api/v1/conversation/approval-page-submit',[success.approval.id,opID,offered.revision,{answer:'synthetic-confirmation'}]);assert.equal(duplicate.body.value.outcome,'accepted');assert.equal((await state()).cases.SUCCESS.webEffects,1);
 const conflict=await host(a,'/api/v1/conversation/approval-page-submit',[success.approval.id,opID,offered.revision,{answer:'changed-intent'}]);assert.equal(conflict.status,503);assert.equal((await state()).cases.SUCCESS.webEffects,1);
 evidence.steps.push('Actual authenticated A handler and peer receive duplicate original URL op once; same ID/different values refused without another POST');

 await a.screenshot({path:join(artifacts,'A-web-submitted-native-pending.png'),fullPage:true});await closePage(dialog);
 await consent(success,'accept');await until('real B native continuation exactly once',async()=>{const v=(await state()).cases.SUCCESS;return v.nativeEffects===1&&v.continuations===1});
 await until('A completed original task',async()=>{const d=JSON.parse(await readFile(join(A.data,'tasks.json'),'utf8'));return d.records[success.record.view.id].view.status==='completed'});
 evidence.steps.push('B-loopback form completed only through A React; original native accept/tool result/continuation once; A task completed');
 const rejected=await create(A,B,'DECLINE');await consent(rejected,'decline');await until('native decline confirmed',async()=>(await state()).cases.DECLINE.decisions.length===1);assert.equal((await state()).cases.DECLINE.nativeEffects,0);assert.equal((await state()).cases.DECLINE.webEffects,0);
 const lost=await create(A,B,'PEER_LOSS');const lostDialog=await openPage(lost);await lostDialog.getByLabel('Mock confirmation').fill('synthetic-confirmation');relay.drop('url-submit');await lostDialog.getByRole('button',{name:'Complete original challenge',exact:true}).click();await lostDialog.getByRole('button',{name:'核对原提交',exact:true}).waitFor();
 const pendingIDs=await a.evaluate(()=>Object.entries(sessionStorage).filter(([key])=>key.startsWith('caelisbot.url-submit:')));assert.ok(pendingIDs.length);
 await lostDialog.getByRole('button',{name:'核对原提交',exact:true}).click();await lostDialog.getByText('网站已接收此提交。请处理原请求以继续。',{exact:true}).waitFor();assert.equal((await state()).cases.PEER_LOSS.webEffects,1);
 await closePage(lostDialog);await consent(lost,'accept');await until('lost-response original native continuation',async()=>(await state()).cases.PEER_LOSS.continuations===1);
 evidence.steps.push('Drop actual peer submit response after B effect; A checks original receipt, no POST replay; native continues once');
 const webLost=await create(A,B,'WEB_LOSS');const webLostDialog=await openPage(webLost);await webLostDialog.getByLabel('Mock confirmation').fill('synthetic-confirmation');await webLostDialog.getByRole('button',{name:'Complete original challenge',exact:true}).click();await webLostDialog.getByRole('button',{name:'核对原提交',exact:true}).waitFor();
 const originalWebOp=await a.evaluate(id=>sessionStorage.getItem('caelisbot.url-submit:'+id),webLost.approval.id);assert.ok(originalWebOp);
 await a.reload();await until('same original request after UI reload',async()=>{const r=await host(a,'/api/v1/conversation/snapshot');return r.body.value.approvals.some(v=>v.id===webLost.approval.id&&v.status==='pending')});
 const reloadDock=a.locator('.approval-dock-trigger');if(await reloadDock.getAttribute('aria-expanded')==='false')await reloadDock.click();
 const resumedDialog=await openPage(webLost);await resumedDialog.getByRole('button',{name:'核对原提交',exact:true}).click();assert.equal((await state()).cases.WEB_LOSS.webEffects,1);assert.equal(await resumedDialog.getByRole('button',{name:'Complete original challenge',exact:true}).count(),0,'unknown submission offered replay');
 await closePage(resumedDialog);await consent(webLost,'accept');await until('native can confirm while prior web effect remains unknown',async()=>(await state()).cases.WEB_LOSS.continuations===1);
 const originalReceipt=await host(a,'/api/v1/conversation/approval-page-operation',[webLost.approval.id,originalWebOp]);assert.equal(originalReceipt.body.value.outcome,'unknown');assert.equal(originalReceipt.body.value.requestId,originalWebOp);
 evidence.steps.push('Lose provider HTTP response after effect; A reload restores original request/op, only checks unknown; no replay; native consent/continuation independently once');
 const expired=await create(A,B,'EXPIRED');await control('/expire',{case:'EXPIRED'});const expiredDialog=await openPage(expired);await expiredDialog.getByText('原网页暂不可用，尚未发送授权决定。',{exact:true}).waitFor();assert.equal((await state()).cases.EXPIRED.webEffects,0);await closePage(expiredDialog);await consent(expired,'decline');
 const gone=await create(A,B,'OWNER_LOSS');const goneDialog=await openPage(gone);await goneDialog.getByLabel('Mock confirmation').waitFor();
 const originalOwner=(await bindings(B)).find(x=>x.value.tasks?.[gone.record.view.id]);assert.ok(originalOwner?.value.ownerProcess?.pid>1);
 const ownerPID=originalOwner.value.ownerProcess.pid,endpoint=originalOwner.value.ownerEndpoint;evidence.cases.OWNER_LOSS.originalEndpoint=endpoint;evidence.cases.OWNER_LOSS.originalProcess=originalOwner.value.ownerProcess;
 const command=execFileSync('ps',['-p',String(ownerPID),'-o','command='],{encoding:'utf8'}).trim();
 const cwd=await processCWD(ownerPID);
 assert.ok(command.startsWith(binary+' app-server ')&&command.includes(endpoint));assert.ok(cwd?.startsWith(join(B.data,'WorkerPeers','sources')+'/'));
 assert.ok(endpoint.startsWith('unix://'));ownedSockets.add(dirname(endpoint.slice(7)));
 process.kill(ownerPID,'SIGKILL');await until('confirmed original private native exit',()=>{try{return !execFileSync('ps',['-p',String(ownerPID),'-o','pid='],{encoding:'utf8'}).trim()}catch{return true}});
 await goneDialog.getByLabel('Mock confirmation').fill('synthetic-confirmation');await goneDialog.getByRole('button',{name:'Complete original challenge',exact:true}).click();await goneDialog.getByRole('button',{name:'核对原提交',exact:true}).waitFor();
 assert.equal((await state()).cases.OWNER_LOSS.webEffects,0);assert.equal((await state()).cases.OWNER_LOSS.nativeEffects,0);
 const preserved=JSON.parse(await readFile(originalOwner.path,'utf8'));assert.equal(preserved.ownerEndpoint,endpoint);assert.equal(preserved.tasks[gone.record.view.id].thread,gone.binding.thread);assert.equal(preserved.tasks[gone.record.view.id].owner,gone.binding.owner);
 await closePage(goneDialog);evidence.steps.push('Verified private B native PID/endpoint/cwd, injected owner exit; stale original URL submission produced zero web/native effects, old owner/thread route preserved without replacement');
 const native=await state();for(const [kind,v]of Object.entries(native.cases)){evidence.cases[kind]={...evidence.cases[kind],webEffects:v.webEffects,nativeEffects:v.nativeEffects,decisions:v.decisions,continuations:v.continuations};assert.ok(v.webEffects<=1&&v.nativeEffects<=1&&v.continuations<=1)}
 const bTasks=await host(b,'/api/v1/tasks/list');assert.equal(bTasks.status,200);assert.ok(Array.isArray(bTasks.body.value));assert.equal(JSON.stringify(bTasks.body).includes(success.record.view.id),false,'B resident listed foreign work');
 const secret='synthetic-confirmation';for(const record of await bindings(B))assert.equal(JSON.stringify(record.value).includes(secret),false,'form values persisted in native binding');
 assert.deepEqual(pageErrors,[]);evidence.result='real Codex A/B/React HTTP-form journey passed';
}catch(error){evidence.result='failed';evidence.error=error.stack;if(b)await writeFile(join(artifacts,'B-visible-alerts.json'),JSON.stringify(await b.locator('[role=alert],.inline-error').allTextContents()));if(a){await a.screenshot({path:join(artifacts,'failure.png'),fullPage:true}).catch(()=>{});await writeFile(join(artifacts,'failure-dom.txt'),await a.locator('body').innerText().catch(()=>''));const x=await host(a,'/api/v1/conversation/snapshot').catch(()=>null);await writeFile(join(artifacts,'failure-snapshot.json'),JSON.stringify(x,null,2));}throw error}
finally{
 if(mock)try{const v=await state();evidence.mockCounts=Object.fromEntries(Object.entries(v.cases).map(([k,x])=>[k,{webEffects:x.webEffects,nativeEffects:x.nativeEffects,decisions:x.decisions,continuations:x.continuations}]));evidence.modelSteps=v.models}catch{}
 await writeFile(join(artifacts,'evidence.json'),JSON.stringify(evidence,null,2));
 if(browser)await browser.close();if(relay)await relay.close();
 for(const app of apps)await writeFile(join(artifacts,app.name+'-app.log'),app.log());
 for(const child of children.slice().reverse())await stop(child);
 // Private native owners can intentionally survive App detach. Kill only real
 // Codex PIDs whose executable, exact endpoint and cwd prove this test's ownership.
 const rows=execFileSync('ps',['-axo','pid=,command='],{encoding:'utf8'}).split('\n');
 for(const row of rows){const match=row.trim().match(/^(\d+) (.+)$/);if(!match||!match[2].startsWith(binary+' app-server '))continue;const pid=+match[1];let cwd;try{cwd=await processCWD(pid)}catch{continue}if(!cwd?.startsWith(root+'/'))continue;const birth=execFileSync('ps',['-p',String(pid),'-o','lstart='],{encoding:'utf8'}).trim();
  const exited=()=>{try{return !execFileSync('ps',['-p',String(pid),'-o','pid='],{encoding:'utf8'}).trim()}catch{return true}};
  process.kill(pid,'SIGTERM');
  if(!await until('owned native graceful exit',exited,5000).then(()=>true,()=>false)){
   const now=execFileSync('ps',['-p',String(pid),'-o','lstart='],{encoding:'utf8'}).trim();
   const command=execFileSync('ps',['-p',String(pid),'-o','command='],{encoding:'utf8'}).trim();
   assert.equal(now,birth,'refuse reused PID');assert.equal(command,match[2],'refuse changed process');
   process.kill(pid,'SIGKILL');await until('owned native forced exit',exited,5000);
  }
  const socket=match[2].match(/--listen unix:\/\/(\/[^ ]+\/runtime\.sock)$/)?.[1];
  if(socket&&/^caelis-bot-[0-9]+$/.test(dirname(socket).split('/').at(-1)))await rm(dirname(socket),{recursive:true,force:true});}
 for(const dir of ownedSockets)if(/^caelis-bot-[0-9]+$/.test(dir.split('/').at(-1)))await rm(dir,{recursive:true,force:true});
 evidence.cleanup='own apps/browser/mock/native processes stopped; isolated HOME/CODEX_HOME removed; daily configuration untouched';
 await writeFile(join(artifacts,'evidence.json'),JSON.stringify(evidence,null,2));await rm(root,{recursive:true,force:true});console.log('Real URL evidence:',artifacts);
}
