import type { NodeCatalog, NodeInfo } from '../../backend/contract';
import type { ConnectInput, ConnectionFlow, ModelScope, ModelSelection, RuntimeSettingsClient, RuntimeView, TeamChange } from './types';
import { ConfigurationError } from './client';
import { createNodeRuntimeClient, type NodeSettingsClient } from './nodeClient';
import { validSelection } from './state';

// This template deliberately has no API key, command, native profile ID or path.
export type ConnectionTemplate = Pick<ConnectInput, 'choice'|'baseUrl'|'model'|'contextWindowTokens'|'maxOutputTokens'|'imageInput'|'reasoningLevels'> & {kind:'api-key'};
export type BatchOptions = { conversation:boolean; work:boolean; main:boolean; team:boolean; connection:boolean; teamSet:string };
type PortableRole = {id:string;description:string;custom:boolean;system:boolean;selection:ModelSelection|null};
export type BatchTemplate = {backend:'codex'|'caelis';models:{scope:ModelScope;selection:ModelSelection}[];roles:PortableRole[]|null;connection?:ConnectionTemplate;teamSet:string};
export type BatchReason = 'authentication'|'unavailable'|'model'|'mapping'|'team'|'connection'|'pending'|'failed'|'unknown';
export class BatchError extends Error {constructor(readonly reason:BatchReason){super(reason);}}
export type BatchResult = {nodeId:string;label:string;outcome:'applying'|'committed'|'needs-initialization'|'failed'|'partial'|'unknown';reason?:BatchReason;applied:string[]};

export function publicConnectionTemplate(input:ConnectInput):ConnectionTemplate|undefined {
 if(input.kind!=='api-key'||!input.choice||!input.model?.trim()||!input.baseUrl)return;
 try {
  const url=new URL(input.baseUrl);
  // Even an innocuous query may contain a secret. Do not try to redact or guess.
  if(!['https:','http:'].includes(url.protocol)||url.username||url.password||url.search||url.hash)return;
 }catch{return;}
 return {kind:'api-key',choice:input.choice,baseUrl:input.baseUrl,model:input.model,
  ...(input.contextWindowTokens!==undefined?{contextWindowTokens:input.contextWindowTokens}:{}),
  ...(input.maxOutputTokens!==undefined?{maxOutputTokens:input.maxOutputTokens}:{}),
  ...(input.imageInput!==undefined?{imageInput:input.imageInput}:{}),
  ...(input.reasoningLevels?{reasoningLevels:[...input.reasoningLevels]}:{})};
}
const selectionOnly=(value:ModelSelection):ModelSelection=>({model:value.model,effort:value.effort,serviceTier:value.serviceTier});
export function makeBatchTemplate(view:RuntimeView,options:BatchOptions,connection?:ConnectionTemplate):BatchTemplate {
 const backend=view.profile.runtime;
 if(backend!=='codex'&&backend!=='caelis')throw new BatchError('unavailable');
 const models:BatchTemplate['models']=[];
 for(const [enabled,scope,value] of [[options.conversation&&backend==='codex','conversation',view.conversation],[options.work&&backend==='codex','work',view.work],[options.main&&backend==='caelis','runtime',view.main]] as const){
  if(!enabled)continue;
  if(!value)throw new BatchError('model');
  models.push({scope,selection:selectionOnly(value)});
 }
 let roles:PortableRole[]|null=null;
 if(options.team&&backend==='caelis'){
  if(!view.team.available)throw new BatchError('team');
  roles=view.team.roles.filter(role=>role.id!=='self').map(role=>{
   let selection:ModelSelection|null=null;
   if(!role.inherited){
    const selectors=[...new Set(view.team.modelBindings?.filter(binding=>binding.profileId===role.selection.model).map(binding=>binding.selector)??[])];
    if(selectors.length!==1||!selectors[0])throw new BatchError('mapping');
    selection={...selectionOnly(role.selection),model:selectors[0]};
   }
   return {id:role.id,description:role.description,custom:role.custom,system:role.system,selection};
  });
 }
 const shared=options.connection&&backend==='caelis'&&connection?publicConnectionTemplate(connection):undefined;
 if(options.connection&&!shared)throw new BatchError('connection');
 if(!models.length&&!roles&&!shared)throw new BatchError('unavailable');
 return {backend,models,roles,connection:shared,teamSet:roles?options.teamSet.trim():''};
}
function targetRoleSelection(role:PortableRole,view:RuntimeView):ModelSelection|null {
 if(!role.selection)return null;
 const profiles=[...new Set(view.team.modelBindings?.filter(binding=>binding.selector===role.selection!.model).map(binding=>binding.profileId)??[])];
 if(profiles.length!==1)throw new BatchError('mapping');
 const selection={...role.selection,model:profiles[0]};
 if(!validSelection(selection,view.team.models))throw new BatchError('model');
 return selection;
}
function validateRole(role:PortableRole,view:RuntimeView) {
 const current=view.team.roles.find(value=>value.id===role.id);
 if(!current){if(!role.custom)throw new BatchError('team');}
 else if(current.custom!==role.custom||current.system!==role.system||role.custom&&current.description!==role.description)throw new BatchError('team');
 const selection=targetRoleSelection(role,view);
 if(selection&&current&&!current.modelIds?.includes(selection.model))throw new BatchError('team');
 return {current,selection};
}
export function validateBatchTarget(template:BatchTemplate,view:RuntimeView) {
 if(view.profile.runtime!==template.backend)throw new BatchError('unavailable');
 for(const item of template.models){
  const current=item.scope==='conversation'?view.conversation:item.scope==='work'?view.work:view.main;
  if(!current||item.scope==='runtime'&&!view.canEditMain||!validSelection(item.selection,view.models,item.scope!=='runtime'))throw new BatchError('model');
 }
 if(template.roles){
  if(!view.team.available)throw new BatchError('team');
  for(const role of template.roles)validateRole(role,view);
 }
}
async function applyConnection(client:RuntimeSettingsClient,template:ConnectionTemplate,onApplied:()=>void) {
 const session=await client.beginConnection?.()??client;
 const abort=new AbortController();
 let flow:ConnectionFlow|undefined;
 try {
  const catalog=await session.catalog('api-key');
  if(!catalog.choices.some(choice=>choice.id===template.choice&&!choice.custom))throw new BatchError('connection');
  const options=await session.apiKeyOptions(template.choice,template.baseUrl!);
  if(!options.endpoints.some(endpoint=>endpoint.value===template.baseUrl&&endpoint.reuseAuth))throw new BatchError('authentication');
  let settle:(flow:ConnectionFlow)=>void=()=>{};
  const terminal=new Promise<ConnectionFlow>(resolve=>{settle=resolve;});
  const observe=(next:ConnectionFlow)=>{
   flow=next;
   if(['complete','failed','unknown','authorization','auth-method','installation','launcher','models'].includes(next.stage))settle(next);
  };
  const timer=setTimeout(()=>settle({id:flow?.id??'',revision:flow?.revision??'',sequence:flow?.sequence??0,stage:'unknown',title:'',message:''}),60_000);
  try {observe(await session.startConnection(template,abort.signal,observe));flow=await terminal;}
  finally{clearTimeout(timer);abort.abort();}
  if(flow.stage==='complete')onApplied();
  else if(flow.stage==='unknown')throw new BatchError('unknown');
  else if(flow.stage==='failed')throw new BatchError('connection');
  else {await session.cancelConnection(flow);throw new BatchError('authentication');}
 } finally {
  abort.abort();
  try{await session.closeConnection?.();}catch{throw new BatchError('unknown');}
 }
}

