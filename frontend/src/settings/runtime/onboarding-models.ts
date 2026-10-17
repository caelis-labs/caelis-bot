import type { SetupState, RuntimeSettings, SetupRequest } from '../../backend/contract';
import type { backend } from '../../desktop';

export function existingModelChoice(state:SetupState|null,draft='') {
 const models=state&&!state.loginPending&&['ready','models'].includes(state.state)?state.models.filter(m=>!m.noAuth):[];
 const selected=[draft,state?.selectedModel,models.find(m=>m.current)?.value,models[0]?.value].find(value=>models.some(m=>m.value===value))||'';
 return {models,selected};
}

// Menu changes are local drafts. Only Next applies a choice. Never recreate
// authentication or retry an uncertain effect. Native readiness gates work.
export async function selectExistingModel(call:typeof backend,profile:RuntimeSettings,state:SetupState,model:string) {
 if(!existingModelChoice(state).models.some(m=>m.value===model))throw new Error('Model is no longer available');
 if(state.selectedModel!==model){
  const request:SetupRequest={settings:profile,action:'use-model',model,provider:'',baseUrl:'',apiKey:''};
  const next=await call<SetupState>('ApplySetup',request);
  if(next.state!=='ready'||next.selectedModel!==model)throw new Error('Model selection was not confirmed');
 }
}
export async function activateExistingModel(call:typeof backend,profile:RuntimeSettings,state:SetupState,model:string) {
 await selectExistingModel(call,profile,state,model);
 await call('ActivateRuntime',profile);
}
