import type { RemoteManagementResult, RemoteRuntimeState, RuntimeConfiguration, RuntimeConfigurationChange, WorkExecutionSettings } from '../../backend/contract';
import type { RuntimeSettingsClient } from './types';
import { ConfigurationError } from './client';

type Invoke = <T>(method: string, ...args: unknown[]) => Promise<T>;
const empty: WorkExecutionSettings = {model:'',effort:'',serviceTier:''};

export function createRemoteRuntimeSettingsClient(invoke:Invoke,binding:string,message:string,changed:()=>void): RuntimeSettingsClient {
 const unavailable=async():Promise<never>=>{throw new Error(message);};
 const change=async(fields:Partial<RuntimeConfigurationChange>)=>{
  const state=await invoke<RemoteRuntimeState>('RemoteRuntime');
  if(!state.available || state.binding!==binding || state.pending.length)throw new ConfigurationError({operationId:'',outcome:state.pending.length?'unknown':'rejected',message});
  let result:RemoteManagementResult;
  try {result=await invoke<RemoteManagementResult>('ChangeRemoteRuntimeConfiguration',{id:crypto.randomUUID(),binding,change:{action:'',id:'',name:'',description:'',selection:empty,expectedRevision:'',...fields}});}
  catch {changed();throw new ConfigurationError({operationId:'',outcome:'unknown',message});}
  changed();
  if(result.outcome!=='accepted'||result.configuration?.outcome!=='committed')throw new ConfigurationError(result.configuration??{operationId:'',outcome:result.outcome==='rejected'?'rejected':'unknown',message});
 };
 return {
  async read(){
   const state=await invoke<RemoteRuntimeState>('RemoteRuntime');
   if(!state.available||state.binding!==binding||!state.capabilities.configuration)throw new Error(message);
   const shared=await invoke<RuntimeConfiguration>('RemoteRuntimeConfiguration',binding);
   const profile={runtime:'caelis',cliPath:'',caelisStore:''};
   return {revision:shared.revision,profile,setup:{settings:profile,state:'ready',message:'',serviceUpdateAvailable:false,serviceVersion:'',serviceState:'',selectedModel:'',installation:{installed:false,path:'',version:'',latestVersion:'',updateState:'',message:''},models:[],loginPending:false,accountType:''},pending:'',models:shared.models,conversation:null,work:null,main:shared.main,canEditMain:true,team:shared.team,connections:shared.connections.map(group=>({...group,kind:group.kind as 'provider'|'agent'}))};
  },
  async saveModel(scope,selection,revision){if(scope!=='runtime')return unavailable();await change({action:'main',selection,expectedRevision:revision});},
  async changeTeam(fields,revision){await change({...fields,expectedRevision:revision});},
  async removeModel(group,model,revision){if(model.uses.length||group.kind==='agent'&&group.models.some(model=>model.uses.length))return unavailable();await change({action:group.kind==='agent'?'disconnect-agent':'remove-model',id:group.kind==='agent'?group.id:model.id,expectedRevision:revision});},
  catalog:unavailable,apiKeyOptions:unavailable,startConnection:unavailable,advanceConnection:unavailable,cancelConnection:unavailable,openURL:unavailable,
 };
}
