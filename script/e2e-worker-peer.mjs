// Real App/HTTP/React journey; only Codex's native wire protocol is synthetic.
import assert from 'node:assert/strict';
import {spawn,execFileSync} from 'node:child_process';
import {mkdtemp,mkdir,writeFile,readFile,readdir,rm,realpath} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {resolve,join,dirname} from 'node:path';
import {pathToFileURL} from 'node:url';
import {createHash} from 'node:crypto';
import {deflateSync} from 'node:zlib';
import {faultRelay} from './worker-peer-e2e-relay.mjs';
import {urlContractJourney,host} from './worker-peer-url-contract.mjs';

const repo=resolve(import.meta.dirname,'..');
const {chromium}=await import(pathToFileURL(resolve(process.env.WORKER_PEER_PLAYWRIGHT_MODULE)).href);
const root=await realpath(await mkdtemp(join(tmpdir(),'bot2bot-app-e2e-')));
const artifacts=resolve(repo,'output/playwright',`worker-peer-${Date.now()}`);
await mkdir(artifacts,{recursive:true});
const sha=process.env.WORKER_PEER_SOURCE_SHA||execFileSync('git',['rev-parse','HEAD'],{cwd:repo,encoding:'utf8'}).trim();
const tree=process.env.WORKER_PEER_SOURCE_TREE||execFileSync('git',['rev-parse','HEAD^{tree}'],{cwd:repo,encoding:'utf8'}).trim();
const worktreeDirty=!!execFileSync('git',['status','--porcelain','--untracked-files=no'],{cwd:repo,encoding:'utf8'}).trim();
const evidence={sha,tree,worktreeDirty,platform:process.platform,arch:process.arch,nativeBuild:'worker-peer-native-fixture/0.1.0',level:'real isolated production A/B App + HTTP + React; synthetic native Runtime; no account/model',steps:[],gates:{remoteLoopbackURL:'not verified',liveMachine:'not run'}};
const children=[],apps=[];let browser,debugPage,debugB,relay;
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
function fixturePNG(){
 const chunk=(kind,data)=>{const type=Buffer.from(kind),body=Buffer.concat([type,data]);let crc=0xffffffff;for(const byte of body){crc^=byte;for(let i=0;i<8;i++)crc=(crc>>>1)^((crc&1)?0xedb88320:0);}const size=Buffer.alloc(4),sum=Buffer.alloc(4);size.writeUInt32BE(data.length);sum.writeUInt32BE((crc^0xffffffff)>>>0);return Buffer.concat([size,body,sum]);};
 const header=Buffer.alloc(13);header.writeUInt32BE(1);header.writeUInt32BE(1,4);header[8]=8;header[9]=6;
 return Buffer.concat([Buffer.from('89504e470d0a1a0a','hex'),chunk('IHDR',header),chunk('IDAT',deflateSync(Buffer.from([0,255,0,0,255]))),chunk('IEND',Buffer.alloc(0))]);
}

