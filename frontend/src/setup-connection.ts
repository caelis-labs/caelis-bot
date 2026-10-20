import type {SetupOverview,Snapshot} from './backend/contract';
type Read=<T>(method:string,...args:unknown[])=>Promise<T>;

// A ready catalog is not an active connection. In particular, the old Codex
// snapshot must never authorize a newly selected Caelis introduction.
export async function selectedSetupReady(read:Read,runtime:string){
 const [overview,snapshot]=await Promise.all([read<SetupOverview>('SetupOverview'),read<Snapshot>('Snapshot')]);
 return !!runtime&&overview.hasRuntimeChoice&&overview.active===runtime&&snapshot.connection==='ready'&&snapshot.phase!=='unknown';
}
const key='caelisbot.onboarding-introduction';
export function introductionStorage():Storage|undefined{try{return globalThis.sessionStorage}catch{return undefined}}
export function readIntroductionDraft(storage:Storage|undefined){
 try{const v=JSON.parse(storage?.getItem(key)||'{}');return {name:typeof v.name==='string'?v.name.slice(0,80):'',description:typeof v.description==='string'?v.description.slice(0,2000):''}}catch{return {name:'',description:''}}
}
export function saveIntroductionDraft(storage:Storage|undefined,value:{name:string;description:string}){
 try{if(value.name||value.description)storage?.setItem(key,JSON.stringify(value));else storage?.removeItem(key)}catch{/* The current form remains authoritative if storage is unavailable. */}
}

export function activationNeedsRecovery(error:unknown){
 if(!error||typeof error!=='object')return false;
 const value=error as {code?:unknown;message?:unknown};
 return value.code==='setup_blocked_runtime_recovery_required'||value.message==='原连接还有工作需要核对，尚未切换连接。你的输入已保留';
}