// Sequential, independent targets; each write rereads that target's own revision.
// Unknown delivery stops this target and retains the existing client's receipt.
// No mutation retries, global rollback, activation or machine settings writes.
export async function applyBatchTemplate(owner:NodeSettingsClient,template:BatchTemplate,targets:Pick<NodeInfo,'id'|'label'>[],report:(result:BatchResult)=>void) {
 for(const target of targets){
  const result:BatchResult={nodeId:target.id,label:target.label,outcome:'applying',applied:[]};
  const publish=()=>report({...result,applied:[...result.applied]});
  publish();
  let mutationAttempted=false;
  try {
   const catalog:NodeCatalog=await owner.catalog();
   const node=catalog.nodes.find(value=>value.id===target.id),status=node?.runtimes.find(value=>value.backend===template.backend);
   if(!node||!status||status.health==='missing'||status.health==='unknown')throw new BatchError('unavailable');
   if(owner.pending(node.id,template.backend))throw new BatchError('pending');
   if(!template.connection&&status.authentication!=='authenticated')throw new BatchError('authentication');
   const client=createNodeRuntimeClient(owner,{nodeId:node.id,backend:template.backend,revision:catalog.revision});
   let view=await client.read();
   if(template.connection){mutationAttempted=true;await applyConnection(client,template.connection,()=>{result.applied.push('connection');publish();});mutationAttempted=false;}
   if(template.connection)view=await client.read();
   validateBatchTarget(template,view);
   for(const item of template.models){
    view=await client.read();
    validateBatchTarget({...template,models:[item],roles:null},view);
    mutationAttempted=true;
    await (client.capture?.(view.revision)??client).saveModel(item.scope,item.selection,view.revision);
    mutationAttempted=false;
    result.applied.push(item.scope);publish();
   }
   for(const role of template.roles??[]){
    view=await client.read();
    let mapped=validateRole(role,view);
    if(!mapped.current){
     mutationAttempted=true;
     await (client.capture?.(view.team.revision)??client).changeTeam({action:'create-role',id:role.id,description:role.description},view.team.revision);
     mutationAttempted=false;
     result.applied.push(`create:${role.id}`);publish();
     view=await client.read();mapped=validateRole(role,view);
     if(!mapped.current)throw new BatchError('team');
    }
    const change:TeamChange=mapped.selection?{action:'bind',id:role.id,selection:mapped.selection}:{action:'reset',id:role.id};
    mutationAttempted=true;
    await (client.capture?.(view.team.revision)??client).changeTeam(change,view.team.revision);
    mutationAttempted=false;
    result.applied.push(`role:${role.id}`);publish();
   }
   if(template.teamSet){
    view=await client.read();
    mutationAttempted=true;
    await (client.capture?.(view.team.revision)??client).changeTeam({action:'save-set',name:template.teamSet},view.team.revision);
    mutationAttempted=false;
    result.applied.push('teamSet');
   }
   result.outcome='committed';
  }catch(error){
   result.reason=error instanceof BatchError?error.reason:error instanceof ConfigurationError?(error.unknown?'unknown':'failed'):mutationAttempted?'unknown':'failed';
   result.outcome=result.reason==='unknown'||result.reason==='pending'?'unknown':result.applied.length?'partial':['authentication','mapping','model','team','connection'].includes(result.reason)?'needs-initialization':'failed';
  }
  publish();
 }
}
