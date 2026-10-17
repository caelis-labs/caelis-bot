import type {ExecutionSettings,LocalWorkerSettings,ModelOption,RuntimeSettings,SetupOverview,SetupState,WorkExecutionSettings} from '../../backend/contract';
import {validSelection} from './state.ts';

type Call=<T>(method:string,...args:unknown[])=>Promise<T>;
export type RuntimeID='caelis'|'codex';
export type RuntimeView={profile:RuntimeSettings;state:SetupState;models:ModelOption[]};
export const encodeModel=(runtime:string,model:string)=>JSON.stringify([runtime,model]);
export const followConversationModel=encodeModel('conversation','');
export const followingWorkerModel:WorkExecutionSettings={model:followConversationModel,effort:'',serviceTier:''};

// Empty provider defaults are not real catalog entries. Keep internal route
// keys out of presentation and only declare a saved choice obsolete after a
// successful catalog read, never while loading or temporarily disconnected.
export function workerPickerValue(runtime:string,value:WorkExecutionSettings,views:Partial<Record<RuntimeID,RuntimeView>>):WorkExecutionSettings {
 if(!value.model||runtime==='conversation'||!runtime)return followingWorkerModel;
 const view=views[runtime as RuntimeID];
 if(view?.state.state==='ready'&&!validSelection(value,view.models))return followingWorkerModel;
 return {...value,model:encodeModel(runtime,value.model)};
}

// Inspection already carries negotiated model parameters. An older adapter may
// omit them; only that case needs the separate catalog fallback.
export function inspectedModels(state:SetupState):ModelOption[]|null {
 if(state.state!=='ready')return [];
 const available=state.models.filter(m=>!m.noAuth);
 return available.every(m=>m.parameters)?available.map(m=>m.parameters!):null;
}

export async function loadConnections(call:Call, selections:(values:Awaited<ReturnType<typeof readSelections>>)=>void, runtime:(id:RuntimeID,result:RuntimeView|Error)=>void, inspect=true) {
 // SetupOverview shares the native inspection lock. Read selections BEFORE
 // starting inspection, so unrelated CLI probes cannot queue ahead of them.
 const current=await readSelections(call);
 selections(current);
 if(!inspect)return;
 await Promise.all((['caelis','codex'] as const).map(async id=>{
  try {
   const profile=await call<RuntimeSettings>('SetupProfile',id);
   const state=await call<SetupState>('InspectSetup',profile);
   const models=inspectedModels(state)??await call<ModelOption[]>('SetupModels',id);
   runtime(id,{profile,state,models});
  }catch(e){runtime(id,e instanceof Error?e:new Error(String(e)));}
 }));
 // Successful inspection may have reconciled an obsolete Worker default.
 // Read that small local preference again without repeating native discovery.
 try {current[2]={status:'fulfilled',value:await call<LocalWorkerSettings>('LocalWorkerSelection')};selections(current);}catch {/* Preserve the last successful local read. */}
}
function readSelections(call:Call){return Promise.allSettled([
 call<SetupOverview>('SetupOverview'),call<ExecutionSettings>('ExecutionSettings'),call<LocalWorkerSettings>('LocalWorkerSelection'),
]);}
