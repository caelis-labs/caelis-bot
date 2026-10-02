import type {RemoteExecutionView,RemoteManagementResult,RemoteRuntimeState} from '../../backend/contract';
import type {ModelSelection} from './types';
import {ConfigurationError} from './client';

type Invoke=<T>(method:string,...args:unknown[])=>Promise<T>;
export function createRemoteExecutionClient(invoke:Invoke,binding:string,message:string,changed:()=>void){
 const inspect=async()=>{
  const state=await invoke<RemoteRuntimeState>('RemoteRuntime');
  if(!state.available||state.binding!==binding||!state.capabilities.execution)throw new ConfigurationError({operationId:'',outcome:'rejected',message});
  return state;
 };
 return {
  async read(){
   await inspect();
   const view=await invoke<RemoteExecutionView>('RemoteExecutionSettings',binding);
   if(view.binding!==binding)throw new ConfigurationError({operationId:'',outcome:'rejected',message});
   return view;
  },
  async save(target:'conversation'|'work',selection:ModelSelection,revision:string){
   const state=await inspect();
   if(state.pending.length)throw new ConfigurationError({operationId:'',outcome:'unknown',message});
   let result:RemoteManagementResult;
   try{result=await invoke<RemoteManagementResult>('ChangeRemoteExecutionSettings',{id:crypto.randomUUID(),binding,target,expectedRevision:revision,selection:{model:selection.model,effort:selection.effort}});}
   catch{changed();throw new ConfigurationError({operationId:'',outcome:'unknown',message});}
   changed();
   if(result.outcome!=='accepted')throw new ConfigurationError({operationId:'',outcome:result.outcome==='rejected'?'rejected':'unknown',message});
  }
 };
}
