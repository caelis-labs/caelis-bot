import { backend } from '../../desktop';
import type { NodeCatalog, NodeEditGuard, NodeManagementRequest, NodeOperationReceipt, NodeOperationRef, NodeRuntimeConfiguration, RuntimeConfigurationChange } from '../../backend/contract';
import { ConfigurationError } from './client';
import type { RuntimeSettingsClient } from './types';
import type {MessageKey} from '../../i18n/catalogs';
import {translator} from '../../i18n/core';

type Invoke = <T>(method: string, ...args: unknown[]) => Promise<T>;
export const nodeScopeKey = (nodeId:string, backend:string) => JSON.stringify([nodeId,backend]);
const sameRef = (a:NodeOperationRef,b:NodeOperationRef) => a.nodeId===b.nodeId && a.backend===b.backend && a.operationId===b.operationId && a.requestDigest===b.requestDigest;


export function managementDigestInput(guard:NodeEditGuard,payload:Pick<NodeManagementRequest,'change'|'installation'>):string {
 const change=payload.change,install=payload.installation;
 if(!!change===!!install)throw new Error('Exactly one node action is required.');
 const parts=['node-management-v1',guard.nodeId,guard.backend,guard.revision,change?'configuration':'installation'];
 if(change)parts.push(change.action,change.id,change.name,change.description,change.selection.model,change.selection.effort,change.selection.serviceTier,change.expectedRevision);
 if(install)parts.push(install.action,install.version,install.expectedVersion);
 return parts.map(value=>`${new TextEncoder().encode(value).length}:${value}`).join('');
}

// One owner survives selection changes. Uncertain delivery retains the original
// operation reference; refresh never authorizes a second mutation.
export function createNodeSettingsClient(invoke:Invoke=backend,text:(key:MessageKey)=>string=translator('en').t) {
 const pending=new Map<string,Map<string,NodeOperationRef>>();
 const terminal=new Set<string>();
 const refKey=(ref:NodeOperationRef)=>JSON.stringify([ref.nodeId,ref.backend,ref.operationId,ref.requestDigest]);
 const first=(key:string)=>pending.get(key)?.values().next().value;
 const working=new Set<string>();
 const listeners=new Set<()=>void>();
 const publish=()=>listeners.forEach(listener=>listener());
 const check=(receipt:NodeOperationReceipt,ref:NodeOperationRef) => {
  if(!sameRef(receipt.ref,ref))throw new ConfigurationError({operationId:ref.operationId,outcome:'unknown',message:text('settings.nodeOperationUnknown')});
  if(receipt.outcome!=='unknown'){const key=nodeScopeKey(ref.nodeId,ref.backend);pending.get(key)?.delete(ref.operationId);if(!pending.get(key)?.size)pending.delete(key);terminal.add(refKey(ref));publish();}
  if(receipt.outcome!=='committed')throw new ConfigurationError({operationId:ref.operationId,outcome:receipt.outcome,message:receipt.message});
  return receipt;
 };
 return {
  text,
  subscribe:(listener:()=>void)=>{listeners.add(listener);return()=>{listeners.delete(listener);};},
  async catalog(){const next=await invoke<NodeCatalog>('NodeCatalog');let changed=false;for(const ref of next.pendingOperations??[]){const key=nodeScopeKey(ref.nodeId,ref.backend);if(terminal.has(refKey(ref))||pending.get(key)?.has(ref.operationId))continue;const refs=pending.get(key)??new Map<string,NodeOperationRef>();refs.set(ref.operationId,ref);pending.set(key,refs);changed=true;}if(changed)publish();return next;},
  configuration:(guard:NodeEditGuard)=>invoke<NodeRuntimeConfiguration>('NodeRuntimeConfiguration',guard.nodeId,guard.backend),
  pending:(nodeId:string,runtime:string)=>first(nodeScopeKey(nodeId,runtime)),
  async change(guard:NodeEditGuard,payload:Pick<NodeManagementRequest,'change'|'installation'>) {
   const key=nodeScopeKey(guard.nodeId,guard.backend);
   if(working.has(key)||pending.has(key))throw new ConfigurationError({operationId:first(key)?.operationId??'',outcome:'unknown',message:text('settings.nodeOperationUnknown')});
   working.add(key);
   let ref:NodeOperationRef|undefined;
   try {
    const bytes=new TextEncoder().encode(managementDigestInput(guard,payload));
    const digest=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',bytes)),b=>b.toString(16).padStart(2,'0')).join('');
    ref={nodeId:guard.nodeId,backend:guard.backend,operationId:crypto.randomUUID(),requestDigest:digest};
    pending.set(key,new Map([[ref.operationId,ref]]));publish();
    const receipt=await invoke<NodeOperationReceipt>('ChangeNodeConfiguration',{guard,ref,...payload});
    return check(receipt,ref);
   } catch(e) {
    if(e instanceof ConfigurationError)throw e;
    throw new ConfigurationError({operationId:ref?.operationId??'',outcome:ref?'unknown':'rejected',message:e instanceof Error?e.message:text('settings.nodeOperationUnknown')});
   } finally {working.delete(key);}
  },
  async reconcile(nodeId:string,runtime:string) {
   const key=nodeScopeKey(nodeId,runtime),ref=first(key);
   if(!ref||working.has(key))return;
   working.add(key);
   try{return check(await invoke<NodeOperationReceipt>('ReconcileNodeOperation',ref),ref);}
   finally{working.delete(key);}
  },
 };
}
export type NodeSettingsClient=ReturnType<typeof createNodeSettingsClient>;

