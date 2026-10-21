// Isolated presentation/protocol fixture: no account, host filesystem or model.
import React,{useState} from 'react';
import {createRoot} from 'react-dom/client';
import {I18nProvider} from '../frontend/src/i18n';
import {installTransport} from '../frontend/src/transport';
import {ConnectionsPage} from '../frontend/src/settings/runtime/ConnectionsPage';
import {SettingsBreadcrumbs,SettingsToolbarContext,type SettingsCrumb} from '../frontend/src/settings/SettingsToolbar';
import {createPreviewClient} from '../frontend/src/settings/runtime/preview/client';
import '../frontend/src/application.css';
const base=await createPreviewClient().read(),profile={runtime:'caelis',cliPath:'',caelisStore:''};
const calls:Array<{method:string;id?:string;action?:string}>=[],waiters:Array<{resolve:(v:any)=>void;reject:(e:Error)=>void}>=[];
let offline=false,flow:any=null,starts=0,copied='',native:any={settings:{...profile,runtime:'codex'},state:'login',installation:{installed:true,version:'fixture-0.163.0',path:'',updateState:''},models:[],loginPending:false,selectedModel:''};
const catalogModels=Array.from({length:7},(_,i)=>({id:'openai-codex/model-'+i,name:i===0?'openai-codex/a-very-long-model-name-for-responsive-layout-verification-2026':'openai-codex/model-'+i,description:'native preset · reasoning · tools'}));
const resolveWaiters=(value=flow)=>waiters.splice(0).forEach(w=>w.resolve(structuredClone(value)));
const invoke:any=async(method:string,...args:any[])=>{
 calls.push({method,...(method==='WaitRuntimeConnection'?{id:args[0]}:{}),...(method==='AdvanceRuntimeConnection'?{id:args[0].id,action:args[0].action}:{})});
 switch(method){
 case 'CopyText':copied=args[0];return;
 case 'OpenMessageLink':return; // Never open a real issuer page.
 case 'SetupOverview':return {active:'caelis',hasRuntimeChoice:true,pending:'',onboarding:false};
 case 'ModelSelections':return [];
 case 'SetupProfile':return {...profile,runtime:args[0]};
 case 'RuntimeSettings':return profile;
 case 'InspectSetup':return structuredClone(args[0].runtime==='codex'?native:{...base.setup,installation:{...base.setup.installation,version:'fixture'},state:'ready'});
 case 'ExecutionSettings':return base.conversation;
 case 'LocalWorkerSelection':return {runtime:'conversation',work:{model:'',effort:'',serviceTier:''}};
 case 'ExecutionOptions':case 'Models':return base.models;
 case 'ProfileConfiguration':return {...base,oauthAvailable:true};
 case 'RuntimeDefaultModel':return base.main;
 case 'RuntimeExperiments':return {supported:false,features:[],revision:'',saveState:''};
 case 'RuntimeConnectionCatalog':return {choices:args[0]==='account'?[{id:'codex',name:'Codex',description:'ChatGPT'}]:[],unavailable:''};
 case 'StartRuntimeConnection':starts++;flow={id:'original-flow-'+starts,sequence:1,revision:'1',stage:'models',title:'选择账号模型',message:'选择模型后，通过提供方的浏览器页面登录。',models:catalogModels};return structuredClone(flow);
 case 'AdvanceRuntimeConnection':
  if(args[0].id!==flow.id)throw Error('Wrong original flow');
  if(args[0].action==='refresh')return structuredClone(flow);
  if(args[0].action!=='connect')throw Error('Unexpected fixture write');
  flow={...flow,sequence:flow.sequence+1,revision:String(flow.sequence+1),stage:'authorization',title:'完成浏览器授权',message:'登录信息由 Caelis 保存。完成后会自动继续。',authorization:{url:'https://example.invalid/device',userCode:'TEST-ONLY',expiresAt:new Date(Date.now()+600000).toISOString(),canSubmit:false}};resolveWaiters();return structuredClone(flow);
 case 'WaitRuntimeConnection':
  if(offline)throw Error('Synthetic read unavailable');
  if(args[0]!==flow.id)throw Error('Wrong observation owner');
  if(args[1]<flow.sequence)return structuredClone(flow);
  return new Promise((resolve,reject)=>waiters.push({resolve,reject}));
 case 'CancelRuntimeConnection':if(args[0]!==flow.id)throw Error('Wrong cancellation');flow={...flow,stage:'failed',reason:'cancelled',authorization:null,sequence:flow.sequence+1};resolveWaiters();return;
 case 'ApplySetup':if(args[0].action==='login'){native={...native,loginPending:true,login:{id:'native-login',state:'pending',userCode:'TEST-ONLY',verificationUrl:'https://auth.openai.com/codex/device',expiresAt:new Date(Date.now()+600000).toISOString()}};return native;}if(args[0].action==='cancel-login'){native={...native,loginPending:false,login:{id:'native-login',state:'cancelled'}};return native;}return native;
 default:throw Error('Unexpected fixture method '+method);
 }
};
installTransport(async(_service,method,args)=>invoke(method,...args));
(window as any).oauthFixture={
 snapshot:()=>({calls,starts,copied,id:flow?.id}),
 tick:()=>resolveWaiters(),
 offline:()=>{offline=true;waiters.splice(0).forEach(w=>w.reject(Error('Synthetic disconnect')))},
 online:()=>{offline=false},
 stale:()=>resolveWaiters({...flow,sequence:1,stage:'models',authorization:null}),
 terminal:(stage:string)=>{flow={...flow,sequence:flow.sequence+1,revision:String(flow.sequence+1),stage:stage==='expired'?'unknown':stage,reason:stage==='expired'?'expired':'',authorization:stage==='unknown'?flow.authorization:null};resolveWaiters()},
};
function Fixture(){const [trail,setTrail]=useState<SettingsCrumb[]>([]);return <div className="settings-window"><div className="settings-main"><p role="status" style={{margin:0,padding:8,textAlign:'center',fontSize:12}}>合成登录夹具 · 不访问真实账户</p><SettingsToolbarContext.Provider value={setTrail}><header className="settings-toolbar"><SettingsBreadcrumbs label="设置分类" items={[{label:'设置',open:()=>{}},...trail]}/></header><main className="settings-content"><ConnectionsPage/></main></SettingsToolbarContext.Provider></div></div>}
const bridge={read:async()=>({preference:'zh-CN' as const,locale:'zh-CN' as const,revision:1}),save:async()=>({preference:'zh-CN' as const,locale:'zh-CN' as const,revision:1}),subscribe:()=>()=>{}};
createRoot(document.getElementById('root')!).render(<I18nProvider bridge={bridge}><Fixture/></I18nProvider>);
