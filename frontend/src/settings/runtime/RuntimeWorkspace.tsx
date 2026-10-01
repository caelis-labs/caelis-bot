import { useEffect, useRef, useState, type ReactNode } from 'react';
import type { ConnectionGroup, ConnectionModel, ModelScope, RuntimeSettingsClient, RuntimeView } from './types';
import { formatModelUse, messageOf } from './state';
import { ConfigurationError, runtimeSettingsClient } from './client';
import { ModelPicker, ModelSummary } from './ModelPicker';
import { SettingsDialog } from './SettingsDialog';
import { TeamSettings } from './TeamSettings';
import { ConnectionWizard } from './ConnectionWizard';
import { useI18n } from '../../i18n';
import './runtime.css';

export function RuntimeWorkspace({ client = runtimeSettingsClient, preparation,active=true,refreshKey=0,remote=false,heading=true,readOnly=false,connectionReadOnly=readOnly,nodeManaged=false,connectionState }: { connectionState?:'connected'|'disconnected'|'unknown'; connectionReadOnly?:boolean; nodeManaged?:boolean; readOnly?:boolean; heading?:boolean; remote?:boolean; active?:boolean; refreshKey?:number; client?: RuntimeSettingsClient; preparation: (id: string, onBusy: (busy: boolean) => void, defaultLocalView?:boolean) => ReactNode }) {
 const { t, locale } = useI18n();
 const [view, setView] = useState<RuntimeView | null>(null);
 const [error, setError] = useState(''), [notice, setNotice] = useState(''), [loading, setLoading] = useState(true);
 const [editing, setEditing] = useState<ModelScope | null>(null), [connecting, setConnecting] = useState(false), [switching, setSwitching] = useState<string | null>(null);
 const [removing, setRemoving] = useState<{ group: ConnectionGroup; model: ConnectionModel } | null>(null), [removeError, setRemoveError] = useState(''), [busy, setBusy] = useState(false);
 const [preparationBusy, setPreparationBusy] = useState(false);
 const [connectionClient,setConnectionClient]=useState<RuntimeSettingsClient|null>(null),[connectionStarting,setConnectionStarting]=useState(false),[connectionError,setConnectionError]=useState('');
 const connectionWorking=useRef(false),connectionOwner=useRef<RuntimeSettingsClient|null>(null),connectionEpoch=useRef(0),alive=useRef(true);
 const generation = useRef(0), working = useRef(false);
 const editingRevision=useRef(''),editingSelection=useRef<RuntimeView['main']>(null),editingClient=useRef(client);
 const refresh = async (strict = false) => {
  const id = ++generation.current; setLoading(true); setError('');
  try { const next = await client.read(); if (id === generation.current) {setView(next);return next;} }
  catch (e) { if (id === generation.current) setError(messageOf(e, t('runtime.actionFailed'))); if(strict) throw e; }
  finally { if (id === generation.current) setLoading(false); }
 };
 useEffect(() => { if(active)void refresh(); return () => { generation.current++; }; }, [client,active,refreshKey]);
 useEffect(()=>{
  alive.current=true;
  return ()=>{alive.current=false;connectionEpoch.current++;queueMicrotask(()=>{if(!alive.current)void connectionOwner.current?.closeConnection?.().catch(()=>{});});};
 },[]);
 useEffect(()=>{
  const close=()=>{connectionEpoch.current++;void Promise.resolve(connectionOwner.current?.closeConnection?.()).then(()=>{if(alive.current){setConnecting(false);setConnectionClient(null);}}).catch(()=>{if(alive.current)setConnectionError(t('settings.nodeOperationUnknown'));});};
  window.addEventListener('settings-close',close);
  if(!active)close();
  return ()=>window.removeEventListener('settings-close',close);
 },[active]);
 const connect=async()=>{
  if(connectionWorking.current||connecting)return;
  connectionWorking.current=true;setConnectionStarting(true);setConnectionError('');
  const epoch=++connectionEpoch.current,captured=client;connectionOwner.current=captured;
  try {
   const prepared=await captured.beginConnection?.()??captured;
   if(!alive.current||epoch!==connectionEpoch.current){await prepared.closeConnection?.();return;}
   connectionOwner.current=prepared;setConnectionClient(prepared);setConnecting(true);
  } catch (error) {if(alive.current&&epoch===connectionEpoch.current)setConnectionError(error instanceof ConfigurationError&&!error.unknown?error.message:t('settings.nodeOperationUnknown'));}
  finally {connectionWorking.current=false;if(alive.current)setConnectionStarting(false);}
 };
 // Navigation cannot silently discard an active settings/authentication dialog.
 useEffect(() => {
  const guard = (event: Event) => { if (connectionWorking.current || editing || connecting || removing || switching !== null) event.preventDefault(); };
  window.addEventListener('settings-navigate', guard);
  return () => window.removeEventListener('settings-navigate', guard);
 }, [editing, connecting, removing, switching]);
 const remove = async () => {
  if (!removing || working.current) return;
  working.current = true; setBusy(true); setRemoveError('');
  try { await client.removeModel(removing.group, removing.model, view?.revision); setRemoving(null); setNotice(t('runtime.connectionRemoved')); await refresh(); }
  catch { setRemoveError(t('runtime.removeConfirmFailed')); }
  finally { working.current = false; setBusy(false); }
 };
 const remoteView=remote&&!view?.local;
 const caelis = view?.profile.runtime === 'caelis', name = caelis ? 'Caelis' : 'Codex';
 const connected=!error&&view?.setup.state==='ready'&&(connectionState===undefined||connectionState==='connected');
 const connectionMessage=error||connectionState==='unknown'?t('runtime.connectionUnknown'):t('runtime.notConnected');
 const edit=(scope:ModelScope)=>{editingRevision.current=view?.revision??'';editingSelection.current=(scope==='conversation'?view?.conversation:scope==='runtime'?view?.main:view?.work)??null;editingClient.current=client.capture?.(editingRevision.current)??client;setEditing(scope);};
 const selection = editingSelection.current;
 return <section className="runtime-workspace">
  {heading&&<h1>{t(remoteView?'settings.productTargetRuntime':'runtime.runtimeAndModelsTitle')}</h1>}
  {!view ? <div className="runtime-empty"><p role="status">{loading ? t('runtime.loadingRuntime') : t('runtime.loadRuntimeFailed')}</p>{error && <p role="alert" className="inline-error">{error}</p>}<button disabled={loading||readOnly||!!error} onClick={() => void refresh()}>{t('runtime.reload')}</button>{!remoteView&&!loading&&<button className="text-action" onClick={()=>setSwitching('')}>{t('runtime.completeSetup')}</button>}</div> : <>
   <div className="runtime-active"><span className="runtime-monogram" aria-hidden="true">{caelis ? 'C' : '⌘'}</span><div><strong>{name}</strong><p>{remoteView ? t('settings.productHostConfiguration') : connected ? t('runtime.connected') : connectionState&&connectionState!=='connected'?connectionMessage:view.setup.message || t('runtime.setupRequired')}{view.pending && t('runtime.pendingSwitch', { runtime: view.pending })}</p></div></div>
   {!remoteView && view.setup.state !== 'ready' && <p className="settings-note"><button className="text-action" onClick={() => setSwitching(view.profile.runtime)}>{t('runtime.completeSetup')}</button></p>}

   {(!remoteView||nodeManaged) && < >
    <section className="runtime-model-section">
     <div className="runtime-setting-row"><strong>{t('runtime.botConversation')}</strong>{view.conversation ? <ModelSummary showDetail={false} value={view.conversation} models={view.models} label={` ${t('runtime.botConversationModel')}`} disabled={loading||readOnly||!!error} onClick={() => edit('conversation')}/> : <span className="settings-note">{t('runtime.notConnected')}</span>}</div>
    </section>
   </>}
   < >
    <div className="runtime-section-title"><div><h2>{t('runtime.connectionsHeading')}</h2></div>{(!remoteView||nodeManaged&&caelis)&&<button disabled={connectionReadOnly||loading||connectionStarting||!!error||nodeManaged&&caelis&&!view.setup.installation.installed} onClick={() => caelis ? void connect() : setSwitching('codex')}>{caelis ? t('runtime.addConnection') : t(connected?'runtime.manageAccount':'runtime.connectAccount')}</button>}</div>
    {caelis ? view.connections.length ? view.connections.map(group => <details className="runtime-connection-group" key={group.id}><summary><span><strong>{group.name}</strong><small>{group.kind === 'agent' ? t('runtime.externalAgent') : t('runtime.modelCount', { count: group.models.length })}</small></span><span aria-hidden="true">⌄</span></summary>{group.models.map(model => <div className="runtime-connection-model" key={model.id}><div><strong>{model.name}</strong><p>{model.unavailable ? t('runtime.reauthNeeded') : model.uses.map(u => formatModelUse(u, t)).join(' · ') || t('runtime.notInUse')}</p></div>{group.kind === 'provider' && <span title={model.uses.length ? t('runtime.inUseByHint', { uses: model.uses.map(u => formatModelUse(u, t)).join(locale === 'zh-CN' ? '、' : ', ') }) : undefined}><button disabled={readOnly||!!error||!!model.uses.length} aria-label={t('runtime.removeModelLabel', { label: model.name })} onClick={() => { setRemoving({ group, model }); setRemoveError(''); }}>{t('runtime.remove')}</button></span>}</div>)}{group.kind === 'agent' && <div className="setup-end"><button disabled={readOnly||!!error||group.models.some(m => m.uses.length > 0)} onClick={() => { setRemoving({group,model:group.models[0]});setRemoveError(''); }}>{t('runtime.disconnectAgent')}</button></div>}</details>) : <div className="runtime-empty">{t('runtime.noConnections')}</div> : <p className="settings-note">{!connected?connectionMessage:view.setup.accountType === 'chatgpt' ? t('runtime.connectedViaChatGPT') : view.setup.accountType === 'apiKey' ? t('runtime.connectedViaApiKey') : t('runtime.viewOrChangeCodexLogin')}</p>}
   </>
   <details className="settings-disclosure runtime-advanced-settings"><summary>{t('runtime.tabAdvanced')}</summary>
    <div className="runtime-setting-row"><div><strong>{t('runtime.connectionService')}</strong><p>{name}</p></div><div className="runtime-active-actions"><button disabled={readOnly||!!error} onClick={() => setSwitching(view.profile.runtime)}>{t('runtime.manage')}</button>{!remoteView&&<button className="text-action" onClick={() => setSwitching('')}>{t('runtime.switch')}</button>}</div></div>
    <section className="runtime-model-section">     <div className="runtime-setting-row"><strong>{caelis ? t('runtime.workMainModel') : t('runtime.workModel')}</strong>{(caelis ? view.main : view.work) ? <ModelSummary value={(caelis ? view.main : view.work)!} models={view.models} label={caelis ? ` ${t('runtime.caelisMainModel')}` : ` ${t('runtime.workModel')}`} disabled={readOnly||!!error||loading || caelis && !view.canEditMain} onClick={() => edit(caelis ? 'runtime' : 'work')}/> : <span className="settings-note">{t('runtime.notConnected')}</span>}</div>
     {caelis && !view.canEditMain && <p className="settings-note">{t('runtime.updateCaelisForMainModel')}</p>}
     {(!remoteView||nodeManaged) && caelis && !!view.work?.model && <p className="runtime-callout">{t('runtime.dedicatedWorkModelAssigned')}<button disabled={readOnly||!!error} className="text-action" onClick={() => edit('work')}>{t('runtime.change')}</button></p>}

    </section>
    {(!remoteView||nodeManaged) && caelis && view.work && <div className="runtime-setting-row"><strong>{t('runtime.dedicatedWorkModel')}</strong><ModelSummary disabled={readOnly||!!error} value={view.work} models={view.models} label={` ${t('runtime.dedicatedWorkModel')}`} onClick={() => edit('work')}/></div>}
    {caelis && <TeamSettings disabled={readOnly||!!error} team={view.team} models={view.team.models} client={client} onRefresh={async()=>{await refresh(true);}}/>}
   </details>
   <div className="runtime-page-footer"><span role="status">{loading ? t('runtime.refreshing') : notice}</span><button className="text-action" disabled={loading||readOnly||!!error} onClick={() => void refresh()}>{t('runtime.refreshConfig')}</button></div>
   {error && <p role="alert" className="inline-error">{error}</p>}
  </>}
  {editing && selection && view && <ModelPicker disabled={readOnly||!!error} title={editing === 'conversation' ? t('runtime.botConversationModel') : editing === 'runtime' ? t('runtime.caelisMainModel') : t('runtime.workModel')} description={editing === 'work' ? t('runtime.dedicatedWorkModelDescription') : undefined} value={selection} models={view.models} onReload={async()=>{const next=await refresh(true);if(next){editingRevision.current=next.revision;editingClient.current=client.capture?.(next.revision)??client;}}} inherited={editing !== 'runtime'} onSave={async value => { await editingClient.current.saveModel(editing, value, editingRevision.current); setNotice(t('runtime.modelSaved')); await refresh(); }} onClose={() => setEditing(null)} onConnect={(!remoteView||nodeManaged) && caelis && !connectionReadOnly ? () => { setEditing(null); void connect(); } : undefined}/>}
  {connectionStarting&&<p role="status" className="settings-note">{t('connections.preparing')}</p>}
  {connectionError&&<p role="alert" className="inline-error">{connectionError}</p>}
  {connecting && connectionClient && <ConnectionWizard client={connectionClient} onClose={() => {setConnecting(false);setConnectionClient(null);}} onConnected={async () => { if(alive.current){setConnecting(false);await refresh();} }}/ >}
  {removing && <SettingsDialog title={removing.group.kind === 'agent' ? t('runtime.disconnectAgentPrompt', { name: removing.group.name }) : t('runtime.removeModelPrompt', { name: removing.model.name })} busy={busy} onClose={() => setRemoving(null)}><p className="settings-note">{removing.group.kind === 'agent' ? t('runtime.disconnectAgentNote', { count: removing.group.models.length }) : t('runtime.removeModelNote')}</p>{removeError && <p role="alert" className="inline-error">{removeError}</p>}<div className="setup-end"><button disabled={busy} onClick={() => { setRemoving(null); if (removeError) void refresh(); }}>{removeError ? t('runtime.closeAndRefresh') : t('common.cancel')}</button><button disabled={readOnly||!!error||busy || !!removeError} onClick={() => void remove()}>{busy ? t('runtime.removing') : t('runtime.remove')}</button></div></SettingsDialog>}
  {switching !== null && <SettingsDialog busy={preparationBusy} title={switching ? (switching === 'caelis' ? t('runtime.manageCaelis') : t('runtime.manageCodex')) : t('runtime.switchRuntime')} description={switching ? undefined : t('runtime.switchRuntimeDescription')} onClose={() => { setSwitching(null); void refresh(); }}>{switching ? preparation(switching, setPreparationBusy, view?.local===true) : <div className="runtime-choices">{['caelis', 'codex'].map(id => <button key={id} onClick={() => setSwitching(id)}><div><strong>{id === 'caelis' ? 'Caelis' : 'Codex'}</strong><span>{id === view?.profile.runtime ? t('runtime.currentInUse') : id === 'caelis' ? t('runtime.caelisOptionDescription') : t('runtime.codexOptionDescription')}</span></div><span aria-hidden="true">→</span></button>)}</div>}</SettingsDialog>}
 </section>;
}
