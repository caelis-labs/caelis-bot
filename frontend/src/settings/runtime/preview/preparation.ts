import type { backend } from '../../../desktop';
import type { RuntimeSettings, SetupRequest, SetupState } from '../../../backend/contract';

// Development-only transport for the actual installation component.
export function createPreparationPreview(): typeof backend {
 const mode=new URLSearchParams(location.search).get('upgrade');
 let installed=['available','failure'].includes(mode??'')?'v0.61.0':'v0.62.0', service='v0.61.0';
 let updateState='',latest='', failed=false;
 const profile:RuntimeSettings={runtime:'caelis',cliPath:'',caelisStore:''};
 const state=():SetupState=>({serviceUpdateAvailable:installed>service,settings:profile,selectedModel:'fixture/model',serviceVersion:service,serviceState:'running',installation:{installed:true,path:'/Users/demo/.local/bin/caelis',version:installed,message:'',latestVersion:latest,updateState},state:service==='v0.62.0'?'ready':'incompatible',message:service==='v0.62.0'?'Caelis 服务已就绪，连接验证通过':'运行中的服务尚未支持当前 Bot 协议',models:[{value:'fixture/model',label:'MiMo V2.6 Flash',noAuth:false,current:true}],loginPending:false,accountType:''});
 return async<T>(method:string,...args:unknown[]):Promise<T>=>{
  let result:unknown;
  switch(method){
   case 'RuntimeSettings':case 'SetupProfile':result=profile;break;
   case 'SetupOverview':result={active:'caelis',pending:'',onboarding:false};break;
   case 'ComposerSnapshot':result={connection:'ready'};break;
   case 'InspectSetup':updateState='';latest='';result=state();break;
   case 'ApplySetup':{
    const request=args[0] as SetupRequest;
    await new Promise(resolve=>setTimeout(resolve,350));
    if(request.action==='check-update'){latest='v0.62.0';updateState=installed===latest?'current':'available';}
    else if(['update','apply-update','start'].includes(request.action)){
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
