// Disposable UI fixture: serves the built React surfaces with synthetic facts.
// No application data, native backend connection or model requests.
import {createServer} from 'node:http';
import {readFileSync,writeFileSync} from 'node:fs';
import {resolve,extname,sep} from 'node:path';

const runtime=String.raw`
let revision=1,streamTimer=0,recordFrame=0,petState='idle';
const previewParams=new URLSearchParams(location.search);
let draftFiles=['attachments','quote-worker'].includes(previewParams.get('fixture'))?[{id:'fixture-image',name:'Pasted image.png',size:184320,type:'image/png',image:true,unavailable:false},{id:'fixture-file',name:'project-notes.pdf',size:2457600,type:'application/pdf',image:false,unavailable:false}]:[];
window.fixtureFrames=[];
// Fixture-only frame accounting verifies that completed portraits release RAFs.
const requestFrame=window.requestAnimationFrame.bind(window),cancelFrame=window.cancelAnimationFrame.bind(window);
const pendingFrames=new Set();
window.requestAnimationFrame=callback=>{const id=requestFrame(time=>{pendingFrames.delete(id);callback(time);});pendingFrames.add(id);return id;};
window.cancelAnimationFrame=id=>{pendingFrames.delete(id);cancelFrame(id);};
const short='钉好了 — 任务卡片现在是 **固定** 状态，\`pinned: true\`，会一直留在你脚底下。';
const long=short+'\n\n### 工作进展\n\n- 已读取 **README.md**\n- 已完成网络搜索\n- 正在更新说明文件\n\n> 悬浮可以阅读完整内容，移开后收起。\n\n| 操作 | 状态 |\n| --- | --- |\n| 阅读 | 完成 |\n| 编辑 | 完成 |\n\n\`\`\`go\nfmt.Println("Caelis Bot")\n\`\`\`\n\n'+Array.from({length:16},(_,i)=>(i+1)+'. 这是用于验证完整内容和屏幕高度限制的长段落。**格式保持可读**，滚动可继续阅读。').join('\n\n')+'\n\n**全文结束 END**\n\n[示例链接](https://example.com)';
const snapshot={connection:'ready',phase:'working',currentTurn:'fixture',canInterrupt:true,canSend:false,canSteer:true,quiet:false,items:[],approvals:[],reviews:[],references:[],message:'',previewKey:'fixture',previewDismissed:false,botStatus:'',hasEarlier:false,lastReceipt:{id:'',outcome:'',message:''}};
if(previewParams.get('fixture')==='attachment-menu')snapshot.references=[
 {id:'fixture-core',kind:'skill',name:'bot-core',description:'Resident guidance for the Bot.'},
 {id:'fixture-acpx',kind:'skill',name:'acpx',description:'Work with agent-to-agent communication through ACP.'},
 {id:'fixture-acpx-duplicate',kind:'skill',name:'acpx',description:'Work with agent-to-agent communication through ACP.'},
 {id:'fixture-doc',kind:'skill',name:'doc',description:'Create and edit documents with formatting checks.'},
 {id:'fixture-pdf',kind:'skill',name:'pdf',description:'Read and prepare PDF documents.'},
 {id:'fixture-plugin',kind:'plugin',name:'Desktop World',description:'Use desktop windows and controls when a task needs visual context.'},
 {id:'fixture-plugin-2',kind:'plugin',name:'Google Drive',description:'Find and work with files in your connected Drive.'}
];
window.fixtureSet=kind=>{
 clearInterval(streamTimer);
 Object.assign(snapshot,{quiet:false,maintenance:'',phase:'working',canInterrupt:true,activity:{kind:'read',target:'README.md'},approvals:[],reviews:[],previewDismissed:false});
 snapshot.items=[{id:'answer',turnKey:'fixture',kind:'assistant',text:kind==='short'?short:long,status:'completed',artifacts:[]}];
 if(['read','edit','web','execute'].includes(kind))snapshot.activity={kind,target:kind==='read'?'README.md':kind==='edit'?'notes.md':''};
 if(kind==='review')snapshot.reviews=[{id:'review',status:'inProgress',action:'',rationale:''}];
 if(kind==='approval')snapshot.approvals=[{id:'approval',title:'允许读取所选文件？',status:'pending',action:'cat README.md',details:'',target:'README.md',questions:[],choices:[{id:'allow',label:'允许一次',scope:'once'},{id:'deny',label:'拒绝',scope:'deny'}]}];
 if(kind==='completed')Object.assign(snapshot,{phase:'completed',canInterrupt:false,activity:null});
 if(kind==='stop')Object.assign(snapshot,{phase:'interrupting',activity:null});
 if(kind==='table'){
  Object.assign(snapshot,{phase:'completed',canInterrupt:false,activity:null});
  snapshot.items[0].text='两个 issue 都已开好并确认存在。\n\n### 已挂到 GitHub\n\n| # | 标题 | 链接 |\n| --- | --- | --- |\n| 36 | runtimeenv: HOME/ZDOTDIR 被私有化，环境探测读不到用户 dotfiles，工具链 PATH 丢失 | [https://github.com/caelis-labs/caelis-bot/issues/36](https://github.com/caelis-labs/caelis-bot/issues/36) |\n| 35 | care: 多源订阅 + 按「实际打扰」计量节流额度 | [https://github.com/caelis-labs/caelis-bot/issues/35](https://github.com/caelis-labs/caelis-bot/issues/35) |';
 }
 if(kind==='stream'){
  Object.assign(snapshot,{activity:null});
  const text='现在可以平滑地阅读流式回复了。中文、英文和 👩🏽‍💻 都会完整出现。'+long;
  const item=snapshot.items[0];item.id='stream-'+revision;item.text='';item.status='inProgress';
  let offset=0;window.fixtureFrames=[];
  const record=()=>{window.fixtureFrames.push(document.querySelector('.bubble-copy')?.textContent??'');if(offset<text.length)requestAnimationFrame(record);};requestAnimationFrame(record);
  streamTimer=setInterval(()=>{offset=Math.min(text.length,offset+36);item.text=text.slice(0,offset);if(offset===text.length){clearInterval(streamTimer);item.status='completed';snapshot.phase='completed';snapshot.canInterrupt=false;}},450);
 }

};
window.fixtureSet('short');
window.fixtureReviewApproval=()=>{fixtureSet('approval');snapshot.items[0].text='需要读取 README.md，继续前请确认访问权限。';revision++;};
if(previewParams.get('fixture')==='approval'){
 window.fixtureSet('approval');
 snapshot.approvals[0]={id:'synthetic-computer-use',title:'Allow Computer Use',action:'Allow Computer Use',status:'pending',description:'Synthetic window only',details:'',target:'',questions:[],choices:[
  {id:'once',label:'Allow once',scope:'allow_once'},
  {id:'session',label:'Allow this session',scope:'allow_always'},
  {id:'always',label:'Always allow',scope:'allow_always'},
  {id:'deny',label:'Deny',scope:'reject_once'}
 ]};
 snapshot.phase='waiting_approval';
}
if(draftFiles.length){Object.assign(snapshot,{phase:'idle',canSend:true,canSteer:false,canInterrupt:false,activity:null});}
// Runs against the mounted production Bubble, including its 450ms polling and
// real animation frames. Also callable from the native preview's regression button.
window.fixtureBubbleReplay=async()=>{
 const samples=[];
 const body=()=>document.querySelector('.bubble-copy .markdown-body');
 const visible=()=>body()?.getClientRects().length>0;
 const wait=async(label,condition)=>{
  const deadline=performance.now()+12000;
  while(!condition()){
   if(performance.now()>deadline)throw Error('Timed out: '+label);
   await new Promise(requestAnimationFrame);
  }
 };
 try{
  await wait('initial snapshot',()=>visible());
  window.fixtureSet('completed');
  const text='已经完成的说明文字不应在审批后重新打字。'.repeat(8);
  const item=snapshot.items[0];item.id='overlay-'+(++revision);item.text=text;
  await wait('reply fully revealed',()=>visible()&&body().textContent===text);
  // Current Bubble overlays are approvals and native notices. Review records
  // and raw transport messages no longer replace assistant content.
  for(const overlay of ['approval','notice']){
   if(overlay==='approval')snapshot.approvals=[{id:'overlay-approval',title:'允许读取所选文件？',status:'pending',action:'cat README.md',target:'README.md',details:'',questions:[],choices:[]}];
   if(overlay==='notice')window.dispatchEvent(new CustomEvent('terminal-notice',{detail:{message:'合成通知',pending:true}}));
   await wait(overlay+' shown',()=>!visible());
   snapshot.approvals=[];snapshot.reviews=[];snapshot.message='';
   if(overlay==='notice')window.dispatchEvent(new CustomEvent('terminal-notice',{detail:{message:'',pending:false}}));
   await wait(overlay+' dismissed',visible);
   samples.push({overlay,text:body().textContent});
   if(body().textContent!==text)throw Error(overlay+' replayed a fully revealed reply');
  }
  // A genuine in-progress tail survives an overlay. Its completion must drain
  // all received text immediately, rather than keep pacing a terminal message.
  item.status='inProgress';snapshot.phase='working';snapshot.currentTurn=item.turnKey;
  item.text=text+'生成中的末尾保持增量显示。'.repeat(30);
  await wait('queued tail',()=>visible()&&body().textContent.length>text.length&&body().textContent!==item.text);
  const before=body().textContent;
  window.dispatchEvent(new CustomEvent('terminal-notice',{detail:{message:'合成通知',pending:true}}));
  await wait('tail overlay',()=>!visible());
  window.dispatchEvent(new CustomEvent('terminal-notice',{detail:{message:'',pending:false}}));
  await wait('tail restored',visible);
  if(!body().textContent.startsWith(before)||body().textContent===item.text)throw Error('overlay restarted or flushed queued tail');
  item.status='completed';snapshot.phase='completed';snapshot.currentTurn='';
  await wait('terminal tail complete',()=>body().textContent===item.text);
  return window.fixtureReplayResult={ok:true,overlays:samples.map(s=>s.overlay),tailLength:item.text.length};
 }catch(error){return window.fixtureReplayResult={ok:false,error:String(error),samples};}
};
window.fixtureDream=kind=>{
 clearInterval(streamTimer);cancelAnimationFrame(recordFrame);
 window.fixtureSet(kind==='approval'?'approval':'completed');
 snapshot.items=[{id:'ordinary',turnKey:'ordinary',kind:'assistant',text:'已经整理好了今天的工作。你随时可以继续说。',status:'completed',artifacts:[]}];
 Object.assign(snapshot,{currentTurn:'dream',maintenance:kind==='running'?'dreaming':'',quiet:kind!=='approval',phase:kind==='done'?'completed':kind==='approval'?'waiting_approval':'working',canInterrupt:kind!=='done',canSend:kind==='done',canSteer:kind==='running',activity:null});
 petState=kind==='running'?'dreaming':kind==='approval'?'waiting':'idle';
 window.dispatchEvent(new CustomEvent('pet-activity',{detail:petState}));
};
if(new URLSearchParams(location.search).has('dream'))window.fixtureDream('running');

// Exercise the actual History component, polling bridge and Markdown renderer.
// These synthetic replies never reach an agent or the user's conversation.
window.fixtureChat=kind=>{
 clearInterval(streamTimer);cancelAnimationFrame(recordFrame);
 const id='chat-'+(++revision);
 const paragraph='这是一段用于检查聊天窗口打字效果的合成回复。文字应该连续出现，即使模型一次返回了较长的段落，也不应该整块跳出。';
 const text=paragraph.repeat(5)+'\n\n**完整结束** 👩🏽‍💻 é 🇨🇳\n\n| 项目 | 状态 |\n| --- | --- |\n| 中文 | 完成 |\n| Emoji | 完成 |\n\n\`\`\`js\nconst complete = true;\n\`\`\`';
 const item={id,turnKey:id,kind:'assistant',text:'',status:'inProgress',artifacts:[]};
 Object.assign(snapshot,{quiet:false,maintenance:'',items:[...snapshot.items,item],currentTurn:id,activity:null,phase:'working',canInterrupt:true,canSend:false,approvals:[],reviews:[]});
 window.fixtureFrames=[];window.fixtureExpected=text;window.fixtureTarget=id;
 const started=performance.now();
 const record=now=>{
  const element=document.querySelector('[data-message-id="'+id+'"] .markdown-body');
  const scroll=document.querySelector('.chat-scroll');
  window.fixtureFrames.push({time:now-started,text:element?.textContent??'',phase:snapshot.phase,stop:!!document.querySelector('[aria-label="停止工作"]'),bottom:scroll?scroll.scrollHeight-scroll.clientHeight-scroll.scrollTop:0});
  if(now-started<15000)recordFrame=requestAnimationFrame(record);
 };recordFrame=requestAnimationFrame(record);
 let offset=0;
 const push=()=>{
  offset=kind==='instant'||kind==='final'&&offset>0?text.length:Math.min(text.length,offset+(kind==='burst'?160:kind==='final'?12:36));
  item.text=text.slice(0,offset);
  if(offset===text.length){clearInterval(streamTimer);Object.assign(snapshot,{phase:'completed',currentTurn:'',canInterrupt:false,canSend:true});item.status='completed';}
 };
 if(kind==='instant')push();else streamTimer=setInterval(push,450);
};
window.fixtureReopen=()=>{window.dispatchEvent(new Event('history-close'));setTimeout(()=>window.dispatchEvent(new Event('history-open')),100);};
window.fixtureMedia=()=>{
 clearInterval(streamTimer);cancelAnimationFrame(recordFrame);
 Object.assign(snapshot,{items:[{id:'media-fixture',requestId:'media-fixture-request',turnKey:'fixture',kind:'user',text:'请看这两张图片。\nfirst.png\nsecond.png',status:'completed',artifacts:[],media:{caption:'请看这两张图片。',images:[{id:'media-fixture-one',name:'first.png',width:180,height:180},{id:'media-fixture-two',name:'second.png',width:180,height:180}]}}],phase:'completed',currentTurn:'',canInterrupt:false,canSend:true,activity:null});
};
window.fixtureAvatar=kind=>{
 clearInterval(streamTimer);cancelAnimationFrame(recordFrame);
 if(kind==='stream'){
  window.fixtureChat('stream');
  snapshot.activity={kind:'read',target:'README.md'};
  return;
 }
 if(kind==='done'){
  for(const item of snapshot.items)if(item.kind==='assistant')item.status='completed';
  Object.assign(snapshot,{phase:'completed',currentTurn:'',canInterrupt:false,canSend:true,activity:null});return;
 }
 window.fixtureSet(kind==='approval'?'approval':'read');
 Object.assign(snapshot,{currentTurn:'fixture',message:'',canSend:false,quiet:false,maintenance:''});
 snapshot.items=[{id:'avatar-history',kind:'assistant',turnKey:'old',status:'completed',text:'头像会跟着我正在做的事情变化。历史消息里的头像保持安静。',artifacts:[]}];
 snapshot.activity=kind==='thinking'?null:{kind:kind==='search'?'web':'read',target:kind==='search'?'':'README.md'};
};
window.fixtureAvatarRegression=async()=>{
 const wait=async(condition)=>{const until=performance.now()+8000;while(!condition()){if(performance.now()>until)throw Error('avatar fixture timeout');await new Promise(r=>setTimeout(r,50));}};
 const samples=[];
 try{
  for(const [kind,clip]of [['thinking','think'],['search','scan'],['read','focus']]){
   window.fixtureAvatar(kind);
   await wait(()=>document.querySelector('.working-message [data-portrait="'+clip+'"] canvas')?.dataset.frame>='1');
   const row=document.querySelector('.working-message'),canvas=row.querySelector('canvas');
   const first=canvas.toDataURL();await new Promise(r=>setTimeout(r,700));
   if(first===canvas.toDataURL())throw Error(kind+' portrait did not move');
   if(!row.querySelector('.working-label')?.textContent)throw Error(kind+' has no visible label');
   if(document.querySelector('.history-messages canvas'))throw Error('history animated');
   samples.push({kind,clip,label:row.textContent});
  }
  window.fixtureAvatar('stream');
  await wait(()=>document.querySelector('[data-portrait="listen"] canvas')?.dataset.frame>='1'&&!document.querySelector('.working-message'));
  samples.push({kind:'stream',dots:false});
  window.fixtureAvatar('done');
  await wait(()=>document.querySelector('[data-portrait="delight"] canvas')?.dataset.frame>='1');
  samples.push({kind:'done',clip:'delight'});
  await new Promise(r=>setTimeout(r,6500));
  if(document.querySelector('.history-messages canvas'))throw Error('completion still animates after expiry');
  await wait(()=>pendingFrames.size===0);
  samples.push({kind:'expired',clip:'still',pendingFrames:pendingFrames.size});
  // A later tool-only turn must not reanimate the preceding reply.
  Object.assign(snapshot,{phase:'working',currentTurn:'tool-only',activity:{kind:'read'},canInterrupt:true});
  await wait(()=>!!document.querySelector('.working-message'));
  Object.assign(snapshot,{phase:'completed',currentTurn:'',activity:null,canInterrupt:false});
  await wait(()=>!document.querySelector('.working-message'));
  if(document.querySelector('.history-messages canvas'))throw Error('tool-only completion replayed an old portrait');
  samples.push({kind:'tool-only',clip:'still'});
  window.fixtureAvatar('approval');
  await wait(()=>document.querySelector('.state-message [data-portrait="waiting"] canvas')?.dataset.frame>='1');
  if(document.querySelector('.working-message'))throw Error('approval duplicated waiting row');
  samples.push({kind:'approval',clip:'waiting'});
  return window.fixtureAvatarResult={ok:true,samples};
 }catch(error){return window.fixtureAvatarResult={ok:false,error:String(error),samples};}
};
// Exercise actual typing, submission, delayed receipts and the draft/outbox
// handoff. All bridge facts and input are disposable synthetic data.
let draft={text:'',referenceIds:[],notice:'',revision:1},sendMode='',sendCount=0,draftReads=0,acceptedReads=0,staleHeld=false;
if(previewParams.get('fixture')==='quote-worker'){
 const quote={localId:'fixture-original',role:'assistant',text:'Runtime 允许了沙箱外执行，date 已完成：Sun Oct 11 08:50:12 CST 2026'};
 const now=Date.now()*1000;
 draft={...draft,quoted:quote};
 snapshot.items=[
  {id:'fixture-original',turnKey:'fixture',kind:'assistant',text:quote.text,status:'completed',artifacts:[],seenAt:now-120000000},
  {id:'fixture-user',requestId:'fixture-user',turnKey:'fixture',kind:'user',text:'我引用的信息是什么？',quoted:quote,status:'completed',artifacts:[],seenAt:now-90000000},
  {id:'fixture-task',turnKey:'fixture',kind:'assistant',text:'Worker 已开始：处理 Bot Issue 152',task:{title:'处理 Bot Issue 152',status:'working'},status:'completed',artifacts:[],seenAt:now-60000000}
 ];
 Object.assign(snapshot,{phase:'idle',activity:null,canSend:true,canSteer:false,canInterrupt:false});
 if(previewParams.has('openQuote')){
  const open=setInterval(()=>{const button=document.querySelector('.sent-quote');if(button){clearInterval(open);button.click();}},50);
 }
}
if(['control-ux','control-ux-question'].includes(previewParams.get('fixture'))){
 const now=Date.now()*1000;
 snapshot.items=[
  {id:'fixture-request',turnKey:'fixture',kind:'user',text:'请检查 Calculator，并问我选哪项。',status:'completed',artifacts:[],seenAt:now-180000000},
  {id:'fixture-explanation',turnKey:'fixture',kind:'assistant',text:'我会核对当前可用选项。',status:'completed',artifacts:[],seenAt:now-120000000},
  {id:'fixture-question',turnKey:'fixture',kind:'assistant',text:'[Q5] 测试题：请选择一个字。',questionCards:[{shortId:'Q5',title:'测试题：请选择一个字。',options:['甲','乙','丙'],state:''},{shortId:'Q6',title:'测试题：输入一句简短说明。',options:[],state:''}],status:'completed',artifacts:[],seenAt:now-60000000}
 ];
 snapshot.approvals=[
  {id:'fixture-approval-one',turnKey:'fixture',title:'Allow Computer Use to use "Calculator"?',action:'cua_repl',taskTitle:'检查 Calculator',status:'pending',target:'Calculator',questions:[],choices:[{id:'allow-once',label:'允许这一次',scope:'once'},{id:'allow-session',label:'允许本次会话',scope:'session'},{id:'allow-always',label:'总是允许',scope:'always'},{id:'deny',label:'拒绝',scope:'deny'}]},
  {id:'fixture-approval-two',turnKey:'fixture',title:'另一个待处理请求',action:'read file',status:'pending',target:'notes.txt',questions:[],choices:[{id:'allow',label:'允许',scope:'once'},{id:'deny',label:'拒绝',scope:'deny'}]}
 ];
 Object.assign(snapshot,{phase:'waiting_approval',activity:{kind:'read',target:'Calculator'},canSend:false,canSteer:false,canInterrupt:true});
 if(previewParams.get('fixture')==='control-ux'){
  const open=setInterval(()=>{const button=document.querySelector('.approval-dock-trigger');if(button){clearInterval(open);button.click();}},50);
 }
}

window.fixtureQuestionSetup=()=>{
 revision++;
 const now=Date.now()*1000;
 Object.assign(snapshot,{phase:'idle',activity:null,canSend:true,canSteer:false,canInterrupt:false,approvals:[],items:[
  {id:'question-request',kind:'user',text:'准备新版 Bot 的演示视频。',status:'completed',seenAt:now-60000000,artifacts:[]},
  {id:'question-explanation',kind:'assistant',text:'素材已准备好，还需要确认演示时的连接方式。',status:'completed',seenAt:now-30000000,artifacts:[]},
  {id:'question-item',kind:'assistant',text:'选择演示连接方式',status:'completed',seenAt:now,artifacts:[],questionCards:[{shortId:'Q7',binding:'original-fixture-question-'+revision,title:'演示视频使用哪个模型连接？',context:'仅用于隔离演示，无需密码或密钥；已有连接请提供位置，暂不配置则先录免登录片段。',taskTitle:'新版 Bot 演示视频',expiresAt:Math.floor(Date.now()/1000)+86400,options:['手动登录','使用已有连接','暂不配置'],state:''}]}
 ]});
};
window.fixtureReviewMulti=()=>{
 fixtureQuestionSetup();
 snapshot.items.find(i=>i.id==='question-item').questionCards.push({shortId:'Q8',binding:'review-second-'+revision,title:'演示视频发布到哪里？',context:'只使用脱敏后的演示材料；公开发布需要你确认。',taskTitle:'新版 Bot 演示视频',expiresAt:Math.floor(Date.now()/1000)+86400,options:['仅保存在本机','公开到项目网站','先发给我审阅'],state:''});
};
window.fixtureReviewLong=()=>{
 fixtureQuestionSetup();
 Object.assign(snapshot.items.find(i=>i.id==='question-item').questionCards[0],{title:'新版 Bot 视频中的真实对话需要可用的演示模型连接。你希望如何提供？会先继续准备素材和录制无需登录的流程，无需在这里发送密码或 API Key。',context:'',options:['我在隔离演示 Bot 中手动登录或配置模型','使用已有独立演示 Runtime，我提供位置','暂不配置，先完成无需登录的片段']});
};
window.fixtureReviewCombined=()=>{fixtureReviewMulti();const second=snapshot.items.find(i=>i.id==='question-item').questionCards[1];fixtureReviewLong();snapshot.items.find(i=>i.id==='question-item').questionCards.push(second);};
window.fixtureReviewFree=()=>{fixtureQuestionSetup();Object.assign(snapshot.items.find(i=>i.id==='question-item').questionCards[0],{title:'独立演示连接位于哪里？',context:'填写本机位置即可，无需密码或密钥。',options:[]});};
if(previewParams.get('fixture')==='question-lifecycle'){
 const mode=previewParams.get('questionCase');
 if(mode==='combined')fixtureReviewCombined();else if(mode==='long')fixtureReviewLong();else if(mode==='multiple')fixtureReviewMulti();else if(mode==='free')fixtureReviewFree();else fixtureQuestionSetup();
}

window.fixtureQuestionLayout=()=>{
 const card=document.querySelector('.question-prompt'),composer=document.querySelector('.compose-area');
 const rect=e=>{const r=e?.getBoundingClientRect();return r?{x:r.x,y:r.y,width:r.width,height:r.height,bottom:r.bottom}:null;};
 return {viewport:{width:innerWidth,height:innerHeight},cards:Array.from(document.querySelectorAll('.question-prompt')).map(e=>({title:e.querySelector('h3')?.textContent,rect:rect(e),controls:Array.from(e.querySelectorAll('button,input')).map(rect)})),card:rect(card),composer:rect(composer),decisions:Array.from(card?.querySelectorAll('button,input')??[]).map(e=>({text:e.textContent||e.getAttribute('placeholder'),...rect(e)})),accordions:card?.querySelectorAll('details,summary').length??0,questionTray:!!document.querySelector('.question-dock-panel'),internalScroll:!!card&&[card,...card.querySelectorAll('*')].some(e=>/auto|scroll/.test(getComputedStyle(e).overflowY)&&e.scrollHeight>e.clientHeight+1)};
};
window.fixtureQuestionRegression=async()=>{
 const wait=ms=>new Promise(resolve=>setTimeout(resolve,ms)),checks=[];
 const check=(name,ok)=>{checks.push({name,ok:!!ok});if(!ok)throw Error(name);};
 try{
  fixtureQuestionSetup();await wait(600);
  const layout=fixtureQuestionLayout();check('no disclosure or question tray',layout.accordions===0&&!layout.questionTray&&!document.querySelector('.approval-dock-trigger'));
  check('all decisions visible above composer',layout.card.y>=0&&layout.card.bottom<layout.composer.y&&layout.decisions.every(d=>d.y>=0&&d.bottom<=layout.composer.y));
  check('no internal scrolling',!layout.internalScroll);
  const options=Array.from(document.querySelectorAll('.question-prompt-options button'));
  check('short complete object question',document.querySelector('.question-prompt-title').textContent==='演示视频使用哪个模型连接？');
  options[0].focus();options[0].dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true}));check('arrow navigation',document.activeElement===options[1]);
  options[1].click();await wait(400);check('selected original index',window.fixtureQuestionResult?.[0]==='Q7'&&window.fixtureQuestionResult?.[2]==='2');
  check('answered leaves no card, status or empty message row',!document.querySelector('.question-prompt,.question-result,[data-message-id="question-item"]'));
  check('answer state stays in authoritative snapshot',snapshot.items.find(i=>i.id==='question-item').questionCards[0].state==='answered');
  window.dispatchEvent(new Event('history-close'));window.dispatchEvent(new Event('history-open'));await wait(400);
  check('reopen does not restore answered card',!document.querySelector('[data-message-id="question-item"]'));
  fixtureReviewMulti();await wait(600);document.querySelector('.question-prompt-options button').click();await wait(400);
  check('answering Q1 keeps Q2 only',document.querySelectorAll('.question-prompt').length===1&&document.querySelector('.question-prompt-title')?.textContent==='演示视频发布到哪里？'&&!document.querySelector('.question-result'));
  check('ordinary messages stay once each',document.querySelectorAll('[data-message-id="question-request"]').length===1&&document.querySelectorAll('[data-message-id="question-explanation"]').length===1);
  fixtureQuestionSetup();await wait(600);document.querySelector('.question-dismiss').click();await wait(400);
  check('dismissed has no decision controls',!document.querySelector('.question-prompt')&&snapshot.items.find(i=>i.id==='question-item').questionCards[0].state==='dismissed');
  window.dispatchEvent(new Event('history-close'));window.dispatchEvent(new Event('history-open'));await wait(400);
  check('reopen stays closed',!document.querySelector('.question-prompt'));
  for(const setup of [fixtureReviewLong,fixtureReviewMulti,fixtureReviewCombined]) {
   setup();await wait(600);const view=fixtureQuestionLayout();
   check(setup===fixtureReviewLong?'legacy full text decisions visible':'all questions and close actions visible',view.cards.every(c=>c.rect.y>=0&&c.rect.bottom<view.composer.y&&c.controls.every(r=>r.y>=0&&r.bottom<view.composer.y)));
   check('no internal scroll in long/multiple scenes',!view.internalScroll);
  }
 }catch(error){checks.push({error:String(error)});}finally{window.fixtureQuestionChecks=checks;fixtureQuestionSetup();}
};
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
window.fixtureSendSetup=mode=>{
 clearInterval(streamTimer);cancelAnimationFrame(recordFrame);sendMode=mode;sendCount=0;draftReads=0;acceptedReads=0;staleHeld=false;
 draftFiles=mode.startsWith('attachments-')?[{id:'send-fixture-file',name:'review-notes.pdf',size:1024,type:'application/pdf',image:false,unavailable:false}]:[];
 draft={text:'',referenceIds:[],notice:'',revision:draft.revision+1};
 const history=mode.startsWith('history-')?[{id:'send-history:'+mode,requestId:'',turnKey:'earlier',kind:'assistant',text:'这是一条已有的历史消息。',status:'completed',artifacts:[],seenAt:Date.now()*1000-(mode==='history-gap-accepted'?600e6:mode==='history-boundary-accepted'?295e6:60e6)}]:[];
 Object.assign(snapshot,{items:history,phase:'idle',currentTurn:'',activity:null,canInterrupt:false,canSteer:false,canSend:true,approvals:[],reviews:[],message:'',quiet:false,maintenance:'',lastReceipt:{id:'',outcome:'',message:''}});
 window.dispatchEvent(new Event('history-close'));setTimeout(()=>window.dispatchEvent(new Event('history-open')),50);
 window.dispatchEvent(new Event('files-changed'));
};
window.fixtureSendRegression=async(modes=['accepted','attachments-accepted','attachments-rejected','rejected','unknown','history-gap-accepted','history-recent-accepted','history-boundary-accepted','draft-failure'])=>{
 const results=[];let activeCase;
 const wait=async(test)=>{const deadline=performance.now()+8000;while(!test()){if(performance.now()>deadline)throw Error('send fixture timeout');await delay(20);}};
 try{
  for(const mode of modes){
   window.fixtureSendSetup(mode);
   await wait(()=>!!document.querySelector('.composer-editor[contenteditable=true]')&&snapshot.canSend);
   await delay(550);
   const editor=document.querySelector('.composer-editor');
   const text='检查发送体验。\n多行内容也应稳定。';
   editor.textContent=text;editor.dispatchEvent(new Event('input',{bubbles:true}));
   await wait(()=>!!document.querySelector('.send:not(:disabled)'));
   const frames=[],started=performance.now();let recording=true;activeCase={mode,frames};
   const record=()=>{
    const users=[...document.querySelectorAll('.message-row.user')],reply=document.querySelector('[data-message-id^="reply:"] .markdown-body');
    const composer=document.querySelector('.compose-area'),capsule=document.querySelector('.capsule'),group=users[0]?.closest('.history-message-group'),divider=group?.querySelector('.history-time-divider'),spinner=composer.querySelector('.composer-progress,.setup-spinner'),button=document.querySelector('.send');
    const spinnerRect=spinner?.getBoundingClientRect(),buttonRect=button.getBoundingClientRect();
    const spinnerInButton=!spinner||button.contains(spinner)&&parseFloat(getComputedStyle(spinner).width)<=18&&parseFloat(getComputedStyle(spinner).height)<=18&&spinnerRect.left>=buttonRect.left&&spinnerRect.right<=buttonRect.right&&spinnerRect.top>=buttonRect.top&&spinnerRect.bottom<=buttonRect.bottom;
    frames.push({time:performance.now()-started,users:users.length,userHeight:users[0]?.getBoundingClientRect().height??0,userTop:users[0]?.getBoundingClientRect().top??0,groupHeight:group?.getBoundingClientRect().height??0,divider:divider?.querySelector('time')?.dateTime??'',dividerBeforeUser:!divider||!!(divider.compareDocumentPosition(users[0])&Node.DOCUMENT_POSITION_FOLLOWING),dividers:document.querySelectorAll('.history-time-divider').length,composerExtraHeight:composer.getBoundingClientRect().height-52,spinner:!!spinner,spinnerInButton,emptyReply:!!reply&&!reply.textContent,editor:editor.innerText,files:document.querySelectorAll('.composer-context .attachments li').length,disabled:(editor.contentEditable!=='true'),stop:!!document.querySelector('.composer-stop'),loading:!!document.querySelector('.working-message .loading-dots'),delivery:document.querySelector('.message-row.user .message-delivery')?.textContent??'',reply:reply?.textContent??''});
    if(recording)requestAnimationFrame(record);
   };requestAnimationFrame(record);
   document.querySelector('.send').click();
   await delay(3500);recording=false;
   if(sendCount!==1)throw Error(mode+' dispatched '+sendCount+' submissions');
   if(frames.some(frame=>frame.users>1))throw Error(mode+' duplicated the outgoing message');
   const first=frames.find(frame=>frame.users);
   if(!first||first.editor!==''||first.files||!first.loading||first.delivery)throw Error(mode+' did not hand off editor/message/loading in the first visible frame');
   if(frames.filter(frame=>frame.time<500&&frame.users).some(frame=>frame.editor!==''||frame.files||!frame.loading||frame.delivery))throw Error(mode+' flashed the original draft or sending status before receipt');
   const heights=frames.filter(frame=>frame.users&&(mode.endsWith('accepted')||mode==='draft-failure'||frame.time<500)).map(frame=>frame.userHeight);
   if(Math.max(...heights)-Math.min(...heights)>1)throw Error(mode+' acknowledgement resized the outgoing bubble');
   if(frames.some(frame=>frame.emptyReply))throw Error(mode+' showed an empty reply bubble');
   const visible=frames.filter(frame=>frame.users),expectedDividers=mode==='history-gap-accepted'?1:0;
   if(visible.some(frame=>frame.dividers!==expectedDividers||!frame.dividerBeforeUser||frame.divider!==first.divider))throw Error(mode+' inserted, removed or relabelled a time divider after the first user frame');
   if(frames.some(frame=>!frame.spinnerInButton||frame.stop&&frame.spinner))throw Error(mode+' misplaced progress or obscured Stop');
   if(mode.endsWith('accepted')||mode==='draft-failure'){
    if(visible.some(frame=>frame.composerExtraHeight>3))throw Error(mode+' draft reconciliation added composer height');
    if(Math.max(...visible.map(frame=>frame.userTop))-Math.min(...visible.map(frame=>frame.userTop))>1)throw Error(mode+' late acknowledgement shifted the message list');
   }
   if(mode.endsWith('accepted')){
    if(acceptedReads!==1)throw Error('accepted draft read '+acceptedReads+' times');
    if(editor.innerText!=='')throw Error('accepted draft was restored');
    const settled=frames.findIndex(frame=>frame.editor==='');
    if(frames.slice(settled).some(frame=>frame.editor!==''))throw Error('cleared draft flashed back');
    if(!frames.some(frame=>frame.stop&&!frame.disabled))throw Error('stop action never became available');
    if(!document.querySelector('[data-message-id^="reply:"] .markdown-body')?.textContent.includes('合成回复'))throw Error('reply missing');
    if(document.querySelector('.composer-context .attachments li'))throw Error('accepted attachments reappeared');
   }else if(mode==='draft-failure'){
    if((editor.contentEditable!=='true')||document.querySelector('.input-error')||document.querySelector('.compose-area .setup-spinner,.compose-area .composer-progress')||!document.querySelector('.composer-stop'))throw Error('accepted draft recovery interrupted the composer or exposed a technical alert');
   }else if(editor.innerText!==text)throw Error(mode+' lost the unaccepted draft');
   if(mode==='attachments-rejected'&&document.querySelectorAll('.composer-context .attachments li').length!==1)throw Error('rejected attachment was not restored');
   if(mode==='unknown'){
    if(document.querySelector('.send:not(:disabled)'))throw Error('unknown request can be sent again');
    document.querySelector('.compose-area > .quiet')?.click();await delay(350);
    if(document.querySelector('.send:not(:disabled)')||sendCount!==1)throw Error('local cleanup unlocked an uncertain native request');
    // A later authoritative rejection ends uncertainty without another send.
    snapshot.lastReceipt={...snapshot.lastReceipt,outcome:'rejected'};
    await wait(()=>!!document.querySelector('.send:not(:disabled)'));
    if(sendCount!==1||editor.innerText!==text)throw Error('late refusal replayed or lost the original draft');
   }
   results.push({mode,sendCount,acceptedReads,frames});
  }
  return window.fixtureSendResult={ok:true,results};
 }catch(error){return window.fixtureSendResult={ok:false,error:String(error),results,failed:activeCase};}
};
export const Call={ByName:async(name,...args)=>{
 const method=name.split('.').at(-1);
 if(method==='LanguagePreferences')return {preference:previewParams.get('lang')==='en'?'en':'zh-CN',locale:previewParams.get('lang')==='en'?'en':'zh-CN',revision:1};
 if(method==='Appearance')return {revision:1,selection:{character:'builtin:caelis',avatar:'follow'},model:'',avatar:'',basic:false,key:'builtin:caelis'};
 if(method==='CharacterActivity')return petState;
 if(method==='Placement')return {visible:true,x:0,y:0,scale:1};
 if(method==='PetSnapshot'||method==='Snapshot'||method==='ComposerSnapshot')return structuredClone({...snapshot,revision:++revision});
 if(method==='ChatSnapshot'){
  const value=structuredClone({...snapshot,revision:sendMode?revision:++revision});
  if(sendMode==='accepted'&&sendCount&&!staleHeld){staleHeld=true;await delay(1200);}
  return {changed:true,snapshot:value};
 }
 if(method==='SaveDraft'){draft={...args[0],revision:draft.revision+1,notice:''};return {...draft};}
 if(method==='Submit'){
  sendCount++;const history=[...snapshot.items],input=args[0],selected=draftFiles.filter(file=>input.fileIds.includes(file.id)),outgoing={id:'outgoing:'+input.id,requestId:input.id,turnKey:'',kind:'user',text:[input.text,...selected.map(file=>file.name)].join('\n'),quoted:input.quoted,status:'sending',artifacts:[]};snapshot.items=[...history,outgoing];
  if(sendMode.startsWith('attachments-')){if(selected.length!==1)throw Error('original attachment missing');setTimeout(()=>window.dispatchEvent(new Event('files-changed')),100);}
  await delay(600);
  const outcome=sendMode.endsWith('rejected')?'rejected':sendMode==='unknown'?'unknown':'accepted';
  snapshot.lastReceipt={id:input.id,outcome,message:outcome==='accepted'?'':'合成发送未确认'};outgoing.status=outcome;
  if(outcome==='accepted'){
   draftFiles=draftFiles.filter(file=>!input.fileIds.includes(file.id));
   draft={text:'',referenceIds:[],notice:'',revision:draft.revision+1};
   Object.assign(snapshot,{phase:'working',currentTurn:input.id,canInterrupt:true,canSend:false,canSteer:true});
   const confirmed={...outgoing,id:'native-user:'+input.id,status:'completed'};
   snapshot.items=[...history,confirmed];
   // Timestamp enrichment intentionally arrives after the native row. The
   // boundary case crosses five minutes only at acknowledgement, not at send.
   setTimeout(()=>{confirmed.seenAt=sendMode==='history-boundary-accepted'?history[0].seenAt+301e6:Date.now()*1000;},1000);
   setTimeout(()=>{snapshot.items.push({id:'reply:'+input.id,turnKey:input.id,kind:'assistant',text:'这是合成回复。发送和首段回复现在可以连续衔接。',status:'inProgress',artifacts:[]});},2000);
  }
  await delay(300);return {...snapshot.lastReceipt};
 }
 if(method==='Draft'){draftReads++;if(sendCount&&snapshot.lastReceipt.outcome==='accepted'){acceptedReads++;await delay(200);if(sendMode==='draft-failure')throw Error('Synthetic draft read failure');}return {...draft};}
 if(method==='DraftFiles')return draftFiles;
 if(method==='DraftImage')return '__MEDIA_ONE__';
 if(method==='RemoveFile'){draftFiles=draftFiles.filter(file=>file.id!==args[0]);return draftFiles;}
 if(method==='PickFiles')return draftFiles;
 if(method==='PasteAttachments')return {handled:true,files:draftFiles};
 if(method==='MediaImage')return args[0]==='media-fixture-one'?'__MEDIA_ONE__':'__MEDIA_TWO__';
 if(method==='HistoryVisible')return true;
 if(method==='Interrupt')window.fixtureSet('stop');
 if(method==='DismissPreview')snapshot.previewDismissed=true;
 if(method==='Decide'){const p=snapshot.approvals.find(p=>p.id===args[0].id);if(p){p.status='sent';revision++;await new Promise(r=>setTimeout(r,900));p.status='resolved';p.resolution={choiceId:args[0].choice,outcome:args[0].choice.includes('deny')?'declined':'allowed',scope:'once'};revision++;}return;}
 if(method==='AnswerQuestion'||method==='DismissQuestion'){
  const card=snapshot.items.flatMap(item=>item.questionCards??[]).find(card=>card.shortId===args[0]&&card.binding===args[1]);
  if(!card)return '这条问题卡片已失效，请使用最新卡片。';
  window.fixtureQuestionResult=args;
  if(method==='AnswerQuestion'){
   const selected=card.options[Number(args[2])-1]??args[2],requestId=args[3];
   const reply={id:'control-input:'+requestId,requestId,kind:'user',text:selected,status:'sending',seenAt:Date.now()*1000,questionReply:{shortId:card.shortId,binding:card.binding,title:card.title,answer:selected,outcome:'sending'}};
   snapshot.items.push(reply);revision++;await new Promise(r=>setTimeout(r,600));
   reply.status=reply.questionReply.outcome=window.fixtureReplyOutcome||previewParams.get('replyOutcome')||'accepted';
   card.state=reply.status==='accepted'?'answered':reply.status==='unknown'?'unknown':'';
  }else card.state='dismissed';revision++;
  return method==='AnswerQuestion'?(card.state==='unknown'?'回答投递结果待核对，不会自动重发。':card.state==='answered'?'回答已提交，等待后续回复。':'回答未提交。'):'问题已关闭，未提供答案。';
 }

 if(method==='OpenApproval')window.dispatchEvent(new Event('bubble-expand'));
 if(method==='CollapseBubble')window.dispatchEvent(new Event('bubble-collapse'));
 window.webkit?.messageHandlers.preview?.postMessage({method,args});
}};
`;
const root=resolve('frontend/dist');
const server=createServer((req,res)=>{
 const path=new URL(req.url,'http://127.0.0.1').pathname;
 if(path==='/wails/runtime.js'){res.setHeader('Content-Type','text/javascript');res.end(runtime.replace('__MEDIA_ONE__','data:image/png;base64,'+readFileSync('frontend/public/portraits/caelis-sage-v1/poster.png').toString('base64')).replace('__MEDIA_TWO__','data:image/png;base64,'+readFileSync('frontend/public/icons/caelis-avatar.png').toString('base64')));return;}
 const file=resolve(root,'.'+(path==='/'?'/index.html':path));
 if(!file.startsWith(root+sep)){res.writeHead(403);res.end();return;}
 try {const data=readFileSync(file);res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.png':'image/png'})[extname(file)]??'application/octet-stream');res.end(data);}
 catch{res.writeHead(404);res.end();}
});
server.listen(0,'127.0.0.1',()=>{const url='http://127.0.0.1:'+server.address().port+'/?surface='+(process.env.BOT_PREVIEW_SURFACE||'bubble')+'&lang='+(process.env.BOT_PREVIEW_LANG||'zh-CN')+'&fixture='+(process.env.BOT_PREVIEW_FIXTURE||'')+(process.env.BOT_PREVIEW_OPEN_QUOTE?'&openQuote=1':'')+'&questionCase='+(process.env.BOT_PREVIEW_QUESTION_CASE||'')+'&width='+(process.env.BOT_PREVIEW_WIDTH||760)+'&height='+(process.env.BOT_PREVIEW_HEIGHT||672);writeFileSync(process.argv[2],url);console.log(url);});