async function until(label,fn,ms=30000){const end=Date.now()+ms;let last;while(Date.now()<end){try{const v=await fn();if(v)return v;}catch(e){last=e;}await sleep(100);}throw new Error(`${label}: ${last?.message??'timed out'}`);}
async function stop(child){if(child.exitCode!==null)return;child.kill('SIGTERM');await until('owned App exit',()=>child.exitCode!==null||child.signalCode!==null,15000).catch(()=>child.kill('SIGKILL'));}
async function launch(name){
 const data=join(root,name);await mkdir(data,{mode:0o700});
 const json=(name,v)=>writeFile(join(data,name),JSON.stringify(v),{mode:0o600});
 await json('runtime.json',{version:1,runtime:'codex',cliPath:join(repo,'.cache/worker-peer-e2e/bin/native')});
 await mkdir(join(data,'runtime-profiles'),{mode:0o700});
 await writeFile(join(data,'runtime-profiles/caelis.json'),JSON.stringify({version:1,runtime:'caelis',cliPath:join(data,'absent-caelis'),caelisStore:join(data,'empty-caelis-store')}),{mode:0o600});
 await json('execution.json',{model:'worker-peer-synthetic',effort:'medium',serviceTier:'',approvalMode:'auto'});
 await json('bot-initialization.json',{version:1,id:`synthetic-introduction-${name}`,status:'accepted',runtime:'codex'});


 const env={...process.env};for(const key of Object.keys(env))if(/TOKEN|SECRET|API_KEY|PASSWORD|CREDENTIAL|AUTHORIZATION/i.test(key))delete env[key];Object.assign(env,{CODEX_BIN:join(repo,'.cache/worker-peer-e2e/bin/native'),CODEX_HOME:join(data,'empty-codex-home'),CAELIS_CODEX_SOCKET:'',WORKER_PEER_NATIVE_FIXTURE:'1',WORKER_PEER_FIXTURE_ROOT:root});
 const child=spawn(join(repo,'.cache/worker-peer-e2e/bin/bot'),['serve','--data-dir',data,'--listen','127.0.0.1:0'],{cwd:data,env,stdio:['ignore','pipe','pipe']});children.push(child);
 let output='';child.stdout.on('data',v=>output+=v);child.stderr.on('data',v=>output+=v);
 const url=await until(`App ${name} actual listener`,()=>{assert.equal(child.exitCode,null,output);return output.match(/CaelisBot: (http:\/\/[^\s]+)/)?.[1];});
 const app={name,data,child,url,log:()=>output};apps.push(app);return app;
}
async function shot(page,name){await page.screenshot({path:join(artifacts,`${name}.png`),fullPage:true});}
async function send(page,request){const input=page.locator('[contenteditable="true"]').first();await input.fill('WORKER_PEER_FIXTURE '+JSON.stringify(request));await page.getByRole('button',{name:'发送',exact:true}).click();}
async function nativeStates(data){const out=[];async function walk(dir){for(const d of await readdir(dir,{withFileTypes:true})){if(d.isDirectory())await walk(join(dir,d.name));else if(d.name==='state.json'&&dir.endsWith('.worker-peer-native-fixture')){try{out.push(JSON.parse(await readFile(join(dir,d.name),'utf8')));}catch{}}}}await walk(data);return out;}
try{
 const A=await launch('A'),B=await launch('B');
 browser=await chromium.launch({executablePath:process.env.WORKER_PEER_CHROME||'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',headless:true});
 const context=await browser.newContext({acceptDownloads:true,viewport:{width:1280,height:1000}});
 // Separate browser profiles model A and B on different machines. The product
 // additionally namespaces its authentication cookie by data directory.
 const bContext=await browser.newContext({viewport:{width:1280,height:1000}});
 const a=await context.newPage(),b=await bContext.newPage();
 evidence.machineErrors=[];
 a.on('response',async response=>{if(response.url().includes('/api/v1/machines/')&&response.status()>=400){try{const body=await response.json();evidence.machineErrors.push({path:new URL(response.url()).pathname,status:response.status(),error:body.error,detail:body.detail,recovery:body.recovery});}catch{}}});
 const errors=[];for(const page of [a,b])page.on('pageerror',e=>errors.push(e.message));
 await a.goto(A.url);await b.goto(B.url);
 await a.locator('[contenteditable="true"]').first().waitFor({state:'visible',timeout:30000});
 await b.locator('[contenteditable="true"]').first().waitFor({state:'visible',timeout:30000});
 evidence.steps.push('Production A/B headless apps and actual React chat ready; only isolated fixture configuration seeded');
 // Pairing requires an already-ready local Worker. A visible composer alone
 // does not prove the native handshake has finished. These are safe reads.
 for(const page of [a,b])await until('configured resident native ready',async()=>{const r=await host(page,'/api/v1/conversation/snapshot');return r.status===200&&r.body.value.connection==='ready';});
 // Further steps drive visible controls, not substitute product HTTP responses.
 await a.goto(A.url+'/#settings/machines');await b.goto(B.url+'/#settings/machines');debugPage=a;debugB=b;
 await b.locator('.remote-empty').waitFor();await shot(b,'B-empty-remote-list');
 relay=await faultRelay(B.url);
 async function submit(page,address,name){await page.getByRole('button',{name:'添加机器',exact:true}).click();await page.getByLabel('机器地址',{exact:true}).fill(address);await page.getByRole('button',{name:'发送连接请求',exact:true}).click();await page.locator('.runtime-dialog').waitFor({state:'hidden'});}
 async function approve(page){await page.getByRole('button',{name:'重新检查',exact:true}).click();await page.locator('.remote-inbox-action').click();await page.getByRole('button',{name:'允许连接',exact:true}).click();await until('B request decided',async()=>await page.locator('.remote-request-list li').count()===0);await page.locator('.runtime-close').click();}
 async function check(page,name,rename){await page.locator('.machine-row').filter({hasText:name}).click();await page.locator('.remote-bot-dialog').getByRole('button',{name:'重新检查',exact:true}).click();await page.getByRole('button',{name:'保存连接',exact:true}).waitFor();if(rename){await page.getByLabel('机器名称',{exact:true}).fill(rename);await page.getByRole('button',{name:'保存连接',exact:true}).click();await page.locator('.remote-bot-dialog').waitFor({state:'hidden'});}else await page.locator('.runtime-close').click();await page.locator('.machine-row').filter({hasText:rename||name}).locator('.machine-status.ready').waitFor();}
 await submit(a,relay.url,'B synthetic');
 const originalConnection=(await host(a,'/api/v1/machines/machines')).body.value.find(v=>v.address===relay.url);assert.equal(originalConnection.pairingStatus,'pending');
 assert.equal(Object.keys(JSON.parse(await readFile(join(B.data,'WorkerPeers','peers.json'),'utf8')).grants).length,0,'submitting a request never grants authority');
 await shot(a,'A-pending-independent');await approve(b);
 // B approval is not a continuous A operation; A stays pending until its
 // explicit check. Local chat/Worker remain independent throughout.
 assert.equal((await host(a,'/api/v1/machines/machines')).body.value[0].pairingStatus,'pending');
 await check(a,relay.url,'B synthetic');
 await submit(b,A.url,'A reverse');await approve(a);await check(b,A.url,'A reverse');
 const reverse=(await host(b,'/api/v1/machines/machines')).body.value.find(v=>v.name==='A reverse');
 const aRoutes=JSON.parse(await readFile(join(A.data,'Machines','machines.json'),'utf8')),bRoutes=JSON.parse(await readFile(join(B.data,'Machines','machines.json'),'utf8'));
 assert.notEqual(aRoutes.profiles[originalConnection.id].peer.token,bRoutes.profiles[reverse.id].peer.token,'directions need independent proofs and decisions');
 await b.locator('.remote-authorizations summary').click();await b.locator('.remote-source-list').getByRole('button',{name:'撤销授权',exact:true}).click();
 await until('B revoke confirmed',async()=>await b.locator('.remote-source-list li').count()===0);
 await host(a,'/api/v1/machines/inspect-machine',[originalConnection.id,'']);assert.equal((await host(b,'/api/v1/machines/inspect-machine',[reverse.id,''])).body.value.state,'ready','one-way revoke leaves reverse authorization intact');
 await a.getByRole('button',{name:'重新检查',exact:true}).click();await until('revoked visible',async()=>(await a.locator('.machine-row').filter({hasText:'B synthetic'}).innerText()).includes('授权已撤销'));
 await a.locator('.machine-row').filter({hasText:'B synthetic'}).click();await a.getByRole('button',{name:'发送连接请求',exact:true}).click();await a.locator('.runtime-dialog').waitFor({state:'hidden'});await approve(b);await check(a,'B synthetic');
 evidence.directionalAuthorization={separateRequests:true,separateProofs:true,nonblockingPending:true,lazyARead:true,revokeOnlyOneDirection:true};
 evidence.steps.push('Actual React A submits and returns immediately; no B grant before local B approval. A checks the original request manually. Reverse direction requires independent approval; revoke changes one direction only.');
 await shot(a,'A-paired');
 await a.getByRole('button',{name:'关闭设置',exact:true}).click();
 await a.locator('[data-settings-panel]').waitFor({state:'hidden'});
 await a.locator('[contenteditable="true"]').first().waitFor({state:'visible'});
 if(process.argv.includes('--url-contract')) {
  await urlContractJourney({a,b,A,B,context,send,nativeStates,until,shot,evidence,relay});
  assert.deepEqual(errors,[],'React page errors');
 } else {
 const csv=Buffer.from('name,value\nsource,7\n');
 const pdf=Buffer.from('%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n');
 const files=[{name:'quarterly-report.csv',mimeType:'text/csv',buffer:csv},{name:'fixture.pdf',mimeType:'application/pdf',buffer:pdf}];
 const expected=files.map(f=>({name:f.name,bytes:f.buffer.length,sha256:createHash('sha256').update(f.buffer).digest('hex')}));
 await a.getByRole('button',{name:'添加附件或引用'}).click();
 const [chooser]=await Promise.all([a.waitForEvent('filechooser'),a.getByRole('menuitem',{name:'添加文件',exact:true}).click()]);
 await chooser.setFiles(files);
 await a.locator('.attachments').getByText('quarterly-report.csv',{exact:true}).waitFor();
 if(process.env.WORKER_PEER_FIRST_ADMISSION_LOSS==='1')relay.drop('start');
 await send(a,{action:'delegate',environment:'B synthetic',title:'Synthetic original CSV',scenario:'csv',steps:['question','opaque'],wait:true,expected});
 await until('A original interaction visible',async()=>{
  const option=a.getByRole('button',{name:/^Fixture A/});if(await option.isVisible()){await option.click();return true;}
  const dock=a.locator('.approval-dock-trigger');if(await dock.isVisible()&&await dock.getAttribute('aria-expanded')==='false')await dock.click();
  return false;
 });
 await a.locator('.approval:visible .approval-choices .primary-decision').click();
 await until('native question confirmed',async()=>(await nativeStates(B.data)).some(v=>Object.entries(v.pending).some(([id,p])=>p.fixtureKind==='question'&&v.effectCounts['interaction:'+id]===1)));
 evidence.steps.push('B native original question delivered to A and answered through actual React controls');
 await shot(a,'A-question-answered');
 await until('A native opaque approval visible',async()=>{
  const dock=a.locator('.approval-dock-trigger');if(await dock.isVisible()&&await dock.getAttribute('aria-expanded')==='false')await dock.click();
  const choice=a.locator('.approval:visible').filter({hasText:'echo synthetic-only'}).locator('.approval-choices button').first();if(await choice.isVisible()){await choice.click();return true;}return false;
 });
 await a.getByText(/Synthetic resident verified task/).first().waitFor({timeout:45000});
 const ledger=JSON.parse(await readFile(join(A.data,'tasks.json'),'utf8'));
 const task=Object.values(ledger.records).find(v=>v.view.title==='Synthetic original CSV');assert.ok(task);const taskID=task.view.id;
 if(process.env.WORKER_PEER_FIRST_ADMISSION_LOSS==='1'){const originalOp=task.requests[0];await until('first original response lost',()=>relay.effects.some(v=>v.kind==='drop'&&v.task===taskID&&v.requestId===originalOp&&v.responseLost));assert.ok((await nativeStates(B.data)).some(v=>v.clientInputCounts[originalOp]===1));evidence.firstAdmissionLoss={task:taskID,operation:originalOp,responseLost:true,nativeInputs:1};}
 evidence.task=taskID;evidence.steps.push('Native approval reply confirmed; B executed once; A Agent used real bot_tasks read/file and native file read');
 await a.goto(A.url+'/#task/'+taskID);
 const artifact=a.locator('.web-task-pane .artifact-link').filter({hasText:'fixture-result.csv'}).first();await artifact.waitFor();await artifact.click();
 await a.getByRole('dialog',{name:'文件预览',exact:true}).waitFor();
 assert.match(await a.getByRole('dialog',{name:'文件预览',exact:true}).textContent(),/fixture,42/);
 await shot(a,'A-output-preview');
 await a.keyboard.press('Escape');await a.getByRole('dialog',{name:'文件预览',exact:true}).waitFor({state:'hidden'});
 await artifact.click({button:'right'});
 const download=a.waitForEvent('download');await a.getByRole('menuitem',{name:'另存为…',exact:true}).click();
 const downloaded=await download;assert.equal(downloaded.suggestedFilename(),'fixture-result.csv');
 assert.equal(await readFile(await downloaded.path(),'utf8'),'name,value\nfixture,42\n');
 evidence.steps.push('Actual A React output preview and authenticated browser download preserve original name and bytes');
 await a.locator('[contenteditable="true"]').first().waitFor({state:'visible'});
 const png=fixturePNG();
 const imageChooser=a.waitForEvent('filechooser');await a.getByRole('button',{name:'添加附件或引用'}).click();await a.getByRole('menuitem',{name:'添加文件',exact:true}).click();
 await (await imageChooser).setFiles([{name:'追加图片.png',mimeType:'image/png',buffer:png}]);
 await a.locator('.attachments').getByText('追加图片.png',{exact:true}).waitFor();
 await until('real image thumbnail decodes',()=>a.locator('.attachments img.draft-thumbnail').evaluate(img=>img.complete&&img.naturalWidth===1));
 await send(a,{action:'send',task:taskID,scenario:'csv',wait:true,expected:[{name:'追加图片.png',bytes:png.length,sha256:createHash('sha256').update(png).digest('hex')}]});
 await until('original task continued exactly once',async()=>{const states=await nativeStates(B.data);return states.some(v=>Object.entries(v.effectCounts).some(([k,n])=>k.startsWith('turn/start:')&&n===2));});
 await until('A native file read after continue',async()=>(await nativeStates(A.data)).some(v=>v.effectCounts['verified-file:'+taskID]===2),45000);
 const foreign=await b.evaluate(async()=>{const r=await fetch('/api/v1/bootstrap',{method:'POST',headers:{'X-Caelis-Request':'1'}});const boot=await r.json();return await (await fetch('/api/v1/tasks/list',{method:'POST',headers:{'Content-Type':'application/json','X-Caelis-Request':'1','X-CSRF-Token':boot.csrf},body:JSON.stringify({args:[]})})).json();});assert.equal(JSON.stringify(foreign).includes(taskID),false,'B product must not list foreign task');

 // Lose a response only after B actually produced it; never forge an API reply.
 relay.drop('start');
 await send(a,{action:'delegate',environment:'B synthetic',title:'Lost response original',scenario:'csv',wait:false});
 const lostLedger=await until('lost response original retained',async()=>{const d=JSON.parse(await readFile(join(A.data,'tasks.json'),'utf8'));return Object.values(d.records).find(v=>v.view.title==='Lost response original');});
 const lostID=lostLedger.view.id,lostOp=lostLedger.requests[0];
 await until('real create response lost',()=>relay.effects.some(v=>v.kind==='drop'&&v.task===lostID&&v.requestId===lostOp&&v.responseLost));
 await until('one original native create effect',async()=>(await nativeStates(B.data)).some(v=>v.clientInputCounts[lostOp]===1));
 await send(a,{action:'reconcile',task:lostID,operation:lostOp});
 await a.getByText('Original operation reconciled: accepted ('+lostOp+')',{exact:true}).waitFor();
 assert.equal(relay.effects.filter(v=>v.kind==='drop').length,process.env.WORKER_PEER_FIRST_ADMISSION_LOSS==='1'?2:1);
 assert.ok((await nativeStates(B.data)).some(v=>v.clientInputCounts[lostOp]===1));
 evidence.steps.push('Actual peer create response lost after B acceptance; A reconciled the original op as accepted; native input effect exactly once');

 // Pause B's upload after real bytes reached its HTTP listener, then click
 // A's visible Stop. The synthetic native interrupt cancels its real MCP child,
 // so the product bridge/HTTP context must stop, rather than the test caller.
 relay.holdStart();
 const slow=Buffer.from('name,value\n'+('synthetic,1\n'.repeat(180000)));
 const slowChooser=a.waitForEvent('filechooser');await a.getByRole('button',{name:'添加附件或引用'}).click();await a.getByRole('menuitem',{name:'添加文件',exact:true}).click();
 await (await slowChooser).setFiles([{name:'slow-transfer.csv',mimeType:'text/csv',buffer:slow}]);
 await a.locator('.attachments').getByText('slow-transfer.csv',{exact:true}).waitFor();
 await send(a,{action:'delegate',environment:'B synthetic',title:'Stopped during upload',scenario:'csv',wait:true});
 const held=await until('partial bytes at B upload',()=>relay.held());assert.ok(held.forwarded>0&&held.forwarded<held.received);assert.ok(held.requestId&&held.task);
 await a.getByRole('button',{name:'停止工作',exact:true}).waitFor();await shot(a,'A-upload-stop');
 await a.getByRole('button',{name:'停止工作',exact:true}).click();
 await until('UI native interrupt cancels original HTTP upload',()=>held.cancelled);
 const stopped=await until('original uncertain write persisted',async()=>{const d=JSON.parse(await readFile(join(A.data,'tasks.json'),'utf8'));const v=d.records[held.task];return v?.view.status==='unknown'&&v;});
 assert.deepEqual(stopped.requests,[held.requestId]);
 const nativeInterrupts=(await nativeStates(A.data)).reduce((n,v)=>n+Object.entries(v.effectCounts).filter(([k])=>k.startsWith('turn/interrupt:')).reduce((sum,[,count])=>sum+count,0),0);assert.equal(nativeInterrupts,1);evidence.uiStopNativeEffects=nativeInterrupts;
 assert.equal((await nativeStates(B.data)).reduce((n,v)=>n+(v.clientInputCounts[held.requestId]||0),0),0,'partial upload must not create native work');
 await send(a,{action:'reconcile',task:held.task,operation:held.requestId});
 await a.getByText('Original operation unavailable; result remains unknown ('+held.requestId+')',{exact:true}).waitFor();
 assert.equal(relay.effects.filter(v=>v.kind==='hold').length,1,'unknown original upload replayed');
 assert.equal((await nativeStates(B.data)).reduce((n,v)=>n+(v.clientInputCounts[held.requestId]||0),0),0);
 evidence.steps.push('Upload-period real UI Stop interrupted A native turn, cancelled real MCP/peer upload, retained unknown original op; original reconcile unknown, no replay/no native Worker effect');
 evidence.transportFaults=relay.effects;
 await stop(B.child);evidence.steps.push('Same original task continued on B; B App then stopped for offline-copy acceptance');
 await a.goto(A.url+'/#task/'+taskID);await artifact.waitFor();await artifact.click();
 await a.getByRole('dialog',{name:'文件预览',exact:true}).waitFor();
 assert.match(await a.getByRole('dialog',{name:'文件预览',exact:true}).textContent(),/fixture,42/);
 await shot(a,'A-offline-preview');
 evidence.steps.push('B offline: previously retrieved checked output remains previewable through actual A Web UI');
 await a.keyboard.press('Escape');
 await a.getByRole('dialog',{name:'文件预览',exact:true}).waitFor({state:'hidden'});
 await until('A resident remains healthy with disconnected/unknown remote work',async()=>{const r=await host(a,'/api/v1/conversation/snapshot');return r.status===200&&r.body.value.connection==='ready'&&r.body.value.canSend;});
 await send(a,{action:'delegate',environment:'local',title:'Local remains available while B offline',scenario:'csv',wait:true});
 const independent=await until('real local Worker completes despite B offline and old unknown receipts',async()=>{const d=JSON.parse(await readFile(join(A.data,'tasks.json'),'utf8'));return Object.values(d.records).find(v=>v.view.title==='Local remains available while B offline'&&v.view.status==='completed');},45000);
 assert.equal(independent.view.machine||'','');
 await until('A Agent reads local result through normal product tools',async()=>(await nativeStates(A.data)).some(v=>v.effectCounts['verified-file:'+independent.view.id]===1),45000);
 evidence.localIsolation={task:independent.view.id,resident:'ready',remoteOffline:true,oldUnknownPreserved:JSON.parse(await readFile(join(A.data,'tasks.json'),'utf8')).records[held.task].view.status==='unknown'};
 assert.equal(evidence.localIsolation.oldUnknownPreserved,true);
 evidence.steps.push('B disconnected and original remote upload unknown: A React/resident remains ready; real local delegation completes and A Agent verifies its original file, without fallback or clearing remote uncertainty');
 const native=await nativeStates(B.data);
 evidence.nativeCounts=native.map(v=>({build:v.build,effects:v.effectCounts,inputs:v.clientInputCounts}));
 assert.ok(native.some(v=>Object.keys(v.effectCounts).some(k=>k.includes('verified-input:')&&k.endsWith('quarterly-report.csv'))));
 const interactions=native.flatMap(v=>Object.entries(v.effectCounts).filter(([k])=>k.startsWith('interaction:')));
 assert.equal(interactions.length,2);assert.ok(interactions.every(([,n])=>n===1));
 evidence.gates.uploadUIStop='real UI/native/MCP/HTTP cancellation and original unknown reconciliation passed';
 assert.deepEqual(errors,[],'React page errors');
 evidence.result='vertical journey passed';
 }
} catch(error){evidence.result='failed';evidence.error=error.stack;if(debugB)await writeFile(join(artifacts,'B-visible-errors.json'),JSON.stringify(await debugB.locator('[role=alert],.inline-error').allTextContents()));if(debugPage){const snapshot=await debugPage.evaluate(async()=>{const r=await fetch('/api/v1/bootstrap',{method:'POST',headers:{'X-Caelis-Request':'1'}});const b=await r.json();return await (await fetch('/api/v1/conversation/snapshot',{method:'POST',headers:{'Content-Type':'application/json','X-Caelis-Request':'1','X-CSRF-Token':b.csrf},body:JSON.stringify({args:[]})})).json();}).catch(e=>({error:e.message}));await writeFile(join(artifacts,'failure-snapshot.json'),JSON.stringify(snapshot,null,2));await shot(debugPage,'failure').catch(()=>{});await writeFile(join(artifacts,'failure-dom.txt'),await debugPage.locator('body').innerText()).catch(()=>{});}throw error;}
finally{
 if(relay)evidence.transportFaults=relay.effects;
 if(browser)await browser.close();
 if(relay)await relay.close();
 for(const c of children.reverse())await stop(c);
 for(const app of apps)await writeFile(join(artifacts,app.name+'-app.log'),app.log());
 // A production App update/quit may detach a native Worker intentionally.
 // This test owns those synthetic children and must terminate them explicitly.
 const states=await nativeStates(root);
 evidence.nativeCounts=states.map(v=>({build:v.build,effects:v.effectCounts,inputs:v.clientInputCounts}));
 if(evidence.result==='failed')for(const app of apps){try{const ledger=JSON.parse(await readFile(join(app.data,'tasks.json'),'utf8'));const safe=Object.fromEntries(Object.entries(ledger.records||{}).map(([id,r])=>[id,{view:r.view,requests:r.requests,confirmedRequests:r.confirmedRequests,requestOutcomes:r.requestOutcomes,operations:r.operations}]));await writeFile(join(artifacts,app.name+'-failure-tasks.json'),JSON.stringify(safe,null,2));}catch{}}
 for(const state of states){
  const pid=state.fixturePid;if(!Number.isInteger(pid)||pid<=1)continue;
  let command,cwd;
  try {command=execFileSync('ps',['-p',String(pid),'-o','command='],{encoding:'utf8'}).trim();
   cwd=process.platform==='linux'?await realpath('/proc/'+pid+'/cwd'):execFileSync('lsof',['-a','-p',String(pid),'-d','cwd','-Fn'],{encoding:'utf8'}).split('\n').find(v=>v.startsWith('n'))?.slice(1);
  } catch {continue}
  assert.ok(command.startsWith(join(repo,'.cache/worker-peer-e2e/bin/native')+' '),'refuse unrelated PID');
  assert.ok(cwd?.startsWith(root+'/'),'refuse unrelated cwd');
  process.kill(pid,'SIGTERM');
  await until('owned native fixture exit',()=>{try{process.kill(pid,0);return false}catch{return true}},5000);
  const socket=command.match(/--listen unix:\/\/(\/[^ ]+\/runtime\.sock)$/)?.[1];
  if(socket&&/^caelis-bot-[0-9]+$/.test(dirname(socket).split('/').at(-1)))await rm(dirname(socket),{recursive:true,force:true});
 }
 evidence.cleanup='owned App, browser and detached synthetic native children exited; no daily service touched';
 await writeFile(join(artifacts,'evidence.json'),JSON.stringify(evidence,null,2));
 if(process.env.WORKER_PEER_KEEP_PROFILE!=='1')await rm(root,{recursive:true,force:true});
 console.log('Worker peer evidence:',artifacts);
}
