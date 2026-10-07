import type { backend } from '../../../desktop';
import type { RuntimeSettings, SetupRequest, SetupState } from '../../../backend/contract';

// Development-only transport for the actual installation component.
export function createPreparationPreview(): typeof backend {
 const params=new URLSearchParams(location.search),scenario=params.get('state')??'',mode=params.get('upgrade');
 let installed=scenario.includes('install')?'':['available','failure'].includes(mode??'')||scenario.includes('update')?'v0.61.0':'v0.62.0', service=scenario.includes('install')?'':scenario.startsWith('runtime-')?installed:'v0.61.0';
 let updateState='',latest='', failed=false;
 const profile:RuntimeSettings={runtime:'caelis',cliPath:'',caelisStore:''};
 const state=():SetupState=>({serviceUpdateAvailable:!!installed&&installed>service,settings:profile,selectedModel:'fixture/model',serviceVersion:service,serviceState:installed?'running':'stopped',installation:{installed:!!installed,path:installed?'/Users/demo/.local/bin/caelis':'',version:installed,message:'',latestVersion:latest,updateState},state:service==='v0.62.0'?'ready':installed?'incompatible':'install',message:'',models:[{value:'fixture/model',label:'MiMo V2.6 Flash',noAuth:false,current:true}],loginPending:false,accountType:''});
 return async<T>(method:string,...args:unknown[]):Promise<T>=>{
  let result:unknown;
  switch(method){
   case 'RuntimeSettings':case 'SetupProfile':result=profile;break;
   case 'SetupOverview':result={active:scenario.includes('unselected')?'':'caelis',pending:'',onboarding:false};break;
   case 'ComposerSnapshot':result={connection:scenario.match(/(?:offline|connecting|unknown|ready)$/)?.[0]??'ready'};break;
   case 'InspectSetup':updateState='';latest='';result=state();break;
   case 'ApplySetup':{
    const request=args[0] as SetupRequest;
    await new Promise(resolve=>setTimeout(resolve,350));
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