export function createNodeRuntimeClient(owner:NodeSettingsClient,guard:NodeEditGuard):RuntimeSettingsClient {
 const unavailable=async():Promise<never>=>{throw new Error(owner.text('settings.nodeRemotePreparation'));};
 const guards=new Map<string,NodeEditGuard>();
 const change=(fields:Partial<RuntimeConfigurationChange>,revision?:string)=>owner.change(guards.get(revision??'')??guard,{change:{action:'',id:'',name:'',description:'',selection:{model:'',effort:'',serviceTier:''},expectedRevision:revision??guard.revision,...fields},installation:null});
 return {
  capture:revision=>createNodeRuntimeClient(owner,guards.get(revision)??guard),
  async read(){
   const read=await owner.configuration(guard);
   if(read.guard.nodeId!==guard.nodeId||read.guard.backend!==guard.backend)throw new Error(owner.text('settings.nodeStateUnknown'));
   if(!read.configurationAvailable)throw new Error(owner.text('settings.nodeRemotePreparation'));
   const shared=read.configuration;
   guards.set(shared.revision,read.guard);guards.set(shared.team.revision,read.guard);
   const profile={runtime:guard.backend,cliPath:'',caelisStore:''};
   return {revision:shared.revision,profile,setup:{settings:profile,state:'ready',message:'',serviceUpdateAvailable:false,serviceVersion:'',serviceState:'',selectedModel:'',installation:{installed:false,path:'',version:'',latestVersion:'',updateState:'',message:''},models:[],loginPending:false,accountType:''},pending:'',models:shared.models,conversation:read.conversation,work:read.worker,main:shared.main,canEditMain:true,team:shared.team,connections:shared.connections.map(group=>({...group,kind:group.kind as 'provider'|'agent'}))};
  },
  async saveModel(scope,selection,revision){await change({action:scope==='conversation'?'conversation-model':scope==='work'?'worker-model':'main',selection:{model:selection.model,effort:selection.effort,serviceTier:selection.serviceTier}},revision);},
  async changeTeam(fields,revision){await change(fields,revision);},
  async removeModel(group,model,revision){if(model.uses.length||group.kind==='agent'&&group.models.some(m=>m.uses.length))return unavailable();await change({action:group.kind==='agent'?'disconnect-agent':'remove-model',id:group.kind==='agent'?group.id:model.id},revision);},
  catalog:unavailable,apiKeyOptions:unavailable,startConnection:unavailable,advanceConnection:unavailable,cancelConnection:unavailable,openURL:unavailable,
 };
}
