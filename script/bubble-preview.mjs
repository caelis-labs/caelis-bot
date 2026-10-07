// Disposable UI fixture: serves the built React surfaces with synthetic facts.
// No application data, native backend connection or model requests.
import {createServer} from 'node:http';
import {readFileSync,writeFileSync} from 'node:fs';
import {resolve,extname} from 'node:path';

const runtime=String.raw`
let revision=1,streamTimer=0,recordFrame=0,petState='idle';
const previewParams=new URLSearchParams(location.search);
let draftFiles=previewParams.get('fixture')==='attachments'?[{id:'fixture-image',name:'Pasted image.png',size:184320,type:'image/png',image:true,unavailable:false},{id:'fixture-file',name:'project-notes.pdf',size:2457600,type:'application/pdf',image:false,unavailable:false}]:[];
window.fixtureFrames=[];
// Fixture-only frame accounting verifies that completed portraits release RAFs.
const requestFrame=window.requestAnimationFrame.bind(window),cancelFrame=window.cancelAnimationFrame.bind(window);
const pendingFrames=new Set();
window.requestAnimationFrame=callback=>{const id=requestFrame(time=>{pendingFrames.delete(id);callback(time);});pendingFrames.add(id);return id;};
window.cancelAnimationFrame=id=>{pendingFrames.delete(id);cancelFrame(id);};
const short='钉好了 — 任务卡片现在是 **固定** 状态，\`pinned: true\`，会一直留在你脚底下。';
const long=short+'\n\n### 工作进展\n\n- 已读取 **README.md**\n- 已完成网络搜索\n- 正在更新说明文件\n\n> 悬浮可以阅读完整内容，移开后收起。\n\n| 操作 | 状态 |\n| --- | --- |\n| 阅读 | 完成 |\n| 编辑 | 完成 |\n\n\`\`\`go\nfmt.Println("Caelis Bot")\n\`\`\`\n\n'+Array.from({length:16},(_,i)=>(i+1)+'. 这是用于验证完整内容和屏幕高度限制的长段落。**格式保持可读**，滚动可继续阅读。').join('\n\n')+'\n\n**全文结束 END**\n\n[示例链接](https://example.com)';
const snapshot={connection:'ready',phase:'working',currentTurn:'fixture',canInterrupt:true,canSend:false,canSteer:true,quiet:false,items:[],approvals:[],reviews:[],references:[],message:'',previewKey:'fixture',previewDismissed:false,botStatus:'',hasEarlier:false,lastReceipt:{id:'',outcome:'',message:''}};
window.fixtureSet=kind=>{
 clearInterval(streamTimer);
 Object.assign(snapshot,{quiet:false,maintenance:'',phase:'working',canInterrupt:true,activity:{kind:'read',target:'README.md'},approvals:[],reviews:[],previewDismissed:false});
 snapshot.items=[{id:'answer',turnKey:'fixture',kind:'assistant',text:kind==='short'?short:long,status:'completed',artifacts:[]}];
 if(['read','edit','web','execute'].includes(kind))snapshot.activity={kind,target:kind==='read'?'README.md':kind==='edit'?'notes.md':''};
 if(kind==='review')snapshot.reviews=[{id:'review',status:'inProgress',action:'',rationale:''}];
 if(kind==='approval')snapshot.approvals=[{id:'approval',title:'允许读取所选文件？',status:'pending',action:'cat README.md',details:'',target:'README.md',questions:[],choices:[{id:'allow',label:'允许一次',scope:'once'},{id:'deny',label:'拒绝',scope:'once'}]}];
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
if(previewParams.get('surface')==='panel')setTimeout(()=>window.dispatchEvent(new CustomEvent('panel-open',{detail:{activation:1}})),200);
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
  await wait('completed reply still animates',()=>visible()&&body().textContent.length>0&&text.startsWith(body().textContent)&&body().textContent!==text);
  await wait('reply fully revealed',()=>visible()&&body().textContent===text);
  for(const overlay of ['approval','review','notice','message']){
   if(overlay==='approval')snapshot.approvals=[{id:'overlay-approval',title:'允许读取所选文件？',status:'pending',action:'cat README.md',target:'README.md',details:'',questions:[],choices:[]}];
   if(overlay==='review')snapshot.reviews=[{id:'overlay-review',status:'completed',action:'',rationale:''}];
   if(overlay==='notice')window.dispatchEvent(new CustomEvent('terminal-notice',{detail:{message:'合成通知',pending:true}}));
   if(overlay==='message')snapshot.message='合成连接提示';
   await wait(overlay+' shown',()=>!visible());
   snapshot.approvals=[];snapshot.reviews=[];snapshot.message='';
   if(overlay==='notice')window.dispatchEvent(new CustomEvent('terminal-notice',{detail:{message:'',pending:false}}));
   await wait(overlay+' dismissed',visible);
   samples.push({overlay,text:body().textContent});
   if(body().textContent!==text)throw Error(overlay+' replayed a fully revealed reply');
  }
  // Completion does not drain a queued tail, and a temporary overlay must not
  // restart or flush that tail either.
  item.text=text+'完成后的末尾仍然逐字出现。'.repeat(30);
  await wait('queued tail',()=>visible()&&body().textContent.length>text.length&&body().textContent!==item.text);
  const before=body().textContent;
  snapshot.message='合成连接提示';
  await wait('tail overlay',()=>!visible());
  snapshot.message='';
  await wait('tail restored',visible);
  if(!body().textContent.startsWith(before)||body().textContent===item.text)throw Error('overlay restarted or flushed queued tail');
  await wait('tail complete',()=>body().textContent===item.text);
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
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
window.fixtureSendSetup=mode=>{
 clearInterval(streamTimer);cancelAnimationFrame(recordFrame);sendMode=mode;sendCount=0;draftReads=0;acceptedReads=0;staleHeld=false;
 draft={text:'',referenceIds:[],notice:'',revision:draft.revision+1};
 Object.assign(snapshot,{items:[],phase:'idle',currentTurn:'',activity:null,canInterrupt:false,canSteer:false,canSend:true,approvals:[],reviews:[],message:'',quiet:false,maintenance:'',lastReceipt:{id:'',outcome:'',message:''}});
 window.dispatchEvent(new Event('history-close'));setTimeout(()=>window.dispatchEvent(new Event('history-open')),50);
};
window.fixtureSendRegression=async()=>{
 const results=[];
 const wait=async(test)=>{const deadline=performance.now()+8000;while(!test()){if(performance.now()>deadline)throw Error('send fixture timeout');await delay(20);}};
 try{
  for(const mode of ['accepted','rejected','unknown','draft-failure']){
   window.fixtureSendSetup(mode);
   await wait(()=>!!document.querySelector('textarea:not(:disabled)')&&snapshot.canSend);
   await delay(550);
   const editor=document.querySelector('textarea');
   const text='检查发送体验。\n多行内容也应稳定。';
   Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value').set.call(editor,text);editor.dispatchEvent(new Event('input',{bubbles:true}));
   await wait(()=>!!document.querySelector('.send:not(:disabled)'));
   const frames=[],started=performance.now();let recording=true;
   const record=()=>{
    const users=[...document.querySelectorAll('.message-row.user')],reply=document.querySelector('.message-row.assistant .markdown-body');
    frames.push({time:performance.now()-started,users:users.length,userHeight:users[0]?.getBoundingClientRect().height??0,emptyReply:!!reply&&!reply.textContent,editor:editor.value,disabled:editor.disabled,stop:!!document.querySelector('.composer-stop'),reply:reply?.textContent??''});
    if(recording)requestAnimationFrame(record);
   };requestAnimationFrame(record);
   document.querySelector('.send').click();
   await delay(3500);recording=false;
   if(sendCount!==1)throw Error(mode+' dispatched '+sendCount+' submissions');
   if(frames.some(frame=>frame.users>1))throw Error(mode+' duplicated the outgoing message');
   const heights=frames.filter(frame=>frame.users).map(frame=>frame.userHeight);
   if(Math.max(...heights)-Math.min(...heights)>1)throw Error(mode+' acknowledgement resized the outgoing bubble');
   if(frames.some(frame=>frame.emptyReply))throw Error(mode+' showed an empty reply bubble');
   if(mode==='accepted'){
    if(acceptedReads!==1)throw Error('accepted draft read '+acceptedReads+' times');
    if(editor.value!=='')throw Error('accepted draft was restored');
    const settled=frames.findIndex(frame=>frame.editor==='');
    if(frames.slice(settled).some(frame=>frame.editor!==''))throw Error('cleared draft flashed back');
    if(!frames.some(frame=>frame.stop&&!frame.disabled))throw Error('stop action never became available');
    if(!document.querySelector('.markdown-body')?.textContent.includes('合成回复'))throw Error('reply missing');
   }else if(mode==='draft-failure'){
    if(editor.disabled||!document.querySelector('.input-error')?.textContent.includes('消息已发送')||!document.querySelector('.compose-area > .quiet')||!document.querySelector('.composer-stop'))throw Error('accepted draft failure lost its local recovery path');
   }else if(editor.value!==text)throw Error(mode+' lost the unsent draft');
   results.push({mode,sendCount,acceptedReads,frames});
  }
  return window.fixtureSendResult={ok:true,results};
 }catch(error){return window.fixtureSendResult={ok:false,error:String(error),results};}
};
export const Call={ByName:async(name,...args)=>{
 const method=name.split('.').at(-1);
 if(method==='LanguagePreferences')return {preference:previewParams.get('lang')==='en'?'en':'zh-CN',locale:previewParams.get('lang')==='en'?'en':'zh-CN',revision:1};
 if(method==='Appearance')return {revision:1,selection:{character:'builtin:caelis',avatar:'follow'},model:'',avatar:'',basic:false,key:'builtin:caelis'};
 if(method==='CharacterActivity')return petState;
 if(method==='Placement')return {visible:true,x:0,y:0,scale:1};
 if(method==='PetSnapshot'||method==='Snapshot'||method==='ComposerSnapshot')return {...snapshot,revision:++revision};
 if(method==='ChatSnapshot'){
  const value=structuredClone({...snapshot,revision:sendMode?revision:++revision});
  if(sendMode==='accepted'&&sendCount&&!staleHeld){staleHeld=true;await delay(1200);}
  return {changed:true,snapshot:value};
 }
 if(method==='SaveDraft'){draft={...args[0],revision:draft.revision+1,notice:''};return {...draft};}
 if(method==='Submit'){
  sendCount++;const input=args[0],outgoing={id:'outgoing:'+input.id,requestId:input.id,turnKey:'',kind:'user',text:input.text,status:'sending',artifacts:[]};snapshot.items=[outgoing];
  await delay(600);
  const outcome=sendMode==='rejected'?'rejected':sendMode==='unknown'?'unknown':'accepted';
  snapshot.lastReceipt={id:input.id,outcome,message:outcome==='accepted'?'':'合成发送未确认'};outgoing.status=outcome;
  if(outcome==='accepted'){
   draft={text:'',referenceIds:[],notice:'',revision:draft.revision+1};
   Object.assign(snapshot,{phase:'working',currentTurn:input.id,canInterrupt:true,canSend:false,canSteer:true});
   snapshot.items=[{...outgoing,id:'native-user:'+input.id,status:'completed'}];
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
 if(method==='Decide')window.fixtureSet('read');
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
 if(!file.startsWith(root+'/')){res.writeHead(403);res.end();return;}
 try {const data=readFileSync(file);res.setHeader('Content-Type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.png':'image/png'})[extname(file)]??'application/octet-stream');res.end(data);}
 catch{res.writeHead(404);res.end();}
});
server.listen(0,'127.0.0.1',()=>{const url='http://127.0.0.1:'+server.address().port+'/?surface='+(process.env.BOT_PREVIEW_SURFACE||'bubble')+'&lang='+(process.env.BOT_PREVIEW_LANG||'zh-CN')+'&fixture='+(process.env.BOT_PREVIEW_FIXTURE||'');writeFileSync(process.argv[2],url);console.log(url);});
