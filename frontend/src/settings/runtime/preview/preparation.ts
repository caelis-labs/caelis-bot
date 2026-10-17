import type { backend } from '../../../desktop';
import type { RuntimeSettings, SetupRequest, SetupState } from '../../../backend/contract';
import { createPreviewClient } from './client';
import type { ConnectionKind } from '../types';

// Development-only transport for the actual installation component.
export function createPreparationPreview(): typeof backend {
 const params=new URLSearchParams(location.search),scenario=params.get('state')??'',mode=params.get('upgrade');
 const codex=scenario.includes('codex');
 let authenticated=!codex||!/(install|login)/.test(scenario),loginPending=false,loginReads=0;
 let installed=scenario.includes('install')?'':['available','failure'].includes(mode??'')||scenario.includes('update')?'v0.61.0':'v0.62.0', service=scenario.includes('install')?'':scenario.startsWith('runtime-')?installed:'v0.61.0';
 let updateState='',latest='', failed=false;
 let selectedModel='fixture/model';
 let models:SetupState['models']=scenario.includes('onboarding-new')||scenario.includes('empty')?[]:codex?[{value:'fixture/model',label:'GPT Codex Standard',noAuth:false,current:true},{value:'fixture/other',label:'GPT Codex Fast',noAuth:false,current:false}]:[{value:'fixture/model',label:'MiMo V2.6 Flash',noAuth:false,current:true},{value:'fixture/other',label:'DeepSeek Flash',noAuth:false,current:false}];
 const profile:RuntimeSettings={runtime:codex?'codex':'caelis',cliPath:'',caelisStore:''};
 const client=createPreviewClient();
 const state=():SetupState=>({serviceUpdateAvailable:!codex&&!!installed&&installed>service,settings:profile,selectedModel,serviceVersion:service,serviceState:installed?'running':'stopped',installation:{installed:!!installed,path:installed?`/Users/demo/.local/bin/${profile.runtime}`:'',version:installed,message:'',latestVersion:latest,updateState},state:codex?(!installed?'missing':authenticated?'ready':'login'):service==='v0.62.0'?(models.length?'ready':'models'):installed?'incompatible':'install',message:'',models:authenticated?models:[],loginPending,accountType:codex&&authenticated?'chatgpt':''});
 return async<T>(method:string,...args:unknown[]):Promise<T>=>{
  let result:unknown;
  switch(method){
   case 'RuntimeSettings':case 'SetupProfile':result=profile;break;
   case 'ActivateRuntime':await new Promise(resolve=>setTimeout(resolve,scenario.includes('slow')?4000:350));result=null;break;
   case 'ExecutionSettings':result={model:selectedModel,effort:'',serviceTier:'',approvalMode:'default'};break;
   case 'SaveExecutionSettings':result=null;break;
   case 'Models':result=models.map(m=>({model:m.value,name:m.label,efforts:[],serviceTiers:[]}));break;
   case 'StartRuntimeConnection':{
    const input=args[0] as {model:string};await new Promise(resolve=>setTimeout(resolve,scenario.includes('slow')?4000:350));
    selectedModel=input.model;models=[{value:input.model,label:input.model,noAuth:false,current:true}];
    result={id:'fixture-connection',revision:'1',sequence:1,stage:'complete',title:'',message:''};break;
   }
   case 'SetupOverview':result={active:profile.runtime,hasRuntimeChoice:!scenario.includes('unselected'),pending:'',onboarding:false};break;
   case 'ComposerSnapshot':result={connection:scenario.match(/(?:offline|connecting|unknown|ready)$/)?.[0]??'ready'};break;
   case 'RuntimeConnectionCatalog':result=await client.catalog(args[0] as ConnectionKind);break;
   case 'SetupCatalog':{
    const request=args[0] as SetupRequest;
    if(request.action==='model-parameters'){result=[];break;}
    const options=await client.apiKeyOptions(request.provider,request.baseUrl);
    result=(request.action==='endpoints'?options.endpoints:options.models).map(v=>({value:v.value,label:v.name,noAuth:request.action==='endpoints'&&scenario.includes('reuse-auth')}));break;
   }
   case 'InspectSetup':if(scenario.includes('slow'))await new Promise(resolve=>setTimeout(resolve,2500));if(loginPending&&++loginReads>=2){authenticated=true;loginPending=false;}updateState='';latest='';result=state();break;
   case 'ApplySetup':{
    const request=args[0] as SetupRequest;
    await new Promise(resolve=>setTimeout(resolve,350));
    if(request.action==='use-model')selectedModel=request.model;
    if(request.action==='prepare'&&!codex&&installed)service=installed;
    if(request.action==='login'){loginPending=true;loginReads=0;}
    if(request.action==='cancel-login'){loginPending=false;loginReads=0;}
    if(request.action==='check-update'){latest='v0.62.0';updateState=installed===latest?'current':'available';}
    else if(['install','update','apply-update','start'].includes(request.action)){
     installed='v0.62.0';
     if(mode==='failure'&&!failed){failed=true;throw new Error('服务尚未启用，请重试。');}
     service=installed;updateState='';
    }
    result=state();break;
   }
   default:throw new Error(`Preview does not implement ${method}`);
  }
  return result as T;
 };
}
