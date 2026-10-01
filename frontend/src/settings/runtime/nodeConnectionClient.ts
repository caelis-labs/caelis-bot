import type {NodeEditGuard, NodeRuntimeConnectionRef, RuntimeFlow, SetupChoice} from '../../backend/contract';
import type {MessageKey} from '../../i18n/catalogs';
import type {ConnectionFlow, RuntimeSettingsClient} from './types';
import {ConfigurationError} from './client';
import {safeWebURL} from './state';

type Invoke = <T>(method:string,...args:unknown[])=>Promise<T>;
// Native contract; the operation identity never appears in the settings UI.
type ConnectionRef = NodeRuntimeConnectionRef;
type ConnectionPort = Pick<RuntimeSettingsClient,'catalog'|'apiKeyOptions'|'startConnection'|'advanceConnection'|'cancelConnection'|'openURL'> & {begin():Promise<void>;close():Promise<void>};
const project = (flow:RuntimeFlow):ConnectionFlow => ({...flow,stage:flow.stage as ConnectionFlow['stage'],installation:flow.installation??undefined,authorization:flow.authorization??undefined});

// Keep original ownership across rerenders and response loss. Only confirmed
// native cleanup releases a scope for a later explicit setup action.
export function createNodeConnectionOwner(invoke:Invoke,text:(key:MessageKey)=>string) {
 const sessions=new Map<string,ConnectionPort>();
 return (guard:NodeEditGuard):ConnectionPort=>{
  const key=JSON.stringify([guard.nodeId,guard.backend]);
  const existing=sessions.get(key);if(existing)return existing;
  const captured={...guard},ref:ConnectionRef={nodeId:guard.nodeId,backend:guard.backend,operationId:crypto.randomUUID()};
  const unknown=()=>new ConfigurationError({operationId:ref.operationId,outcome:'unknown',message:text('settings.nodeOperationUnknown')});
  let closed=false,rejected=false,observing=0;
  let beginning:Promise<void>|undefined,closing:Promise<void>|undefined,start:Promise<RuntimeFlow>|undefined;
  const actions=new Map<string,Promise<RuntimeFlow>>(),cancels=new Map<string,Promise<void>>();
  const requireOpen=()=>{if(closed||!beginning)throw unknown();};
  const call=async<T>(method:string,...args:unknown[]):Promise<T>=>{
   requireOpen();await beginning;requireOpen();
   try{return await invoke<T>(method,ref,...args);}catch{throw unknown();}
  };
  const observe=(initial:RuntimeFlow,signal:AbortSignal,progress:(flow:ConnectionFlow)=>void)=>{
   const epoch=++observing;let current=initial;
   void (async()=>{
    while(!closed&&!signal.aborted&&epoch===observing&&!['complete','failed','unknown'].includes(current.stage)) {
     try {
      const next=await call<RuntimeFlow>('WaitNodeRuntimeConnection',current.id,current.sequence);
      if(closed||signal.aborted||epoch!==observing)return;
      if(next.id!==current.id)throw unknown();
      if(next.sequence>=current.sequence){current=next;progress(project(next));}
     } catch {
      if(!closed&&!signal.aborted&&epoch===observing)progress({...project(current),stage:'unknown',title:text('connections.unknownResultTitle'),message:text('connections.unknownResultMessage')});
      return;
     }
    }
   })();
   return project(initial);
  };
  const session:ConnectionPort={
   begin(){
    if(closed)return Promise.reject(unknown());
    beginning??=(async()=>{
     try {
      const actual=await invoke<ConnectionRef>('BeginNodeRuntimeConnection',captured,ref.operationId);
      if(actual.nodeId!==ref.nodeId||actual.backend!==ref.backend||actual.operationId!==ref.operationId)throw unknown();
     }catch(error){
      // Wails serializes Error() only. Recognize this single fixed native code,
      // which is emitted solely after a durable proof of no setup dispatch.
      const message=error instanceof Error?error.message:error;
      if(message==='node connection begin-rejected'){
       rejected=true;closed=true;
       if(sessions.get(key)===session)sessions.delete(key);
       throw new ConfigurationError({operationId:ref.operationId,outcome:'rejected',message:text('settings.nodeConnectionSetupUnavailable')});
      }
      throw unknown();
     }
    })();
    return beginning;
   },
   close(){
    if(rejected)return Promise.resolve();
    closed=true;observing++;
    if(closing)return closing;
    const attempt=(async()=>{
     // A late or lost Begin still belongs to this original operation. Cleanup
     // checks always use that reference, including after a lost Close reply.
     await beginning?.catch(()=>{});
     try{await invoke('CloseNodeRuntimeConnection',ref);}catch{throw unknown();}
     if(sessions.get(key)===session)sessions.delete(key);
    })();
    closing=attempt;
    void attempt.catch(()=>{if(closing===attempt)closing=undefined;});
    return attempt;
   },
   catalog:kind=>call('NodeRuntimeConnectionCatalog',kind),
   async apiKeyOptions(provider,baseURL){
    const endpoints=await call<SetupChoice[]>('NodeRuntimeSetupCatalog','endpoints',provider,'');
    const models=await call<SetupChoice[]>('NodeRuntimeSetupCatalog','models',provider,baseURL||endpoints[0]?.value||'');
    return {endpoints:endpoints.map(e=>({value:e.value,name:e.label||e.value,reuseAuth:e.noAuth})),models:models.map(m=>({value:m.value,name:m.label||m.value}))};
   },
   async startConnection(input,signal,progress){
    signal.throwIfAborted();
    // Password drafts are arguments only. Retain the result, never the input.
    start??=call<RuntimeFlow>('StartNodeRuntimeConnection',{...input,settings:null});
    const result=await start;signal.throwIfAborted();requireOpen();return observe(result,signal,progress);
   },
   async advanceConnection(flow,action,input,signal,progress){
    signal.throwIfAborted();
    const key=JSON.stringify([flow.id,flow.revision,action]);
    let pending=actions.get(key);
    if(!pending){pending=call<RuntimeFlow>('AdvanceNodeRuntimeConnection',{id:flow.id,revision:flow.revision,action,input});actions.set(key,pending);}
    const next=await pending;signal.throwIfAborted();requireOpen();
    if(next.id!==flow.id)throw unknown();
    return observe(next,signal,progress);
   },
   cancelConnection(flow){
    let pending=cancels.get(flow.id);
    if(!pending){pending=call<void>('CancelNodeRuntimeConnection',flow.id);cancels.set(flow.id,pending);}
    return pending;
   },
   async openURL(url){
    requireOpen();if(!safeWebURL(url))throw new Error(text('connections.invalidUrl'));
    try{await invoke('OpenMessageLink',url);}catch{throw new Error(text('connections.cannotOpenBrowser'));}
   },
  };
  sessions.set(key,session);return session;
 };
}
