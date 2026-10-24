// Opaque IDs only persist. Arguments (including credentials) stay in memory.
// Every subsequent invocation first reads its original receipt; a changed body
// must never inherit that receipt's value or silently dispatch a second effect.
const rejectionDetails=new Set(['pairing_code_invalid','pairing_code_expired','peer_authorization_revoked','peer_authentication_required','original_peer_proof_required','worker_sharing_disabled','worker_not_ready','bot_worker_contract_incompatible','bot_peer_identity_changed','invalid_bot_address','worker_listener_invalid','worker_reachable_address_required','worker_listener_managed_headless','invalid_bot_name','machine_connection_type_conflict','new_pairing_code_required','original_pairing_code_unavailable','original_bot_authorization_changed']);
export function safeWebRejectionDetail(value:unknown){return typeof value==='string'&&rejectionDetails.has(value)?value:undefined;}
type BotRecovery={operationId:string;profileId:string;canAuthorize:boolean};
type WebOperation={state:string;error?:string;detail?:string;value?:unknown;recovery?:BotRecovery;followingAuthorization?:string;authorizationConfirmed?:boolean};
const connectPath='/api/v1/machines/connect-bot',authorizePath='/api/v1/machines/authorize-bot';
const rejectionDetail=(op:WebOperation)=>op.error==='operation_rejected'?safeWebRejectionDetail(op.detail):undefined;
export function createWebOperations(io:{storage:Storage;identity:()=>string;post:(path:string,body:unknown,timeout:number)=>Promise<{value:unknown}>;receipt:(id:string)=>Promise<WebOperation>;failure:(code:string,detail?:string)=>Error}){
 const key='caelisbot.pending-operations';
 let pending:Record<string,string>={};
 try{const saved=JSON.parse(io.storage.getItem(key)||'{}');if(saved&&typeof saved==='object'&&!Array.isArray(saved))pending=Object.fromEntries(Object.entries(saved).filter((e):e is [string,string]=>typeof e[1]==='string'))}catch{/* No saved identities. */}
 const bodies=new Map<string,string>(),active=new Set<string>();
 const remember=()=>io.storage.setItem(key,JSON.stringify(pending));
 const finish=(path:string)=>{const id=pending[path];for(const [key,value] of Object.entries(pending)){if(value===id&&!key.startsWith('authorization:')){delete pending[key];bodies.delete(key)}}remember()};
 const terminal=(op:WebOperation)=>{
  const value=op.value as {outcome?:string;state?:string}|null;
  return op.state==='complete'&&!op.error?.startsWith('operation_unconfirmed')&&value?.outcome!=='unknown'&&value?.state!=='unknown';
 };
 const uncertain=(op?:WebOperation)=>Object.assign(io.failure('operation_unconfirmed'),op?.recovery?{botRecovery:op.recovery}:{});
 const connectionScope=(id='')=>connectPath+(id?'@'+id:'');
 const markAuthorization=(id:string,profile='')=>{pending['authorization:'+connectionScope(profile)]=id;remember()};
 const settle=async(path:string,id:string,same:boolean)=>{
  const op=await io.receipt(id);
  if(!terminal(op))throw uncertain(op);
  finish(path);
  if(!same)throw io.failure('previous_operation_resolved');
  if(op.error)throw io.failure(op.error,rejectionDetail(op));
  return op.value;
 };
 return async(path:string,args:unknown[],read:boolean)=>{
  if(read)return (await io.post(path,{id:io.identity(),args},8000)).value;
  const argument=args[0] as {id?:string;connection?:{id?:string}}|undefined;
  const profile=path===authorizePath?argument?.connection?.id||'':argument?.id||'';
  const connection=connectionScope(profile);
  let scope=path,afterAuthorization='';
  if(path==='/api/v1/conversation/decide'&&profile)scope=path+'@'+profile;
  if(['/api/v1/conversation/answer-question','/api/v1/conversation/dismiss-question'].includes(path)&&typeof args[0]==='string')scope=path+'@'+args[0];
  if(['/api/v1/machines/recover-bot-work','/api/v1/machines/remove-machine'].includes(path)&&typeof args[0]==='string')scope=path+'@'+args[0];
  if(path===authorizePath){
   const request=args[0] as {originalOperationId?:string;connection?:{id?:string}};
   scope=path+'@'+(request.originalOperationId||request.connection?.id||'');
  }
  if(path===connectPath){
   const remembered=pending['authorization:'+connection];
   if(remembered){const op=await io.receipt(remembered);if(!op.authorizationConfirmed)throw uncertain(op);afterAuthorization=remembered;}
   for(let depth=0;depth<16;depth++){
    scope=afterAuthorization?connection+'@'+afterAuthorization:connection;
    const prior=pending[scope];if(!prior)break;
    const op=await io.receipt(prior);
    if(!op.followingAuthorization)break;
    if(op.followingAuthorization===afterAuthorization)throw uncertain(op);
    afterAuthorization=op.followingAuthorization;markAuthorization(afterAuthorization,profile);
    if(depth===15)throw uncertain(op);
   }
  }
  if(active.has(scope))throw io.failure('operation_unconfirmed');
  active.add(scope);
  try{
   const prior=pending[scope],body=JSON.stringify(args);
   if(prior){
    const value=await settle(scope,prior,bodies.get(scope)===body);
    if(path===authorizePath)markAuthorization(prior,profile);
    return value;
   }
   const id=io.identity();pending[scope]=id;bodies.set(scope,body);remember();
   try{
    const result=await io.post(path,{id,args,...(afterAuthorization?{afterAuthorization}:{})},120000);
    if(terminal({state:'complete',value:result.value})){finish(scope);if(path===authorizePath)markAuthorization(id,profile);}
    return result.value;
   }catch(error){
    const code=(error as {code?:string})?.code;
    if(code&&['composer_owned_elsewhere','request_id_required','invalid_request','csrf_denied','origin_denied','pair_required','not_dispatched'].includes(code)){finish(scope);throw error}
    let last:WebOperation|undefined;
    for(let attempt=0;attempt<3;attempt++){
     try{last=await io.receipt(id)}catch{/* Read again, never replay. */}
     if(last&&terminal(last)){finish(scope);if(last.error)throw io.failure(last.error,rejectionDetail(last));if(path===authorizePath)markAuthorization(id,profile);return last.value}
     if(attempt<2)await new Promise(resolve=>setTimeout(resolve,300*(attempt+1)));
    }
    if(last?.recovery&&path===connectPath){const alias=connectionScope(last.recovery.profileId);if(!pending[alias]){pending[alias]=id;remember()}}
    if(last?.recovery)throw Object.assign(error instanceof Error?error:io.failure('operation_unconfirmed'),{botRecovery:last.recovery});
    throw error;
   }
  }finally{active.delete(scope)}
 };
}
