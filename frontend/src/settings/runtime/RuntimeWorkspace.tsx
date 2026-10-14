import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { ConnectionGroup, ConnectionModel, ModelScope, RuntimeSettingsClient, RuntimeView } from './types';
import { executionMethodLabel, formatModelUse, messageOf } from './state';
import { createRuntimeSettingsClient, runtimeSettingsClient } from './client';
import { ModelPicker, ModelSummary } from './ModelPicker';
import { SettingsDialog } from './SettingsDialog';
import { ConnectionWizard } from './ConnectionWizard';
import { useI18n } from '../../i18n';
import './runtime.css';
import {SettingsChevron, ArrowClockwiseIcon} from '../../SettingsIcons';
import {backend} from '../../desktop';
import {botSessionStatusKey} from './setup-status';
import {createModeReadback} from './mode-readback';
import {createResourceReadback, type ResourceReadback} from './resource-readback';
import {FutureModeRecovery} from './FutureModeRecovery';
import {botCardState,connectionReadiness,verifiedSetup} from './readiness';
import type {ExecutionOptions, RuntimeConfiguration, RuntimeSettings as Profile, SetupState} from '../../backend/contract';

export function RuntimeWorkspace({ client = runtimeSettingsClient, call=backend, preparation,active=true,refreshKey=0,workerSettings,onTeam }: { workerSettings?:(manage:(runtime?:string)=>void, refreshKey:number)=>ReactNode; executionSettings?:ReactNode; onTeam?:()=>void; active?:boolean; refreshKey?:number; client?: RuntimeSettingsClient; call?:typeof backend; preparation: (id: string, onBusy: (busy: boolean) => void, onPrepared: () => void, guided: boolean) => ReactNode }) {
 const { t, locale } = useI18n();
 const [view, setView] = useState<RuntimeView | null>(null);
 const [error, setError] = useState(''), [notice, setNotice] = useState(''), [loading, setLoading] = useState(true);
 const [editing, setEditing] = useState<ModelScope | null>(null), [connecting, setConnecting] = useState(false), [switching, setSwitching] = useState<string | null>(null), [groupID,setGroupID]=useState<string|null>(null);
 const [removing, setRemoving] = useState<{ group: ConnectionGroup; model: ConnectionModel } | null>(null), [removeError, setRemoveError] = useState(''), [busy, setBusy] = useState(false);
 const [stopUsing, setStopUsing] = useState(false);
 const [preparationBusy, setPreparationBusy] = useState(false);
 const [workerRefreshKey, setWorkerRefreshKey] = useState(0);
 const [profiles,setProfiles]=useState<Record<string,Profile>>({}),[readiness,setReadiness]=useState<Record<string,SetupState|null>>({});
 const [managementState,setManagementState]=useState<ResourceReadback<RuntimeConfiguration>>({status:'idle',value:null});
 const [modeOptions,setModeOptions]=useState<ExecutionOptions|null>(null),[modeBusy,setModeBusy]=useState(false);
 const [botMode,setBotMode]=useState('');
 const managedClient=useMemo(()=>switching&&profiles[switching]?createRuntimeSettingsClient(call,profiles[switching]):client,[switching,profiles,client,call]);
 const modelAnchor = useRef<HTMLElement | null>(null);
 const edit = (scope: ModelScope, anchor: HTMLElement) => { modelAnchor.current = anchor; setEditing(scope); };
 const generation = useRef(0), working = useRef(false);
 const modeReadback = useRef(createModeReadback());
 const managementReadback = useRef(createResourceReadback<RuntimeConfiguration>());
 const loadManagement = async () => {
  await managementReadback.current.load(()=>call<RuntimeConfiguration>('RuntimeConfiguration'),setManagementState);
 };
 const refresh = async (strict = false) => {
  const id = ++generation.current; modeReadback.current.invalidate(); setLoading(true); setReadiness({}); setModeOptions(null); setBotMode(''); setError('');
  try { const read = await client.read(); const next={...read,setup:verifiedSetup(read.profile.runtime,read.setup)}; if (id === generation.current) { setView(next); setWorkerRefreshKey(current=>current+1); void Promise.all(['caelis','codex'].map(async runtime=>{try {const profile=await call<Profile>('SetupProfile',runtime);const setup=verifiedSetup(runtime,await call<SetupState>('InspectSetup',profile));if(id===generation.current){setProfiles(old=>({...old,[runtime]:profile}));setReadiness(old=>({...old,[runtime]:setup}));}}catch{if(id===generation.current)setReadiness(old=>({...old,[runtime]:null}));}})); void call<ExecutionOptions>('ExecutionOptions').then(o=>{if(id===generation.current)setModeOptions(o);}).catch(()=>{}); void modeReadback.current.read(()=>call<string>('BotExecutionMode'),setBotMode,e=>{setBotMode('');setError(messageOf(e,t('settings.executionSaveFailed')));}).catch(()=>{}); } }
  catch (e) { if (id === generation.current) setError(messageOf(e, t('runtime.actionFailed'))); if(strict) throw e; }
  finally { if (id === generation.current) setLoading(false); }
 };
 useEffect(() => { if(active)void refresh(); return () => { generation.current++; modeReadback.current.invalidate(); }; }, [client,active,refreshKey]);
 useEffect(()=>{if(!switching||profiles[switching])return;void call<Profile>('SetupProfile',switching).then(profile=>setProfiles(old=>({...old,[switching]:profile}))).catch(e=>setError(messageOf(e,t('runtime.actionFailed'))));},[switching,profiles]);
 useEffect(()=>{if(switching==='caelis')void loadManagement();return()=>{managementReadback.current.invalidate();};},[switching,refreshKey]);
 // Navigation cannot silently discard an active settings/authentication dialog.
 useEffect(() => {
  const guard = (event: Event) => { if (editing || connecting || removing || switching !== null || groupID !== null) event.preventDefault(); };
  window.addEventListener('settings-navigate', guard);
  return () => window.removeEventListener('settings-navigate', guard);
 }, [editing, connecting, removing, switching, groupID]);
 const remove = async () => {
  if (!removing || working.current) return;
  working.current = true; setBusy(true); setRemoveError('');
  try { await managedClient.removeModel(removing.group, removing.model, management?.revision, stopUsing); setRemoving(null); setStopUsing(false); setNotice(t('runtime.connectionRemoved')); await refresh(); if(switching==='caelis')await loadManagement(); }
  catch (e) { setRemoveError(e instanceof Error && e.message === 'connection_in_use' ? t('runtime.connectionInUse') : e instanceof Error && e.message === 'agent_connection_in_use' ? t('runtime.agentConnectionInUse') : messageOf(e, t('runtime.removeConfirmFailed'))); }
  finally { working.current = false; setBusy(false); }
 };
 const caelis = view?.profile.runtime === 'caelis';
 const choiceRequired = !view?.hasRuntimeChoice;
 const currentRuntime = choiceRequired ? '' : view?.activeRuntime || view?.profile.runtime || '';
 const name = choiceRequired ? t('runtime.noConnection') : executionMethodLabel(currentRuntime, t);
 const live = view?.session?.connection === 'ready' || view?.session?.connection === 'connecting';
 const configured = !choiceRequired && (view?.setup.state === 'ready' || live);
 const reconnectable = configured && view?.session?.connection === 'offline' && view.session.issue !== 'setup_required';
 const sessionLabel = view ? t(botSessionStatusKey(view.setup,view.session,!choiceRequired)) : t('runtime.statusUnknown');
 const reconnect = async () => {setLoading(true);let failed=false;try {await call('Connect');}catch {failed=true;}await refresh();if(failed)setError(t('runtime.connectionFailed'));};
 const management=managementState.status==='ready'?managementState.value:null;
 const groups=management?.connections.map(g=>({...g,kind:g.kind as 'provider'|'agent'}))??[];
 const group = groups.find(entry=>entry.id===groupID);
 const removeUses = removing ? removing.group.kind === 'agent' ? removing.group.models.flatMap(model => model.uses) : removing.model.uses : [];
 const mainUse = removeUses.some(use => ['Caelis 主模型','caelis_main'].includes(use));
 const teamUse = removeUses.some(use => !['Caelis 主模型','caelis_main','Bot 对话','bot_conversation','独立工作模型','dedicated_work'].includes(use));
 const botUse = removeUses.some(use => ['Bot 对话','bot_conversation','独立工作模型','dedicated_work'].includes(use));
 const selection = editing === 'conversation' ? view?.conversation : editing === 'runtime' ? view?.main : view?.work;
 return <section className="runtime-workspace">
  <h1>{t('settings.connections')}</h1><p className="settings-page-intro">{locale==='zh-CN'?'分别选择 Bot 对话与新工作任务使用的连接。管理连接不会改变正在使用的选择。':'Choose connections for Bot conversation and new work separately. Managing a connection does not change either selection.'}</p>
  {!view ? <div className="runtime-empty"><p role="status">{loading ? t('runtime.loadingRuntime') : t('runtime.loadRuntimeFailed')}</p>{error && <p role="alert" className="inline-error">{error}</p>}<button disabled={loading} onClick={() => void refresh()}>{t('runtime.reload')}</button>{!loading&&<button className="text-action" onClick={()=>setSwitching('caelis')}>{t('runtime.completeSetup')}</button>}</div> : <>
   <section className="runtime-scope-section"><h2>{t('runtime.botConversation')}</h2><p className="settings-note">{locale==='zh-CN'?'更改 Bot 连接会在当前对话空闲时交接；不会更改新工作任务默认值。':'Changing Bot connection hands off the idle conversation and leaves the new-work default unchanged.'}</p><div className="runtime-scope-options" role="group" aria-label={t('runtime.botConversation')}>{['caelis','codex'].map(id=>{const status=botCardState(id,currentRuntime,view.pending),selected=status==='current';const ready=connectionReadiness(id,readiness[id])==='ready'||selected&&live;return <button key={id} disabled={loading||busy||modeBusy} aria-pressed={selected} onClick={async()=>{if(!ready){setSwitching(id);return;}if(selected)return;working.current=true;setBusy(true);setError('');try{await call('ActivateRuntime',profiles[id]);await refresh();}catch(e){setError(messageOf(e,t('runtime.connectionFailed')));}finally{working.current=false;setBusy(false);}}}><strong>{executionMethodLabel(id,t)}</strong><span>{selected?'✓ '+t('runtime.botCurrentConnection'):status==='pending'?t('runtime.botPendingConnection'):ready?t('runtime.botUseConnection'):t('runtime.botSetupConnection')}</span></button>;})}</div><p className="settings-note" role="status">{currentRuntime&&t('runtime.botCurrentSummary',{runtime:executionMethodLabel(currentRuntime,t)})}{currentRuntime?' · ':''}{sessionLabel}{view.pending&&t('runtime.pendingSwitch',{runtime:executionMethodLabel(view.pending,t)})}</p>{reconnectable&&<button disabled={loading} onClick={()=>void reconnect()}>{t('runtime.reconnect')}</button>}</section>
   {!choiceRequired && live && !view.setup.installation.installed && <p className="runtime-update-hint" role="status">{t('runtime.liveWithoutProgram')}</p>}
   {!choiceRequired&&!caelis&&configured&&<p className="settings-note runtime-account-note">{t(view.setup.accountType === 'chatgpt' ? 'runtime.connectedViaChatGPT' : view.setup.accountType === 'apiKey' ? 'runtime.connectedViaApiKey' : 'runtime.viewOrChangeCodexLogin')}</p>}
   {view.setup.accountMismatch && <p className="runtime-update-hint" role="status">{view.setup.message}</p>}
   <section className="runtime-model-section">
    <div className="runtime-setting-row"><div><strong>{t('runtime.botConversationModel')}</strong><p>{name} · {t('runtime.botConversationHint')}</p></div>{view.conversation ? <ModelSummary value={view.conversation} models={view.models} runtimeDefault={view.runtimeDefault} label={` ${t('runtime.botConversationModel')}`} disabled={loading} onClick={anchor => edit('conversation', anchor)}/> : <span className="settings-note">{t('runtime.notConnected')}</span>}</div>
    <h2>{t('settings.backgroundTasks')}</h2>{workerSettings?.(runtime=>setSwitching(runtime??view.profile.runtime), workerRefreshKey) ?? <>    <div className="runtime-setting-row"><div><strong>{t('runtime.workModel')}</strong><p>{t(caelis ? 'runtime.caelisWorkModelHint' : 'runtime.workModelHint')}</p></div>{(caelis ? view.main : view.work) ? <ModelSummary value={(caelis ? view.main : view.work)!} models={view.models} runtimeDefault={view.runtimeDefault} label={t('runtime.workModel')} disabled={loading || caelis && !view.canEditMain} onClick={anchor=>edit(caelis ? 'runtime' : 'work',anchor)}/> : <span className="settings-note">{t('runtime.notConnected')}</span>}</div>
    {caelis && !view.canEditMain && <p className="settings-note">{t('runtime.updateCaelisForMainModel')}</p>}
    {caelis && view.work && <div className="runtime-setting-row"><div><strong>{t('runtime.dedicatedWorkModel')}</strong><p>{t('runtime.dedicatedWorkModelHint')}</p></div><ModelSummary value={view.work} models={view.models} runtimeDefault={view.runtimeDefault} label={t('runtime.dedicatedWorkModel')} disabled={loading} onClick={anchor=>edit('work',anchor)}/></div>}</>}
   </section>
   {!caelis&&<section className="runtime-scope-section"><h2>{locale==='zh-CN'?'Caelis 下次对话偏好':'Caelis next conversation preference'}</h2><FutureModeRecovery call={call}/></section>}
   <section className="runtime-scope-section"><h2>{t('settings.execution')}</h2><p className="settings-note">{locale==='zh-CN'?(caelis?'仅用于下一段 Bot 对话的创建；当前会话权限保持不变。':'重新建立 Codex 连接后用于新对话；当前会话权限保持不变。'):(caelis?'Used when the next Bot conversation is created. Current session permissions stay unchanged.':'Used for a new conversation after the Codex connection is recreated. Current session permissions stay unchanged.')}</p><div className="runtime-scope-options runtime-permission-options" role="group" aria-label={t('settings.execution')}>{([{id:'auto',label:t('settings.approvalAuto'),detail:locale==='zh-CN'?'工作区沙箱；额外访问由自动审查决定。':'Workspace sandbox; extra access goes through automatic review.'},{id:'full-access',label:t('settings.approvalFullAccess'),detail:caelis?(locale==='zh-CN'?'仅用于新对话；需 Host 显式登记完全访问授权。':'For new conversations; requires explicit Host enrollment.'):(locale==='zh-CN'?'关闭命令沙箱，不再询问执行权限。':'Disables the command sandbox and execution prompts.')}] as const).map(option=>{const supported=!!modeOptions?.approvalModes.some(m=>m.id===option.id);const selected=botMode===option.id;return <button key={option.id} className={option.id==='full-access'?'runtime-full-access':undefined} disabled={!configured||!supported||!botMode||modeBusy} aria-pressed={selected} onClick={async()=>{if(selected)return;setModeBusy(true);setError('');try{await modeReadback.current.save(()=>call('SaveBotExecutionMode',option.id),()=>call<string>('BotExecutionMode'),setBotMode,e=>{setBotMode('');setError(messageOf(e,t('settings.executionSaveFailed')));});}catch(e){setError(messageOf(e,t('settings.executionSaveFailed')));}finally{setModeBusy(false);}}}><strong>{option.id==='full-access'?'⚠ ':''}{option.label}</strong><span>{selected?'✓ ':''}{option.detail}{!supported?' · '+(locale==='zh-CN'?'此连接暂不支持':'Unavailable for this connection'):''}</span></button>;})}</div></section>
   <section className="runtime-scope-section"><h2>{t('runtime.manageConnections')}</h2><p className="settings-note">{locale==='zh-CN'?'在各连接内安装、认证、选择模型服务与配置协作。':'Install, authenticate, manage model services and configure collaboration within each connection.'}</p><div className="runtime-connection-resources">{['caelis','codex'].map(id=><button key={id} onClick={()=>setSwitching(id)}><span><strong>{executionMethodLabel(id,t)}</strong><small>{t(`runtime.connectionReadiness_${connectionReadiness(id,readiness[id])}` as Parameters<typeof t>[0])}</small></span><SettingsChevron/></button>)}</div></section>
   <div className="runtime-page-footer"><button className="text-action" disabled={loading} onClick={() => void refresh()}><ArrowClockwiseIcon aria-hidden="true" size={14}/>{t('runtime.refreshConfig')}</button>{(loading || notice) && <p className="settings-note" role="status">{loading ? t('runtime.refreshing') : notice}</p>}</div>
   {error && <p role="alert" className="inline-error">{error}</p>}
  </>}
  {group&&<SettingsDialog title={group.name} onClose={()=>setGroupID(null)}>{group.models.map(model => <div className="runtime-connection-model" key={model.id}><div><strong>{model.name}</strong><p>{model.unavailable ? t('runtime.reauthNeeded') : model.uses.map(u => formatModelUse(u, t)).join(' · ') || t('runtime.notInUse')}</p></div>{group.kind === 'provider' && <button aria-label={t('runtime.removeModelLabel', { label: model.name })} onClick={() => { setGroupID(null); setRemoving({ group, model }); setStopUsing(false); setRemoveError(''); }}>{model.uses.length ? t('runtime.manageDisconnect') : t('runtime.remove')}</button>}</div>)}{group.kind === 'agent' && <div className="setup-end"><button onClick={() => { setGroupID(null); setRemoving({group,model:group.models[0]});setStopUsing(false);setRemoveError(''); }}>{t('runtime.disconnectAgent')}</button></div>}<div className="setup-end"><button onClick={()=>setGroupID(null)}>{t('common.done')}</button></div></SettingsDialog>}
  {editing && selection && view && <ModelPicker anchor={modelAnchor.current} runtimeDefault={view.runtimeDefault} title={editing === 'conversation' ? t('runtime.botConversationModel') : editing === 'runtime' ? t('runtime.caelisMainModel') : t('runtime.workModel')} description={editing === 'work' ? t('runtime.dedicatedWorkModelDescription') : undefined} value={selection} models={view.models} onReload={() => refresh(true)} inherited={editing !== 'runtime'} onSave={async value => { await client.saveModel(editing, value, view.revision); setNotice(t('runtime.modelSaved')); await refresh(); }} onClose={() => setEditing(null)} onConnect={caelis ? () => { setEditing(null); setSwitching('caelis'); setConnecting(true); } : undefined}/>}
  {connecting && switching==='caelis' && profiles.caelis && <ConnectionWizard client={managedClient} active={active} onClose={() => setConnecting(false)} onConnected={async () => { setConnecting(false); await refresh();await loadManagement(); }}/ >}
  {removing && <SettingsDialog title={removing.group.kind === 'agent' ? t('runtime.disconnectAgentPrompt', { name: removing.group.name }) : t('runtime.removeModelPrompt', { name: removing.model.name })} busy={busy} onClose={() => setRemoving(null)}><p className="settings-note">{removing.group.kind === 'agent' ? t('runtime.disconnectAgentNote', { count: removing.group.models.length }) : t('runtime.removeModelNote')}</p>{removeUses.length > 0 && <p className="settings-note">{t('runtime.inUseByHint', { uses: removeUses.map(u => formatModelUse(u, t)).join(locale === 'zh-CN' ? '、' : ', ') })}</p>}{mainUse && <div className="runtime-callout"><p>{t('runtime.disconnectMainFirst')}</p><button onClick={() => { setRemoving(null); setGroupID(null); setConnecting(true); }}>{t('runtime.addAnotherModel')}</button></div>}{teamUse && <div className="runtime-callout"><p>{t('runtime.disconnectTeamFirst')}</p></div>}{removing.group.kind === 'provider' && (mainUse || botUse) && !teamUse && <><label className="runtime-check"><input type="checkbox" checked={stopUsing} disabled={busy} onChange={e=>setStopUsing(e.target.checked)}/>{t('runtime.stopUsingThenDisconnect')}</label><p className="settings-note">{t('runtime.stopUsingSharedImpact')}</p></>}{removeError && <p role="alert" className="inline-error">{removeError}</p>}<div className="setup-end"><button disabled={busy} onClick={() => { setRemoving(null); if (removeError) void refresh(); }}>{removeError ? t('runtime.closeAndRefresh') : t('common.cancel')}</button>{!teamUse && (removing.group.kind === 'provider' || !mainUse && !botUse) && <button disabled={busy || !!removeError || (mainUse || botUse) && !stopUsing} onClick={() => void remove()}>{busy ? t('runtime.removing') : t('runtime.remove')}</button>}</div></SettingsDialog>}
  {switching !== null && <SettingsDialog busy={preparationBusy||connecting||!!removing||groupID!==null} title={switching === 'caelis' ? t('runtime.manageCaelis') : t('runtime.manageCodex')} onClose={() => { setSwitching(null);setManagementState({status:'idle',value:null}); void refresh(); }}>
   {preparation(switching, setPreparationBusy, () => { void refresh(); }, !readiness[switching]||readiness[switching]?.state!=='ready')}
   {switching==='caelis'&&readiness.caelis?.installation.installed&&['ready','models'].includes(readiness.caelis.state)&&<section className="runtime-management-resources">
    <div className="runtime-section-title"><h2>{t('runtime.modelConnections')}</h2><button disabled={!profiles.caelis} onClick={()=>setConnecting(true)}>{t('runtime.addConnection')}</button></div>
    {managementState.status==='loading' ? <p className="settings-note" role="status">{t('runtime.refreshing')}</p>
     : managementState.status==='error' ? <div className="runtime-worker-recovery"><p role="alert">{locale==='zh-CN'?'无法读取连接资源，请刷新核对。':'Connection resources could not be read. Refresh to verify.'} {messageOf(managementState.error,t('runtime.actionFailed'))}</p><button onClick={()=>void loadManagement()}>{t('runtime.refreshConfig')}</button></div>
     : management ? groups.length?groups.map(entry=><button className="runtime-connection-entry" key={entry.id} onClick={()=>setGroupID(entry.id)}><span><strong>{entry.name}</strong><small>{entry.kind==='agent'?t('runtime.externalAgent'):t('runtime.modelCount',{count:entry.models.length})}</small></span><SettingsChevron/></button>):<p className="settings-note" role="status">{readiness.caelis?.models.some(model=>!model.noAuth)?t('runtime.connectionResourceMismatch'):t('runtime.noConnections')}</p>
     : null}
    {onTeam&&<button className="runtime-connection-entry" onClick={()=>{setSwitching(null);onTeam();}}><span><strong>{t('runtime.teamConfiguration')}</strong><small>{t('runtime.teamConfigurationHint')}</small></span><SettingsChevron/></button>}
   </section>}
  </SettingsDialog>}
 </section>;
}
