import type {RemoteManagementResult,RemoteRuntimeState,RemoteRuntimePending,RemoteRuntimeRequest} from '../../backend/contract';
import {ConfigurationError} from './client';

type Invoke=<T>(method:string,...args:unknown[])=>Promise<T>;
const reads=new Set(['RemoteRuntimeConfiguration','RemoteExecutionSettings','RemoteRuntimeStatus']);
const changes=new Set(['ChangeRemoteRuntimeConfiguration','ChangeRemoteExecutionSettings','ManageRemoteRuntime']);

// The paired product connection remains its own native management authority.
// Catalog enrollment never redirects these calls to a standalone node agent.
export function createPairedRuntimeInvoker(invoke:Invoke,binding:string,message:()=>string):Invoke {
 const unresolved=new Map<string,RemoteRuntimePending>(),terminal=new Set<string>();
 return async<T>(method:string,...args:unknown[]):Promise<T>=>{
  const failure=(outcome='rejected',id='')=>new ConfigurationError({operationId:id,outcome,message:message()});
  if(!binding)throw failure();
  if(reads.has(method)){
   if(args[0]!==binding)throw failure();
   const result=await invoke<T>(method,...args);
   if(method==='RemoteExecutionSettings'&&(result as {binding:string}).binding!==binding)throw failure();
   return result;
  }
  const state=await invoke<RemoteRuntimeState>('RemoteRuntime');
  if(state.binding!==binding)throw failure();
  const nativePending=(state.pending??[]).filter(item=>!terminal.has(item.id));
  if(method==='RemoteRuntime')return {...state,pending:[...nativePending,...[...unresolved.values()].filter(item=>!nativePending.some(native=>native.id===item.id))]} as T;
  if(!state.available)throw failure();
  const request=args[0] as {id?:string;binding?:string;action?:string};
  const recovery=method==='ReconcileRemoteManagement'||method==='ManageRemoteRuntime'&&request.action==='resolve';
  if(!changes.has(method)&&method!=='ReconcileRemoteManagement')throw failure();
  const id=method==='ReconcileRemoteManagement'?args[1] as string:request.id;
  if((method==='ReconcileRemoteManagement'?args[0]:request.binding)!==binding||!id)throw failure();
  if(!recovery&&(unresolved.size||nativePending.length))throw failure('unknown',unresolved.keys().next().value??nativePending[0]?.id);
  if(!recovery)unresolved.set(id,{id,kind:method==='ManageRemoteRuntime'?'runtime':method==='ChangeRemoteExecutionSettings'?'configure-execution':'configure-runtime',...(method==='ManageRemoteRuntime'?{runtime:args[0] as RemoteRuntimeRequest}:{})});
  let result:RemoteManagementResult;
  try{result=await invoke<RemoteManagementResult>(method,...args);}
  catch{throw failure('unknown',id);}
  const accepted=result.id===id&&result.outcome==='accepted'&&(method!=='ChangeRemoteRuntimeConfiguration'||result.configuration?.outcome==='committed');
  const rejected=result.id===id&&(result.outcome==='rejected'||method==='ChangeRemoteRuntimeConfiguration'&&['rejected','conflicted'].includes(result.configuration?.outcome??''));
  if(accepted||rejected){unresolved.delete(id);terminal.add(id);}
  if(result.id!==id)throw failure('unknown',id);
  return result as T;
 };
}
