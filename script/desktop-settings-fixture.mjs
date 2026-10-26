// Synthetic transport only. No native backend, account, plugin store or model.
export const fixtureRuntime=String.raw`
const params=new URLSearchParams(location.search),calls=[];
window.fixtureCalls=calls;
window.fixtureSetExperiments=(values,state='')=>{for(const f of experiments.features)if(f.id in values)f.enabled=values[f.id];experiments.revision=String(Number(experiments.revision)+1);experiments.saveState=state};
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
let section=params.get('section')||'connections',draft={revision:1,referenceIds:[],text:'保留的草稿 · retained draft',references:[],quote:null},revision=1;
const experiments={supported:true,revision:'1',features:['async_compact','mcp_catalog_refresh','deferred_tool_retirement'].map(id=>({id,enabled:false})),saveState:'',operationId:''};
const files=[{id:'fixture-file',name:'fixture-notes.txt',size:32,type:'text/plain',image:false,unavailable:false}];
const option={model:'fixture/model',name:'Saved Model',efforts:['low','high'],defaultEffort:'high',serviceTiers:[],default:false,imageInput:false};
const selections=[];
const remote=['list','pending','unknown','unsent'].includes(params.get('machines'))?[{id:'bot-fixture',name:'Fedora',address:'http://fedora.test:9317',connectionType:'bot',pairingStatus:'approved',state:'ready',checkedAt:Date.now(),issue:''},{id:'bot-pending',name:'Build server',address:'http://build.test:9317',connectionType:'bot',pairingStatus:'pending',state:'authorization',checkedAt:Date.now(),issue:'peer_pairing_pending'},{id:'bot-revoked',name:'Mac Studio',address:'http://studio.test:9317',connectionType:'bot',pairingStatus:'revoked',state:'offline',checkedAt:Date.now(),issue:'peer_authorization_revoked'}]:[];
if(['unknown','unsent'].includes(params.get('machines')))remote.splice(0,remote.length,{...remote[1],id:'bot-unknown',pairingStatus:'unknown'});
let sshConfig={path:'/Synthetic/Bot/Machines/SSH/config',userPath:'/Synthetic/.ssh/config',systemPath:'/etc/ssh/ssh_config',revision:'1',text:'Host fedora\n    HostName 192.168.1.25\n    User alice\n    ProxyJump bastion\n',hosts:['fedora','bastion']},sshStatus={state:'idle'};
let sharing={address:'http://100.64.0.5:9317',listen:'0.0.0.0:9317',listening:true,managed:false,peer:'synthetic-local',requests:[{id:'fixture-request',name:'MacBook Air',address:'100.64.0.8',fingerprint:'0123456789abcdef'.repeat(4),expiresAt:Math.floor(Date.now()/1000)+300}],sources:[{id:'synthetic-source-A',name:'Mac Studio',pairedAt:1}]};
if(params.get('inbox')!=='pending')sharing.requests=[];
if(params.get('sources')!=='yes')sharing.sources=[];
if(params.get('receiveIssue'))sharing.issue=params.get('receiveIssue');
window.fixtureMachines=remote;
window.fixtureRemoteFailure=params.get('machines')==='error';
window.fixtureRemoteDelay=params.get('machines')==='loading'?1200:0;
window.fixturePairingError=params.get('pairingError')||'';

const plugins={revision:1,items:['stable','target','notes'].map((id,i)=>({id,title:['Stable Tools','New Plugin','Notes Skill'][i],version:'1.0',description:'Synthetic local regression fixture',installed:i!==1,enabled:i!==1,status:i!==1?'enabled':'available',skills:i===2?[{id:'notes',name:'Notes'}]:[],mcpServers:i===2?[]:[{id:'tools',name:'Tools'}]}))};
window.fixtureMenuData=(count=2,nativeCount=2,fileCount=0)=>{
 plugins.items=Array.from({length:count},(_,i)=>({id:'menu-'+i,title:'Plugin '+i+' — 长名称排版',description:'用于检查插件菜单描述的可读性、可点击区域和滚动边界，不执行任何外部操作。',installed:true,enabled:true,status:'enabled'}));
 window.fixtureNativePlugins=Array.from({length:nativeCount},(_,i)=>({id:'native-'+i,name:'Native '+i,description:'Synthetic native plugin package',source:'codex'}));
 files.splice(0,files.length,...Array.from({length:fileCount},(_,i)=>({id:'menu-file-'+i,name:'menu-notes-'+i+'.txt',size:32,type:'text/plain',image:false,unavailable:false})));
 window.dispatchEvent(new Event('files-changed'));
};
window.fixtureRefreshDelay=0;window.fixtureRuntimeFailure='';window.fixtureInstallFailure=false;
window.fixtureDraftFailure=false;window.fixtureUnknownSend=false;
window.fixtureChatFailure=fail=>{window.fixtureDraftFailure=fail;window.dispatchEvent(new Event('files-changed'));};
window.fixtureNavigate=next=>{section=next;window.dispatchEvent(new Event('settings-open'))};
const snapshot={connection:'ready',phase:'idle',canSend:true,canInterrupt:false,canSteer:false,items:Array.from({length:24},(_,i)=>({id:'fixture-'+i,kind:i%2?'assistant':'user',text:'Synthetic retained conversation '+i+' — 用于核对滚动、草稿和附件保留。',status:'completed',artifacts:[]})),approvals:[],reviews:[],references:[],lastReceipt:{id:'',outcome:'',message:''},hasEarlier:false,message:'',revision:1};
const native=action=>window.webkit?.messageHandlers.navigation.postMessage(action);
export const Call={ByName:async(name,...args)=>{
 const method=name.split('.').at(-1);calls.push(method);
 switch(method){
 case 'SettingsSection':return section;
 case 'SettingsVisible':return true;
 case 'HistoryVisible':return true;
 case 'AppVersion':return 'Synthetic Dev';
 case 'SystemPermissions':return {supported:true,appPath:'/Synthetic/Caelis Settings Preview.app',development:true,permissions:[{id:'accessibility',status:'authorized'},{id:'screenCapture',status:'notDetermined'},{id:'automation',status:'notDetermined',target:'Terminal'},{id:'notifications',status:'authorized'}]};
 case 'SurfaceReady':return;
 case 'OpenSettings':native('settings');return;
 case 'OpenHistory':native('chat');return;
 case 'CloseSettings':case 'CloseHistory':native('hide');return;
 case 'LanguagePreferences':return {preference:'zh-CN',locale:'zh-CN',revision:1};
 case 'Appearance':return {revision:1,selection:{character:'builtin:caelis',avatar:'follow'},model:'',avatar:'',basic:false,key:'builtin:caelis'};
 case 'Display':case 'DisplayState':return {scale:1};
 // Read-only presentation fixtures for the remaining common settings pages.
 // Unsupported mutations still fail below; these never touch host preferences.
 case 'DisplayPreferences':return {mode:params.get('theme')||'system',theme:'default',revision:1};
 case 'ContentState':return {appearance:{revision:1,selection:{character:'builtin:caelis',avatar:'follow'},model:'',avatar:'',basic:false,key:'builtin:caelis'},characters:[{id:'builtin:caelis',name:'Caelis',pack:'',group:''}],avatars:[],packs:[],notice:''};
 case 'Placement':return {scale:1};
 case 'LoginAtLoginStatus':return {supported:true,enabled:false,state:'off',registered:false};
 case 'TaskPreferences':return {revision:1,maxRunning:6,terminal:'system',customCommand:''};
 case 'TerminalChoices':return [{id:'system',name:'System',available:true}];
 case 'ShortcutSettings':case 'TaskShortcutSettings':return {shortcut:{enabled:true,key:method==='TaskShortcutSettings'?'KeyT':'Space',control:true,shift:true,alt:false,meta:false},registered:true,message:''};
 case 'TelegramStatus':return {enabled:false,bot:'',owner:'',paired:false,candidate:'',pairURL:'',issue:''};
 case 'WeixinStatus':return {phase:'unconfigured'};
 case 'ReadSSHConfiguration':if(window.fixtureSSHReadFailure)throw Error('ssh_config_unreadable');return {kind:args[0],exists:true,path:args[0]==='bot'?sshConfig.path:sshConfig.userPath,text:sshConfig.text};
 case 'EditSSHConfiguration':window.fixtureSSHEdit=args[0];return;
 case 'RevealSSHConfiguration':window.fixtureSSHReveal=args[0];return;
 case 'SSHConfiguration':return structuredClone(sshConfig);
 case 'SaveSSHConfiguration':if(window.fixtureSSHConflict)throw Error('ssh_config_changed');sshConfig={...sshConfig,text:args[0].text,revision:'2'};return structuredClone(sshConfig);
 case 'StartSSHConnection':if(params.get('ssh')==='late-start'&&!window.fixtureSSHStarted){window.fixtureSSHStarted=true;sshStatus={state:'failed',issue:'ssh_connection_failed'};await new Promise((resolve,reject)=>window.fixtureSSHStartRelease=()=>reject(Error('late old start')))}if(params.get('ssh')==='same-start'&&!window.fixtureSSHStarted){window.fixtureSSHStarted=true;sshStatus={state:'authentication',prompt:{id:'ssh-original-prompt',text:'Key passphrase:',confirm:false}};await new Promise(resolve=>window.fixtureSSHStartRelease=resolve);return {state:'connecting'}}sshStatus={state:'authentication',prompt:{id:'ssh-original-prompt',text:['password','late-start','late-answer','same-start','late-answer-error'].includes(params.get('ssh'))?'Key passphrase:':"The authenticity of host 192.168.1.25 cannot be established.\nED25519 key fingerprint is SHA256:fixture-only.\nAre you sure you want to continue connecting (yes/no)?",confirm:!['password','late-start','late-answer','same-start','late-answer-error'].includes(params.get('ssh'))}};return structuredClone(sshStatus);
 case 'SSHConnectionStatus':return structuredClone(sshStatus);
 case 'AnswerSSH':window.fixtureSSHAnswer=args[0];if(['late-answer','late-answer-error'].includes(params.get('ssh'))&&!window.fixtureSSHAnswered){window.fixtureSSHAnswered=true;sshStatus={state:'authentication',prompt:{id:'ssh-next-prompt',text:'Second key passphrase:',confirm:false}};await new Promise((resolve,reject)=>window.fixtureSSHAnswerRelease=()=>params.get('ssh')==='late-answer-error'?reject(Error('ssh_prompt_expired')):resolve());return}sshStatus={state:'ready'};return;
 case 'StopSSHConnection':sshStatus={state:'idle'};return;
 case 'Machines':if(window.fixtureRemoteDelay)await sleep(window.fixtureRemoteDelay);if(window.fixtureRemoteFailure)throw Error('private backend details never rendered');return structuredClone(remote.map(v=>v.state==='ready'?{...v,checkedAt:Date.now()}:v));
 case 'ConnectBot':{
  if(params.get('connectError'))throw Error(params.get('connectError'));
  const input=args[0];
  const v={id:input.id,name:input.name||'Fedora',address:input.address,connectionType:'bot',pairingStatus:'pending',state:'authorization',checkedAt:Date.now(),issue:''};if(params.get('connectedIssue')){v.pairingStatus='approved';v.state='offline';v.issue=params.get('connectedIssue')}const index=remote.findIndex(v=>v.id===input.id);if(index<0)remote.push(v);else remote[index]=v;return structuredClone(v);
 }
 case 'InspectMachine':case 'RecoverBotWork':{if(window.fixtureInspectHold){window.fixtureInspectHold=false;await new Promise(resolve=>window.fixtureInspectRelease=resolve);throw Error('late private observation error')}if(window.fixtureInspectFailure)throw Error('private observation error');const v=remote.find(v=>v.id===args[0]);return structuredClone(v?{...v,issue:(params.get('machines')==='unsent'||window.fixtureUndispatched)?'pairing_request_not_dispatched':v.issue,checkedAt:Date.now()}:v)}
 case 'RemoveMachine':{const i=remote.findIndex(v=>v.id===args[0]);if(i>=0)remote.splice(i,1);return;}
 case 'DecideWorkerPairing':{if(window.fixturePairingError)throw Error(window.fixturePairingError);const request=sharing.requests.find(v=>v.id===args[0].id);if(request&&args[0].allow)sharing.sources.push({id:'source-for-'+request.id,name:request.name,pairedAt:Math.floor(Date.now()/1000)});sharing.requests=sharing.requests.filter(v=>v.id!==args[0].id);return;}
 case 'RefreshWorkerSharing':case 'WorkerSharing':return structuredClone(sharing);
 case 'RevokeWorkerPeer':sharing.sources=sharing.sources.filter(s=>s.id!==args[0]);return;
 case 'CopyText':window.fixtureCopied=args[0];return;

 case 'UpdatePreferences':return {available:true,automatic:false,waiting:false};
 case 'ChatSnapshot':return {changed:true,snapshot};
 case 'Snapshot':case 'ComposerSnapshot':case 'SnapshotSummary':return snapshot;
 case 'Draft':return draft;
 case 'SaveDraft':draft={...draft,...args[0],revision:draft.revision+1};return draft;
 case 'PickFiles':return structuredClone(files);
 case 'DraftFiles':if(window.fixtureDraftFailure)throw Error('Synthetic attachment read unavailable');return files;
 case 'Submit':{
  if(!window.fixtureUnknownSend)throw Error('Synthetic send disabled');
  draft.pendingSend=true;snapshot.lastReceipt={id:args[0].id,outcome:'unknown',message:'Synthetic receipt pending'};
  return snapshot.lastReceipt;
 }
 case 'BotInitialization':return {required:false,status:'completed'};
 case 'NativePlugins':return window.fixtureNativePlugins||[];
 case 'SetNativeMenuOpen':return;
 case 'Plugins':return structuredClone(plugins);
 case 'PluginAction':{
  await sleep(650);if(window.fixtureInstallFailure)throw Error('Synthetic install failed');
  const item=plugins.items.find(p=>p.id===args[0]);item.installed=true;item.enabled=true;item.status='pending';plugins.revision++;plugins.syncState='pending';
  setTimeout(()=>{item.status='enabled';plugins.syncState='';plugins.revision++},2000);return structuredClone(plugins);
 }
 case 'SetupOverview':return {active:'codex',hasRuntimeChoice:true,onboarding:false,pending:''};
 case 'ExecutionSettings':return {model:'fixture/model',effort:'high',serviceTier:'',approvalMode:'auto'};
 case 'LocalWorkerSelection':return {runtime:'conversation',work:{model:'',effort:'',serviceTier:''}};
 case 'ModelSelections':return structuredClone(selections);
 case 'SaveSelectedModel':selections.splice(0,selections.length,{scope:args[0],runtime:args[1],selection:args[2],revision:++revision,state:'pending'});return;
 case 'SetupProfile':return {runtime:args[0],cliPath:'',caelisStore:''};
 case 'InspectSetup':{
  await sleep(window.fixtureRefreshDelay||800);
  if(window.fixtureRuntimeFailure===args[0].runtime)throw Error('Synthetic Runtime read failure');
  return {settings:args[0],installation:{installed:true,version:'fixture',path:'',updateState:''},state:'ready',models:[{value:option.model,label:option.name,parameters:option,current:true,noAuth:false}],selectedModel:option.model,accountType:'chatgpt',loginPending:false};
 }
 case 'RuntimeExperiments':if(window.fixtureExperimentalInFlight)await window.fixtureExperimentalInFlight;if(window.fixtureExperimentalReadFailure)throw Error('synthetic read failure');return structuredClone(experiments);
 case 'ChangeRuntimeExperiments':{
  const input=args[0];window.fixtureExperimentalInFlight=sleep(window.fixtureExperimentalDelay||250);await window.fixtureExperimentalInFlight;window.fixtureExperimentalInFlight=null;
  if(input.expectedRevision!==experiments.revision)return {...structuredClone(experiments),saveState:'conflicted'};
  if(window.fixtureExperimentalReject)return {...structuredClone(experiments),saveState:'rejected'};
  if(experiments.saveState==='pending')return structuredClone(experiments);
  experiments.operationId='fixture-'+calls.length;
  if(window.fixtureExperimentalPending){experiments.saveState='pending';return structuredClone(experiments)}
  experiments.features.find(f=>f.id===input.id).enabled=input.enabled;experiments.revision=String(Number(experiments.revision)+1);experiments.saveState='saved';return structuredClone(experiments);
 }
 case 'ProfileConfiguration':return {revision:'fixture',models:[option],connections:[],team:{models:[],roles:[]}};
 default:throw Error('Fixture method unavailable: '+method);
 }
}};
`;
