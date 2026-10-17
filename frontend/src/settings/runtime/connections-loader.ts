import type {ExecutionSettings,LocalWorkerSettings,ModelOption,RuntimeSettings,SetupOverview,SetupState} from '../../backend/contract';

type Call=<T>(method:string,...args:unknown[])=>Promise<T>;
export type RuntimeID='caelis'|'codex';
export type RuntimeView={profile:RuntimeSettings;state:SetupState;models:ModelOption[]};

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
 selections(await readSelections(call));
 if(!inspect)return;
 await Promise.all((['caelis','codex'] as const).map(async id=>{
  try {
   const profile=await call<RuntimeSettings>('SetupProfile',id);
   const state=await call<SetupState>('InspectSetup',profile);
   const models=inspectedModels(state)??await call<ModelOption[]>('SetupModels',id);
   runtime(id,{profile,state,models});
  }catch(e){runtime(id,e instanceof Error?e:new Error(String(e)));}
 }));
}
function readSelections(call:Call){return Promise.allSettled([
 call<SetupOverview>('SetupOverview'),call<ExecutionSettings>('ExecutionSettings'),call<LocalWorkerSettings>('LocalWorkerSelection'),
]);}
