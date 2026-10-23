// Opaque IDs only persist. Arguments (including credentials) stay in memory.
// Every subsequent invocation first reads its original receipt; a changed body
// must never inherit that receipt's value or silently dispatch a second effect.
const rejectionDetails=new Set(['pairing_code_invalid','pairing_code_expired','peer_authorization_revoked','peer_authentication_required','original_peer_proof_required','worker_sharing_disabled','worker_not_ready','bot_worker_contract_incompatible','bot_peer_identity_changed','invalid_bot_address','worker_listener_invalid','worker_reachable_address_required','worker_listener_managed_headless']);
export function safeWebRejectionDetail(value:unknown){return typeof value==='string'&&rejectionDetails.has(value)?value:undefined;}
type WebOperation={state:string;error?:string;detail?:string;value?:unknown};
const rejectionDetail=(op:WebOperation)=>op.error==='operation_rejected'?safeWebRejectionDetail(op.detail):undefined;
export function createWebOperations(io:{storage:Storage;identity:()=>string;post:(path:string,body:unknown,timeout:number)=>Promise<{value:unknown}>;receipt:(id:string)=>Promise<WebOperation>;failure:(code:string,detail?:string)=>Error}){
 const key='caelisbot.pending-operations';
 let pending:Record<string,string>={};
 try{const saved=JSON.parse(io.storage.getItem(key)||'{}');if(saved&&typeof saved==='object'&&!Array.isArray(saved))pending=Object.fromEntries(Object.entries(saved).filter((e):e is [string,string]=>typeof e[1]==='string'))}catch{/* No saved identities. */}
 const bodies=new Map<string,string>(),active=new Set<string>();
 const remember=()=>io.storage.setItem(key,JSON.stringify(pending));
 const finish=(path:string)=>{delete pending[path];bodies.delete(path);remember()};
 const terminal=(op:WebOperation)=>{
  const value=op.value as {outcome?:string;state?:string}|null;
  return op.state==='complete'&&!op.error?.startsWith('operation_unconfirmed')&&value?.outcome!=='unknown'&&value?.state!=='unknown';
 };
 const settle=async(path:string,id:string,same:boolean)=>{
  const op=await io.receipt(id);
  if(!terminal(op))throw io.failure('operation_unconfirmed');
  finish(path);
  if(!same)throw io.failure('previous_operation_resolved');
  if(op.error)throw io.failure(op.error,rejectionDetail(op));
  return op.value;
 };
 return async(path:string,args:unknown[],read:boolean)=>{
  if(read)return (await io.post(path,{id:io.identity(),args},8000)).value;
  if(active.has(path))throw io.failure('operation_unconfirmed');
  active.add(path);
  try{
   const prior=pending[path],body=JSON.stringify(args);
   if(prior)return await settle(path,prior,bodies.get(path)===body);
   const id=io.identity();pending[path]=id;bodies.set(path,body);remember();
   try{
    const result=await io.post(path,{id,args},120000);
    // A successful HTTP response may itself contain a native unknown receipt.
    if(terminal({state:'complete',value:result.value}))finish(path);
    return result.value;
   }catch(error){
    const code=(error as {code?:string})?.code;
    // These are refusals *before admission*. Owner refusals below are read back
    // from the durable receipt, including request_conflict after response loss.
    if(code&&['composer_owned_elsewhere','request_id_required','invalid_request','csrf_denied','origin_denied','pair_required','not_dispatched'].includes(code)){finish(path);throw error}
    for(let attempt=0;attempt<3;attempt++){
     let op;try{op=await io.receipt(id)}catch{/* Read again, never replay. */}
     if(op&&terminal(op)){finish(path);if(op.error)throw io.failure(op.error,rejectionDetail(op));return op.value}
     if(attempt<2)await new Promise(resolve=>setTimeout(resolve,300*(attempt+1)));
    }
    throw error;
   }
  }finally{active.delete(path)}
 };
}
