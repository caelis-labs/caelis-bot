import { useEffect, useRef, useState, type ReactNode } from 'react';
import type { ConnectionGroup, ConnectionModel, ModelScope, RuntimeSettingsClient, RuntimeView } from './types';
import { formatModelUse, messageOf } from './state';
import { runtimeSettingsClient } from './client';
import { ModelPicker, ModelSummary } from './ModelPicker';
import { SettingsDialog } from './SettingsDialog';
import { TeamSettings } from './TeamSettings';
import { ConnectionWizard } from './ConnectionWizard';
import { useI18n } from '../../i18n';
import './runtime.css';
import {SettingsChevron, ArrowClockwiseIcon} from '../../SettingsIcons';
import {backend} from '../../desktop';
import {botSessionStatusKey,setupStatusKey} from './setup-status';

export type RuntimePage = 'models' | 'connections';

export function RuntimeWorkspace({ client = runtimeSettingsClient, preparation,active=true,refreshKey=0,page='connections',onConnections,workerSettings }: { workerSettings?:ReactNode; page?:RuntimePage; onConnections?:()=>void; active?:boolean; refreshKey?:number; client?: RuntimeSettingsClient; preparation: (id: string, onBusy: (busy: boolean) => void) => ReactNode }) {
 const { t, locale } = useI18n();
 const [view, setView] = useState<RuntimeView | null>(null);
 const [error, setError] = useState(''), [notice, setNotice] = useState(''), [loading, setLoading] = useState(true);
 const [editing, setEditing] = useState<ModelScope | null>(null), [connecting, setConnecting] = useState(false), [switching, setSwitching] = useState<string | null>(null);
 const [removing, setRemoving] = useState<{ group: ConnectionGroup; model: ConnectionModel } | null>(null), [removeError, setRemoveError] = useState(''), [busy, setBusy] = useState(false);
 const [preparationBusy, setPreparationBusy] = useState(false);
 const modelAnchor = useRef<HTMLElement | null>(null);
 const edit = (scope: ModelScope, anchor: HTMLElement) => { modelAnchor.current = anchor; setEditing(scope); };
 const generation = useRef(0), working = useRef(false);
 const refresh = async (strict = false) => {
  const id = ++generation.current; setLoading(true); setError('');
  try { const next = await client.read(); if (id === generation.current) setView(next); }
  catch (e) { if (id === generation.current) setError(messageOf(e, t('runtime.actionFailed'))); if(strict) throw e; }
  finally { if (id === generation.current) setLoading(false); }
 };
 useEffect(() => { if(active)void refresh(); return () => { generation.current++; }; }, [client,active,refreshKey]);
 // Navigation cannot silently discard an active settings/authentication dialog.
 useEffect(() => {
  const guard = (event: Event) => { if (editing || connecting || removing || switching !== null) event.preventDefault(); };
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
 const caelis = view?.profile.runtime === 'caelis';
 const choiceRequired = !view?.hasRuntimeChoice;
 const name = choiceRequired ? t('runtime.noConnection') : caelis ? 'Caelis' : 'Codex';
 const configured = view?.setup.state === 'ready' && !choiceRequired;
 const reconnectable = configured && view?.session?.connection === 'offline' && view.session.issue !== 'setup_required';
 const sessionLabel = view ? t(botSessionStatusKey(view.setup,view.session,!choiceRequired)) : t('runtime.statusUnknown');
 const reconnect = async () => {setLoading(true);let failed=false;try {await backend('Connect');}catch {failed=true;}await refresh();if(failed)setError(t('runtime.connectionFailed'));};
 const selection = editing === 'conversation' ? view?.conversation : editing === 'runtime' ? view?.main : view?.work;
 return <section className="runtime-workspace">
  <h1>{t(page === 'models' ? 'settings.models' : 'settings.connections')}</h1><p className="settings-page-intro">{t(page === 'models' ? 'runtime.modelsIntro' : 'runtime.connectionsIntro')}</p>
  {!view ? <div className="runtime-empty"><p role="status">{loading ? t('runtime.loadingRuntime') : t('runtime.loadRuntimeFailed')}</p>{error && <p role="alert" className="inline-error">{error}</p>}<button disabled={loading} onClick={() => void refresh()}>{t('runtime.reload')}</button>{!loading&&<button className="text-action" onClick={()=>setSwitching('')}>{t('runtime.completeSetup')}</button>}</div> : <>
   <div className="runtime-active"><span className="runtime-monogram" aria-hidden="true">{choiceRequired ? '+' : caelis ? 'C' : '⌘'}</span><div><strong>{name}</strong><p role="status">{sessionLabel}{view.pending && t('runtime.pendingSwitch', { runtime: view.pending })}</p></div><div className="runtime-active-actions">{page === 'connections' ? <>{!configured ? <button className="primary" onClick={() => setSwitching(choiceRequired ? '' : view.profile.runtime)}>{t(choiceRequired ? 'runtime.chooseConnection' : 'runtime.completeSetup')}</button> : reconnectable ? <button disabled={loading} onClick={() => void reconnect()}>{t('runtime.reconnect')}</button> : caelis&&<button onClick={() => setSwitching(view.profile.runtime)}>{t('runtime.manage')}</button>}{!choiceRequired&&<button className="text-action" onClick={() => setSwitching('')}>{t('runtime.switchConnection')}</button>}</> : onConnections && (configured || !workerSettings) && <button className="text-action" onClick={onConnections}>{t('runtime.manageConnections')}<SettingsChevron/></button>}</div></div>
   {page === 'models' ? <section className="runtime-model-section">
    <div className="runtime-setting-row"><div><strong>{t('runtime.botConversation')}</strong><p>{t('runtime.botConversationHint')}</p></div>{view.conversation ? <ModelSummary value={view.conversation} models={view.models} runtimeDefault={view.runtimeDefault} label={` ${t('runtime.botConversationModel')}`} disabled={loading} onClick={anchor => edit('conversation', anchor)}/> : <span className="settings-note">{t('runtime.notConnected')}</span>}</div>
    {workerSettings ?? <>    <div className="runtime-setting-row"><div><strong>{t('runtime.workModel')}</strong><p>{t(caelis ? 'runtime.caelisWorkModelHint' : 'runtime.workModelHint')}</p></div>{(caelis ? view.main : view.work) ? <ModelSummary value={(caelis ? view.main : view.work)!} models={view.models} runtimeDefault={view.runtimeDefault} label={t('runtime.workModel')} disabled={loading || caelis && !view.canEditMain} onClick={anchor=>edit(caelis ? 'runtime' : 'work',anchor)}/> : <span className="settings-note">{t('runtime.notConnected')}</span>}</div>
    {caelis && !view.canEditMain && <p className="settings-note">{t('runtime.updateCaelisForMainModel')}</p>}
    {caelis && view.work && <div className="runtime-setting-row"><div><strong>{t('runtime.dedicatedWorkModel')}</strong><p>{t('runtime.dedicatedWorkModelHint')}</p></div><ModelSummary value={view.work} models={view.models} runtimeDefault={view.runtimeDefault} label={t('runtime.dedicatedWorkModel')} disabled={loading} onClick={anchor=>edit('work',anchor)}/></div>}</>}
   </section> : choiceRequired ? null : <>
    <div className="runtime-section-title"><h2>{t(caelis ? 'runtime.modelConnections' : 'runtime.accountGroup')}</h2>{configured && <button onClick={() => caelis ? setConnecting(true) : setSwitching('codex')}>{caelis ? t('runtime.addConnection') : t('runtime.manageAccount')}</button>}</div>
    {!configured ? <p className="settings-note">{choiceRequired ? t('runtime.chooseConnection') : t(setupStatusKey(view.setup))}</p> : caelis ? view.connections.length ? view.connections.map(group => <details className="runtime-connection-group" key={group.id}><summary><span><strong>{group.name}</strong><small>{group.kind === 'agent' ? t('runtime.externalAgent') : t('runtime.modelCount', { count: group.models.length })}</small></span><SettingsChevron/></summary>{group.models.map(model => <div className="runtime-connection-model" key={model.id}><div><strong>{model.name}</strong><p>{model.unavailable ? t('runtime.reauthNeeded') : model.uses.map(u => formatModelUse(u, t)).join(' · ') || t('runtime.notInUse')}</p></div>{group.kind === 'provider' && <span title={model.uses.length ? t('runtime.inUseByHint', { uses: model.uses.map(u => formatModelUse(u, t)).join(locale === 'zh-CN' ? '、' : ', ') }) : undefined}><button disabled={!!model.uses.length} aria-label={t('runtime.removeModelLabel', { label: model.name })} onClick={() => { setRemoving({ group, model }); setRemoveError(''); }}>{t('runtime.remove')}</button></span>}</div>)}{group.kind === 'agent' && <div className="setup-end"><button disabled={group.models.some(m => m.uses.length > 0)} onClick={() => { setRemoving({group,model:group.models[0]});setRemoveError(''); }}>{t('runtime.disconnectAgent')}</button></div>}</details>) : <div className="runtime-empty">{t('runtime.noConnections')}</div> : <p className="settings-note">{view.setup.accountType === 'chatgpt' ? t('runtime.connectedViaChatGPT') : view.setup.accountType === 'apiKey' ? t('runtime.connectedViaApiKey') : t('runtime.viewOrChangeCodexLogin')}</p>}
    {caelis && <details className="settings-disclosure runtime-advanced-settings"><summary><SettingsChevron/>{t('runtime.tabAdvanced')}</summary><TeamSettings team={view.team} models={view.team.models} client={client} onRefresh={() => refresh(true)}/></details>}
   </>}
   <div className="runtime-page-footer"><button className="text-action" disabled={loading} onClick={() => void refresh()}><ArrowClockwiseIcon aria-hidden="true" size={14}/>{t('runtime.refreshConfig')}</button>{(loading || notice) && <p className="settings-note" role="status">{loading ? t('runtime.refreshing') : notice}</p>}</div>
   {error && <p role="alert" className="inline-error">{error}</p>}
  </>}
  {editing && selection && view && <ModelPicker anchor={modelAnchor.current} runtimeDefault={view.runtimeDefault} title={editing === 'conversation' ? t('runtime.botConversationModel') : editing === 'runtime' ? t('runtime.caelisMainModel') : t('runtime.workModel')} description={editing === 'work' ? t('runtime.dedicatedWorkModelDescription') : undefined} value={selection} models={view.models} onReload={() => refresh(true)} inherited={editing !== 'runtime'} onSave={async value => { await client.saveModel(editing, value, view.revision); setNotice(t('runtime.modelSaved')); await refresh(); }} onClose={() => setEditing(null)} onConnect={caelis ? () => { setEditing(null); onConnections?.(); setConnecting(true); } : undefined}/>}
  {connecting && <ConnectionWizard client={client} active={active} onClose={() => setConnecting(false)} onConnected={async () => { setConnecting(false); await refresh(); }}/ >}
  {removing && <SettingsDialog title={removing.group.kind === 'agent' ? t('runtime.disconnectAgentPrompt', { name: removing.group.name }) : t('runtime.removeModelPrompt', { name: removing.model.name })} busy={busy} onClose={() => setRemoving(null)}><p className="settings-note">{removing.group.kind === 'agent' ? t('runtime.disconnectAgentNote', { count: removing.group.models.length }) : t('runtime.removeModelNote')}</p>{removeError && <p role="alert" className="inline-error">{removeError}</p>}<div className="setup-end"><button disabled={busy} onClick={() => { setRemoving(null); if (removeError) void refresh(); }}>{removeError ? t('runtime.closeAndRefresh') : t('common.cancel')}</button><button disabled={busy || !!removeError} onClick={() => void remove()}>{busy ? t('runtime.removing') : t('runtime.remove')}</button></div></SettingsDialog>}
  {switching !== null && <SettingsDialog busy={preparationBusy} title={switching ? (switching === 'caelis' ? t('runtime.manageCaelis') : t('runtime.manageCodex')) : t(choiceRequired ? 'runtime.chooseConnection' : 'runtime.switchRuntime')} description={switching || choiceRequired ? undefined : t('runtime.switchRuntimeDescription')} onClose={() => { setSwitching(null); void refresh(); }}>{switching ? preparation(switching, setPreparationBusy) : <div className="runtime-choices">{['caelis', 'codex'].map(id => <button key={id} onClick={() => setSwitching(id)}><div><strong>{id === 'caelis' ? 'Caelis' : 'Codex'}</strong><span>{!choiceRequired && id === view?.profile.runtime ? t('runtime.currentInUse') : id === 'caelis' ? t('runtime.caelisOptionDescription') : t('runtime.codexOptionDescription')}</span></div><SettingsChevron/></button>)}</div>}</SettingsDialog>}
 </section>;
}
