// Synthetic transport only. No native backend, account, plugin store or model.
export const fixtureRuntime=String.raw`
const params=new URLSearchParams(location.search),calls=[];
window.fixtureCalls=calls;
window.fixtureSetExperiments=(values,state='')=>{for(const f of experiments.features)if(f.id in values)f.enabled=values[f.id];experiments.revision=String(Number(experiments.revision)+1);experiments.saveState=state};
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
let section=params.get('section')||'connections',draft={revision:1,referenceIds:[],text:'保留的草稿 · retained draft',references:[],quote:null},revision=1;
const experiments={supported:true,revision:'1',features:['async_compact','mcp_catalog_refresh','deferred_tool_retirement'].map(id=>({id,enabled:true})),saveState:'',operationId:''};
const files=[{id:'fixture-file',name:'fixture-notes.txt',size:32,type:'text/plain',image:false,unavailable:false}];
const option={model:'fixture/model',name:'Saved Model',efforts:['low','high'],defaultEffort:'high',serviceTiers:[],default:false,imageInput:false};
const selections=[];
const plugins={revision:1,items:['stable','target','notes'].map((id,i)=>({id,title:['Stable Tools','New Plugin','Notes Skill'][i],version:'1.0',description:'Synthetic local regression fixture',installed:i!==1,enabled:i!==1,status:i!==1?'enabled':'available',skills:i===2?[{id:'notes',name:'Notes'}]:[],mcpServers:i===2?[]:[{id:'tools',name:'Tools'}]}))};
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
 case 'SurfaceReady':return;
 case 'OpenSettings':native('settings');return;
 case 'OpenHistory':native('chat');return;
 case 'CloseSettings':case 'CloseHistory':native('hide');return;
 case 'LanguagePreferences':return {preference:'zh-CN',locale:'zh-CN',revision:1};
 case 'Appearance':return {revision:1,selection:{character:'builtin:caelis',avatar:'follow'},model:'',avatar:'',basic:false,key:'builtin:caelis'};
 case 'Display':case 'DisplayState':return {scale:1};
 // Read-only presentation fixtures for the remaining common settings pages.
 // Unsupported mutations still fail below; these never touch host preferences.
 case 'DisplayPreferences':return {mode:'light',theme:'default',revision:1};
 case 'ContentState':return {appearance:{revision:1,selection:{character:'builtin:caelis',avatar:'follow'},model:'',avatar:'',basic:false,key:'builtin:caelis'},characters:[{id:'builtin:caelis',name:'Caelis',pack:'',group:''}],avatars:[],packs:[],notice:''};
 case 'Placement':return {scale:1};
 case 'LoginAtLoginStatus':return {supported:true,enabled:false,state:'off',registered:false};
 case 'TaskPreferences':return {revision:1,maxRunning:6,terminal:'system',customCommand:''};
 case 'TerminalChoices':return [{id:'system',name:'System',available:true}];
 case 'ShortcutSettings':case 'TaskShortcutSettings':return {shortcut:{enabled:true,key:method==='TaskShortcutSettings'?'KeyT':'Space',control:true,shift:true,alt:false,meta:false},registered:true,message:''};
 case 'TelegramStatus':return {enabled:false,bot:'',owner:'',paired:false,candidate:'',pairURL:'',issue:''};
 case 'WeixinStatus':return {phase:'unconfigured'};
 case 'Machines':return [];
 case 'UpdatePreferences':return {available:true,automatic:false,waiting:false};
 case 'ChatSnapshot':return {changed:true,snapshot};
 case 'Snapshot':case 'ComposerSnapshot':case 'SnapshotSummary':return snapshot;
 case 'Draft':return draft;
 case 'SaveDraft':draft={...draft,...args[0],revision:draft.revision+1};return draft;
 case 'DraftFiles':if(window.fixtureDraftFailure)throw Error('Synthetic attachment read unavailable');return files;
 case 'Submit':{
  if(!window.fixtureUnknownSend)throw Error('Synthetic send disabled');
  draft.pendingSend=true;snapshot.lastReceipt={id:args[0].id,outcome:'unknown',message:'Synthetic receipt pending'};
  return snapshot.lastReceipt;
 }
 case 'BotInitialization':return {required:false,status:'completed'};
 case 'NativePlugins':return [];
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
