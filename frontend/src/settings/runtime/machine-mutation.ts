import type {backend} from '../../desktop';
import type {MachineMutation,MachineMutationResult} from '../../backend/contract';
export class MachineMutationError extends Error {constructor(public code:string,public uncertain=false){super(code)}}
const pending=new Map<string,string>();
const operationKey=(machine:string,action:string)=>`machine-operation:${machine}:${action}`;
export function pendingMachineMutation(machine:string,action:string){const key=operationKey(machine,action);try{return localStorage.getItem(key)||pending.get(key)}catch{return pending.get(key)}}
function retain(key:string,id?:string){if(id)pending.set(key,id);else pending.delete(key);try{if(id)localStorage.setItem(key,id);else localStorage.removeItem(key)}catch{/* Memory still retains the original ID when browser storage is unavailable. */}}
// A retry here is always a read of the original local operation, never a second
// remove/save or remote pairing request. Keep the receipt across dialog reopens.
export async function mutateMachine(call:typeof backend,input:MachineMutation):Promise<MachineMutationResult>{
 const key=operationKey(input.machineId,input.action);
 const retained=pendingMachineMutation(input.machineId,input.action);
 let result:MachineMutationResult|undefined;
 if(!retained){retain(key,input.id);try{result=await call<MachineMutationResult>('ManageMachine',input)}catch{}}
 const id=retained||input.id;
 if(!result||['checking','unknown'].includes(result.status)){
  retain(key,id);
  for(let i=0;i<3;i++){
   try{result=await call<MachineMutationResult>('MachineMutation',id)}catch{}
   if(result&&['succeeded','failed'].includes(result.status))break;
   if(i<2)await new Promise(resolve=>setTimeout(resolve,400*(i+1)));
  }
 }
 if(result?.status==='succeeded'){retain(key);return result}
 if(result?.status==='failed'){retain(key);throw new MachineMutationError(result.code||'machine_save_failed')}
 throw new MachineMutationError('machine_save_unknown',true);
}
