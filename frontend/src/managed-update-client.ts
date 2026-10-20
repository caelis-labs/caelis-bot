import {managedUpdateRoutes as routes,type ManagedUpdateCommand as Command,type ManagedUpdateReceipt as Receipt,type ManagedUpdateStatus as Status} from './managed-update-contract.ts';
export type UpdateIntent=Omit<Command,'id'>;
export type UpdateView={status:Status;pending:boolean;confirmed:boolean};
type Journal={command?:Command;install?:{boot:string;version:string;kind:string;command:Command;transaction?:string}};
const key='caelisbot.managed-update.v1';
const definitive=new Set(['offer_unavailable','offer_not_newer','transaction_conflict','commit_already_started','update_busy','invalid_schedule','unknown_action','external_installation','image_managed','request_id_required','invalid_request','csrf_denied','origin_denied','pair_required']);
export function createManagedUpdateClient(io:{get:(path:string)=>Promise<unknown>;post:(path:string,body:unknown)=>Promise<unknown>;storage:Pick<Storage,'getItem'|'setItem'>;identity:()=>string}){
 let journal:Journal={},busy=false;
 try{const saved=JSON.parse(io.storage.getItem(key)||'{}');if(saved&&typeof saved==='object')journal=saved;}catch{/* No prior command. */}
 const save=()=>io.storage.setItem(key,JSON.stringify(journal));
 const reconcile=async(status:Status)=>{
  const command=journal.command;if(!command)return;
  if(command.action==='check'&&status.mode!=='managed'){delete journal.command;save();return;}
  try{const receipt=await io.get(routes.receipt(command.id)) as Receipt;if(receipt.id!==command.id||!['accepted','read_only'].includes(receipt.state))return;
   if(command.action==='install'&&journal.install)journal.install.transaction=receipt.transactionId;
   delete journal.command;save();
  }catch{/* Unknown means read the same receipt again, never issue a new write. */}
 };
 const read=async():Promise<UpdateView>=>{
  const status=await io.get(routes.status) as Status;
  if(status.protocol!==1)throw new Error('update_protocol_unavailable');
  await reconcile(status);
  const expected=journal.install;
  let receiptConfirmed=!expected;
  if(expected?.command)try{const receipt=await io.get(routes.receipt(expected.command.id)) as Receipt;receiptConfirmed=receipt.id===expected.command.id&&receipt.state==='accepted'&&receipt.transactionId===expected.transaction;}catch{/* Keep health unconfirmed until the original receipt can be read. */}
  // The owner verifies the signed identity. A browser additionally requires the
  // original transaction and a newly observed boot before announcing completion.
  const confirmed=receiptConfirmed&&status.state==='installed'&&(!expected||!!expected.transaction&&status.transactionId===expected.transaction&&status.bootId!==expected.boot&&status.current.version===expected.version&&status.current.kind===expected.kind&&/^[a-f0-9]{40}$/.test(status.current.source));
  return {status,pending:!!journal.command,confirmed};
 };
 return {read,async command(intent:UpdateIntent):Promise<UpdateView>{
  if(busy)throw new Error('update_busy');busy=true;
  try{
   const {status,pending}=await read();if(pending)throw new Error('update_unconfirmed');
   const command:Command={...intent,id:io.identity()};
   journal.command=command;
   if(command.action==='install')journal.install={boot:status.bootId||'',version:status.latest||'',kind:status.current.kind,command};
   save(); // Fail before dispatch when original command cannot be retained.
   try{const receipt=await io.post(routes.command,command) as Receipt;
    if(receipt.id!==command.id||!['accepted','read_only'].includes(receipt.state))throw new Error('update_unconfirmed');
    if(command.action==='install'&&journal.install)journal.install.transaction=receipt.transactionId;
    delete journal.command;save();
   }catch(error){const code=(error as {code?:string}).code;if(code&&definitive.has(code)){delete journal.command;if(command.action==='install')delete journal.install;save();throw new Error(code)}
    // A lost HTTP reply is not permission to resubmit. Status/receipt polling
    // survives remount and reload using the saved original ID and command.
    const state=await read();if(state.pending)throw new Error('update_unconfirmed');return state;
   }
   return await read();
  }finally{busy=false}
 }};
}
export function updateProgress(status:Status){return status.state==='downloading'&&status.totalBytes>0?Math.min(100,Math.max(0,status.receivedBytes/status.totalBytes*100)):undefined;}
export function updateReason(reason:string|undefined){
 const known=['channel_handoff_required','runtime_start_failed','runtime_readback_failed','external_installation','image_managed','release_trust_unconfigured','work_pending','operation_unconfirmed','download_interrupted','new_version_unhealthy','rollback_unavailable','shutdown_unconfirmed','persistence_unconfirmed','update_unconfirmed','receipt_unconfirmed','transaction_conflict','commit_already_started','offer_unavailable','update_busy'];
 return known.includes(reason||'')?reason!:'generic';
}
