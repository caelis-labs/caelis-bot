import { backend } from '../../desktop';
import type { ExecutionSettings, ModelOption, RuntimeSettings, SetupChoice, SetupOverview, SetupRequest, SetupState, WorkExecutionSettings, RuntimeConfiguration, RuntimeMutationResult, RuntimeFlow } from '../../backend/contract';
import { groupLegacyModels, withWorkUsage } from './state';
import type { ConnectionFlow, RuntimeSettingsClient } from './types';

type Invoke = <T>(method: string, ...args: unknown[]) => Promise<T>;
const setupRequest = (settings: RuntimeSettings, action: string, fields: Partial<SetupRequest> = {}): SetupRequest => ({ settings, action, provider: '', baseUrl: '', model: '', apiKey: '', ...fields });
export class ConfigurationError extends Error {
 readonly unknown: boolean;
 constructor(readonly receipt: RuntimeMutationResult) { super(receipt.message); this.unknown = !['rejected','conflicted'].includes(receipt.outcome); }
}
function projectFlow(flow: RuntimeFlow): ConnectionFlow {
 return {...flow,stage:flow.stage as ConnectionFlow['stage'],installation:flow.installation ?? undefined,authorization:flow.authorization ?? undefined};
}
function checkResult(receipt: RuntimeMutationResult) { if(receipt.outcome !== 'committed') throw new ConfigurationError(receipt); }

// Native user settings methods own Host authority, exact profile selectors,
// configuration CAS and interactive authentication. The renderer never receives
// the Host credential, persists a secret or dispatches arbitrary protocol paths.
export function createRuntimeSettingsClient(invoke: Invoke = backend, profile?: RuntimeSettings): RuntimeSettingsClient {
 const currentProfile = () => profile ? Promise.resolve(profile) : invoke<RuntimeSettings>('RuntimeSettings');
 const observe = (initial: RuntimeFlow, signal: AbortSignal, progress: (flow: ConnectionFlow) => void) => {
  let current=initial;
  void (async()=>{
   while(!signal.aborted && !['complete','failed','unknown'].includes(current.stage)) {
    try { const next=await invoke<RuntimeFlow>('WaitRuntimeConnection',current.id,current.sequence);if(signal.aborted)return;current=next;progress(projectFlow(next)); }
    catch { if(!signal.aborted)progress({...projectFlow(current),stage:'unknown',title:'连接状态暂时无法确认',message:'请关闭面板并刷新连接，核对结果后再操作。'});return; }
   }
  })();
  return projectFlow(initial);
 };
 const change=async(action:object)=>checkResult(await invoke<RuntimeMutationResult>('ChangeRuntimeConfiguration',action));
 return {
  async read() {
   const [profile, overview] = await Promise.all([currentProfile(), invoke<SetupOverview>('SetupOverview')]);
   const setup = await invoke<SetupState>('InspectSetup', profile);
   let conversation: ExecutionSettings | null = null, work: WorkExecutionSettings | null = null, models: ModelOption[] = [];
   if (setup.state === 'ready') [conversation, work, models] = await Promise.all([invoke<ExecutionSettings>('ExecutionSettings'), invoke<WorkExecutionSettings>('WorkExecutionSettings'), invoke<ModelOption[]>('Models')]);
   const shared = profile.runtime==='caelis' && ['ready','models'].includes(setup.state) ? await invoke<RuntimeConfiguration>('RuntimeConfiguration') : null;
   const view = { profile, setup, pending: overview.pending, models:shared?.models ?? models, conversation, work,
    revision:shared?.revision ?? '', main:shared?.main ?? null, canEditMain:!!shared,
    team:shared?.team ?? { available:false,reason:'连接 Caelis 后可以配置 Team。',revision:'',roles:[],sets:[],activeSet:'',models:[] },
    connections:shared?.connections.map(g=>({...g,kind:g.kind as 'provider'|'agent',models:g.models.map(m=>({...m,uses:[...m.uses,...(m.id===conversation?.model?['Bot 对话']:[])]}))})) ?? groupLegacyModels(setup.models, setup.selectedModel),
   };
   return withWorkUsage(view);
  },
  async saveModel(scope, selection, revision) {
   const fields = { model: selection.model, effort: selection.effort, serviceTier: selection.serviceTier };
   if (scope === 'runtime') { await change({action:'main',selection:fields,expectedRevision:revision});return; }
   if (scope === 'work') { await invoke('SaveWorkExecutionSettings', fields); return; }
   const current = await invoke<ExecutionSettings>('ExecutionSettings');
   await invoke('SaveExecutionSettings', { ...current, ...fields });
  },
  async changeTeam(request, revision) { await change({...request,expectedRevision:revision}); },
  async removeModel(group, model, revision) {
   if (model.uses.length || group.kind==='agent' && group.models.some(m=>m.uses.length)) throw new Error('请先更改正在使用此连接的配置。');
   await change({action:group.kind==='agent'?'disconnect-agent':'remove-model',id:group.kind==='agent'?group.id:model.id,expectedRevision:revision});
  },
  async catalog(kind) {return invoke('RuntimeConnectionCatalog',kind,profile ?? null);},
  async apiKeyOptions(provider, baseUrl) {
   const profile = await currentProfile();
   const endpoints = await invoke<SetupChoice[]>('SetupCatalog', setupRequest(profile, 'endpoints', { provider }));
   const models = await invoke<SetupChoice[]>('SetupCatalog', setupRequest(profile, 'models', { provider, baseUrl: baseUrl || endpoints[0]?.value || '' }));
   return { endpoints: endpoints.map(e => ({ value:e.value,name:e.label || e.value,reuseAuth:e.noAuth })), models: models.map(m => ({ value:m.value,name:m.label || m.value })) };
  },
  async startConnection(input,signal,progress){signal.throwIfAborted();return observe(await invoke<RuntimeFlow>('StartRuntimeConnection',{...input,settings:profile ?? null}),signal,progress);},
  async advanceConnection(flow,action,input,signal,progress){signal.throwIfAborted();return observe(await invoke<RuntimeFlow>('AdvanceRuntimeConnection',{id:flow.id,revision:flow.revision,action,input}),signal,progress);},
  async cancelConnection(flow){await invoke('CancelRuntimeConnection',flow.id);},
  async openURL(url){await invoke('OpenMessageLink',url);},
 };
}
export const runtimeSettingsClient = createRuntimeSettingsClient();
