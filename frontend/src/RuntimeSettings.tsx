import { useEffect, useMemo, useRef, useState } from 'react';
import { backend, desktop } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';
import { ConnectionWizard } from './settings/runtime/ConnectionWizard';
import { createRuntimeSettingsClient } from './settings/runtime/client';
import { SettingsDialog } from './settings/runtime/SettingsDialog';
import { RuntimeWorkspace } from './settings/runtime/RuntimeWorkspace';
import type { RuntimeSettings as Profile, SetupState, SetupOverview, SetupRequest } from './backend/contract';
import { useI18n } from './i18n';

const names: Record<string, string> = { caelis: 'Caelis', codex: 'Codex' };
const empty = (runtime: string): Profile => ({ runtime, cliPath: '', caelisStore: '' });
const request = (settings: Profile, action: string, fields: Partial<SetupRequest> = {}): SetupRequest => ({ settings, action, provider: '', baseUrl: '', model: '', apiKey: '', ...fields });

export function RuntimeSettings({ onboarding = false, onDone }: { onboarding?: boolean; onDone?: () => void }) {
 return onboarding ? <RuntimePreparation onboarding onDone={onDone}/> : <RuntimeWorkspace preparation={(id, onBusy) => <RuntimePreparation initialRuntime={id} onBusy={onBusy}/>}/>;
}

export function RuntimePreparation({ onboarding = false, onDone, initialRuntime = '', onBusy, call = backend, host = desktop }: { onboarding?: boolean; onDone?: () => void; initialRuntime?: string; onBusy?: (busy: boolean) => void; call?: typeof backend; host?: typeof desktop }) {
 const { t } = useI18n();
 const errorText = (e: unknown) => e instanceof Error ? e.message : t('runtime.actionFailed');
 const [activeProfile, setActiveProfile] = useState<Profile | null>(null), [restartNeeded, setRestartNeeded] = useState(false);
 const [overview, setOverview] = useState<SetupOverview | null>(null), [id, setID] = useState(initialRuntime);
 const [profile, setProfile] = useState<Profile>(empty('')), [state, setState] = useState<SetupState | null>(null);
 const [busy, setBusy] = useState(''), [error, setError] = useState(''), [notice, setNotice] = useState(''), [manual, setManual] = useState(false);
 const [form, setForm] = useState<'model' | 'key' | ''>(''), [confirm, setConfirm] = useState<SetupRequest | null>(null);
 useEffect(() => { void call<Profile>('RuntimeSettings').then(setActiveProfile).catch(() => {}); void call<{ connection: string }>('ComposerSnapshot').then(s => { if (!['ready', 'connecting'].includes(s.connection)) setRestartNeeded(true); }).catch(() => {}); }, []);
 const epoch = useRef(0), working = useRef(false), alive = useRef(true);
 const connectionClient = useMemo(() => createRuntimeSettingsClient(call, profile), [profile]);
 const name = names[id] ?? '', blocked = !!busy || !!confirm;
 useEffect(() => { if (confirm) document.querySelector('.runtime-confirm')?.scrollIntoView({ block: 'nearest' }); }, [confirm]);
 useEffect(() => { onBusy?.(blocked || !!form || !!confirm); return () => onBusy?.(false); }, [blocked, form, confirm, onBusy]);
 const refreshOverview = async () => { const v = await call<SetupOverview>('SetupOverview'); if (alive.current) setOverview(v); return v; };
 useEffect(() => { alive.current = true; void refreshOverview().then(v => { if (!onboarding && !initialRuntime) setID(v.pending || v.active); }).catch(e => setError(errorText(e))); const close = () => setForm(''); window.addEventListener('settings-close', close); return () => { alive.current = false; epoch.current++; window.removeEventListener('settings-close', close); }; }, [onboarding]);
 useEffect(() => {
  if (!id) return;
  const generation = ++epoch.current; setState(null); setForm(''); setError(''); setNotice(''); setBusy('detect'); working.current = true;
  void call<Profile>('SetupProfile', id).then(async p => { if (generation !== epoch.current) return; setProfile(p); setManual(!!p.cliPath); const v = await call<SetupState>('InspectSetup', p); if (generation === epoch.current) setState(v); }).catch(e => { if (generation === epoch.current) setError(errorText(e)); }).finally(() => { if (generation === epoch.current) { working.current = false; setBusy(''); } });
 }, [id]);
 const run = async (action: string, fields: Partial<SetupRequest> = {}) => {
  if (working.current) return; working.current = true; setBusy(action); setError(''); setNotice('');
  try {
   const v = action === 'detect' ? await call<SetupState>('InspectSetup', profile) : await call<SetupState>('ApplySetup', request(profile, action, fields));
   if (alive.current) {
    setState(v); setForm('');
    if (action === 'detect') setNotice(v.state === 'ready' ? t('runtime.connectionCheckPassed') : v.message);
    if (['install', 'update', 'apply-update', 'start'].includes(action)) setNotice(id === 'caelis' ? t('runtime.connected') : v.message);
    if (action === 'check-update') setNotice(v.installation.updateState === 'available' ? '' : v.message);
    if (profile.runtime === overview?.active && (['login', 'api-key', 'logout'].includes(action) || profile.runtime === 'codex' && ['update', 'install'].includes(action))) setRestartNeeded(true);
    if (profile.runtime === 'caelis' && ['update', 'apply-update', 'start'].includes(action) && v.state === 'ready') setRestartNeeded(false);
   }
  }
  catch (e) { const message = errorText(e); if (alive.current) { setError(message); try { const current = await call<SetupState>('InspectSetup', profile); if (alive.current) setState(current); } catch { /* Keep the explicit operation failure. */ } } return message; }
  finally { working.current = false; if (alive.current) setBusy(''); }
 };
 useEffect(() => { if (!state?.loginPending) return; const timer = window.setInterval(() => { if (!working.current) void run('detect'); }, 2000); return () => clearInterval(timer); }, [state?.loginPending, profile]);
 const pick = async () => { try { const path = await host<string>('PickRuntimeCLI'); if (path) { setManual(true); setProfile(p => ({ ...p, cliPath: path })); setState(null); } } catch (e) { setError(errorText(e)); } };
 const activate = async () => {
  if (working.current) return; working.current = true; setBusy('activate'); setError('');
  try { await call('ActivateRuntime', profile); await refreshOverview(); await host('RestartForRuntime'); }
  catch (e) { setError(errorText(e)); working.current = false; setBusy(''); }
 };
 const skip = async () => { await call('DismissSetup'); onDone?.(); };
 if (onboarding && !id) return <section className="runtime-welcome">
  <img className="setup-avatar" src="/icons/caelis-avatar.png" alt=""/>
  <h1>{t('runtime.prepareCaelisBot')}</h1><p className="setup-lead">{t('runtime.chooseRuntimeLead')}</p>
  <div className="runtime-choices">{['caelis', 'codex'].map(value => <button key={value} onClick={() => setID(value)}><div><strong>{t('runtime.useRuntime', { name: names[value] })}</strong><span>{value === 'caelis' ? t('runtime.caelisOnboardingLead') : t('runtime.codexOnboardingLead')}</span></div><span aria-hidden="true">→</span></button>)}</div>
  <button className="text-action" onClick={() => void skip().catch(e => setError(errorText(e)))}>{t('runtime.setupLater')}</button>{error && <p role="alert" className="inline-error">{error}</p>}
 </section>;
 const serviceNeedsApply = id === 'caelis' && !!state?.installation.installed && (state.state === 'service' || state.serviceUpdateAvailable || state.state === 'incompatible' && state.installation.updateState === 'current');
 const updateAvailable = state?.installation.updateState === 'available';
 const switchDisabled = blocked || state?.state !== 'ready';
 const switchHint = blocked ? t('runtime.waitCurrentAction') : state?.state === 'incompatible' ? t('runtime.incompatibleServiceHint', { name }) : state?.state === 'ready' ? t('runtime.switchTakesEffectRestart', { name }) : state?.message || t('runtime.switchAvailableAfterInstall');
 return <section className={onboarding ? 'runtime-onboarding' : 'runtime-management'} aria-label={t('runtime.runtimeSettingsAria')}>
  {onboarding ? <><button className="text-action setup-back" disabled={blocked} onClick={() => setID('')}>{t('runtime.chooseOtherRuntime')}</button><h1>{t('runtime.prepareRuntime', { name })}</h1><p className="setup-lead">{t('runtime.prepareRuntimeLead', { name })}</p></> : <>
   {id && overview && id !== overview.active && <div className="runtime-selection"><p className="settings-note">{t('runtime.switchAfterInstallNote', { name })}</p><span className="runtime-switch" title={switchHint}><button className="primary" disabled={switchDisabled} onClick={() => setConfirm(request(profile, 'switch'))}>{t('runtime.switchToRuntime', { name })}</button></span></div>}
  </>}
  <div className="runtime-program">
   <dl className="runtime-version-list">
    <div><dt>{t('runtime.installed')}</dt><dd>{state?.installation.installed ? state.installation.version : busy === 'detect' ? t('runtime.detecting') : t('runtime.notInstalled')}</dd></div>
    {id === 'caelis' && state?.installation.installed && <div><dt>{t('runtime.running')}</dt><dd><span className={`runtime-status-dot ${state.state === 'ready' || state.state === 'models' ? 'ready' : ''}`} aria-hidden="true"/>{state.serviceState === 'running' ? state.serviceVersion || t('runtime.unknownVersion') : state.state === 'service' ? t('runtime.notStarted') : t('runtime.notConnected')}</dd></div>}
   </dl>
   {serviceNeedsApply && <p className="runtime-update-hint">{state?.state === 'service' ? t('runtime.readyAfterServiceStart') : t('runtime.newVersionNotActivated')}</p>}
   <div className="runtime-program-actions">
    {!state?.installation.installed ? <button className="primary" disabled={blocked || !state} onClick={() => void run('install')}>{t('runtime.installRuntime', { name })}</button> : serviceNeedsApply ? <button className="primary" disabled={blocked} onClick={() => setConfirm(request(profile, 'apply-update'))}>{t('runtime.applyInstalledVersion')}</button> : updateAvailable ? <button className="primary" disabled={blocked} onClick={() => setConfirm(request(profile, 'update'))}>{id === 'caelis' ? t('runtime.updateAndReconnect') : t('runtime.updateCodex')}</button> : <button disabled={blocked} onClick={() => void run('check-update')}>{t('runtime.checkUpdate')}</button>}
    <button className="text-action" disabled={blocked || !id} onClick={() => void run('detect')}>{t('runtime.recheck')}</button>
   </div>
   {updateAvailable && <p className="settings-note">{t('runtime.newVersionAvailable', { version: state?.installation.latestVersion ?? '' })}</p>}
  </div>
  {state?.installation.installed && state.state !== 'service' && state.state !== 'incompatible' && state.state !== 'unavailable' && <>
   {id === 'codex' && <SettingGroup title={t('runtime.accountGroup')}><SettingRow label={state.loginPending ? t('runtime.waitingBrowserLogin') : state.state === 'ready' ? t('runtime.connectedCodex') : t('runtime.loginCodex')} description={state.state === 'ready' ? (state.accountType === 'chatgpt' ? t('runtime.chatgptAccount') : state.accountType === 'apiKey' ? t('runtime.useOpenAIApiKey') : t('runtime.codexConfigConnection')) : undefined}>
    <div className="runtime-actions">{state.loginPending ? <><button disabled={blocked} onClick={() => void run('login')}>{t('runtime.openLoginPage')}</button><button disabled={blocked} onClick={() => void run('cancel-login')}>{t('common.cancel')}</button></> : <><button disabled={blocked} onClick={() => void run('login')}>{state.state === 'ready' ? t('runtime.relogin') : t('runtime.loginWithChatGPT')}</button>{state.state === 'ready' && <button disabled={blocked} onClick={() => setConfirm(request(profile, 'logout'))}>{t('runtime.logout')}</button>}</>}</div>
   </SettingRow>{!state.loginPending && <SettingRow label={t('runtime.useOpenAIApiKey')}><button disabled={blocked} onClick={() => setForm('key')}>{t('runtime.useApiKey')}</button></SettingRow>}</SettingGroup>}
   {id === 'caelis' && (onboarding || !state.models.length) && <SettingGroup title={t('runtime.modelConnections')}><SettingRow label={state.models.length ? t('runtime.connectedModels') : t('runtime.noModelsConnected')}><button disabled={blocked} onClick={() => setForm('model')}>{t('runtime.connectNewModel')}</button></SettingRow>{state.models.map(m => <SettingRow key={m.value} label={m.label} description={m.noAuth ? t('runtime.needsAuth') : m.value === state.selectedModel ? t('runtime.inUseByBot') : m.current ? t('runtime.caelisDefaultModel') : undefined}><div className="runtime-actions">{onboarding && <button disabled={blocked || m.noAuth || m.value === state.selectedModel} onClick={() => void run('use-model', { model: m.value })}>{t('runtime.use')}</button>}<button disabled={blocked || m.current || m.value === state.selectedModel} aria-label={t('runtime.removeModelLabel', { label: m.label })} onClick={() => setConfirm(request(profile, 'remove-model', { model: m.value }))}>{t('runtime.remove')}</button></div></SettingRow>)}</SettingGroup>}
   {onboarding && id === 'codex' && state.state === 'ready' && state.models.length > 0 && <SettingGroup title={t('runtime.model')}><SettingRow label={t('runtime.botUseModel')} description={t('runtime.botUseModelDescription')}><select aria-label={t('runtime.botUseModel')} disabled={blocked} value={state.selectedModel} onChange={e => void run('use-model', { model: e.target.value })}><option value="" disabled>{t('runtime.selectModel')}</option>{state.models.map(m => <option key={m.value} value={m.value}>{m.label}</option>)}</select></SettingRow></SettingGroup>}
  </>}
  <details className="runtime-advanced" open={manual || undefined}><summary>{t('runtime.programAndDataDirectory')}</summary>
   <SettingGroup><SettingRow label={t('runtime.programLocation')} description={state?.installation.path || t('runtime.autoDiscovery')}><button disabled={blocked} onClick={() => { setManual(v => !v); if (manual) { setProfile(p => ({ ...p, cliPath: '' })); setState(null); } }}>{manual ? t('runtime.useAutoDiscovery') : t('runtime.change')}</button></SettingRow>
    {manual && <div className="setup-path-field"><input aria-label={t('runtime.programPath')} disabled={blocked} value={profile.cliPath} placeholder={t('runtime.programPathPlaceholder')} onChange={e => { setProfile(p => ({ ...p, cliPath: e.target.value })); setState(null); }}/><button disabled={blocked} onClick={() => void pick()}>{t('runtime.chooseFile')}</button></div>}
    {id === 'caelis' && <SettingRow label={t('runtime.caelisDataDirectory')}><input aria-label={t('runtime.caelisDataDirectory')} disabled={blocked} value={profile.caelisStore ?? ''} placeholder={t('runtime.caelisDataDirectoryPlaceholder')} onChange={e => { setProfile(p => ({ ...p, caelisStore: e.target.value })); setState(null); }}/></SettingRow>}
    <SettingRow label={t('runtime.installAndUninstall')}><button onClick={() => void call('OpenMessageLink', id === 'caelis' ? 'https://caelis.dev' : 'https://learn.chatgpt.com/docs/codex/cli')}>{t('runtime.viewOfficialGuide')}</button></SettingRow>
   </SettingGroup>
  </details>
  {busy && <p className="settings-note" role="status">{busy === 'install' ? t('runtime.downloadingAndInstalling') : busy === 'update' ? (id === 'caelis' ? t('runtime.updatingCaelisNotice') : t('runtime.updatingCodexNotice')) : busy === 'apply-update' ? t('runtime.activatingServiceNotice') : busy === 'activate' ? t('runtime.preparingRestartNotice') : t('runtime.processing')}</p>}
  {error ? <p className="inline-error" role="alert">{error}</p> : notice ? <p className="settings-note" role="status">{notice}</p> : state?.message && !serviceNeedsApply && !['ready', 'models'].includes(state.state) && (state.state !== 'incompatible' || onboarding || overview?.active === id) && <p className="settings-note" role="status">{state.state === 'incompatible' ? t('runtime.updateCaelisToContinue') : state.message}</p>}
  {state?.state === 'ready' && (onboarding || (overview?.active === id && (restartNeeded || overview.pending || !!activeProfile && (profile.cliPath !== activeProfile.cliPath || (profile.caelisStore ?? '') !== (activeProfile.caelisStore ?? ''))))) && <div className="setup-end">{!onboarding && <span className="settings-note">{t('runtime.restartRequiredNote')}</span>}<button className={onboarding ? 'primary' : undefined} disabled={blocked} onClick={() => void activate()}>{onboarding ? t('runtime.restartAndStart') : t('runtime.restartAndApply')}</button></div>}
  {form === 'model' && <ConnectionWizard client={connectionClient} onClose={() => setForm('')} onConnected={async () => { setForm(''); await run('detect'); }}/>}
  {form === 'key' && <APIKeyForm busy={blocked} onClose={() => setForm('')} onSubmit={key => run('api-key', { apiKey: key })}/>}
  {confirm && <section className="runtime-confirm" role="alertdialog" aria-modal="false" aria-labelledby="setup-confirm-title"><h3 id="setup-confirm-title">{confirm.action === 'switch' ? t('runtime.confirmSwitchTitle', { name }) : confirm.action === 'logout' ? t('runtime.confirmLogoutTitle') : confirm.action === 'update' ? (id === 'caelis' ? t('runtime.confirmUpdateCaelisTitle') : t('runtime.confirmUpdateCodexTitle')) : confirm.action === 'apply-update' ? t('runtime.confirmApplyCaelisTitle') : t('runtime.confirmRemoveModelTitle')}</h3><p>{confirm.action === 'switch' ? t('runtime.confirmSwitchBody') : ['update', 'apply-update'].includes(confirm.action) && id === 'caelis' ? t('runtime.confirmUpdateCaelisBody') : confirm.action === 'update' ? t('runtime.confirmUpdateCodexBody') : t('runtime.confirmAffectsOtherAppsBody')}</p><div className="setup-end"><button autoFocus onClick={() => setConfirm(null)}>{t('common.cancel')}</button><button className="primary" onClick={() => { const r = confirm; setConfirm(null); if (r.action === 'switch') void activate(); else void run(r.action, r); }}>{confirm.action === 'switch' ? t('runtime.switchAndRestart') : confirm.action === 'update' ? (id === 'caelis' ? t('runtime.applyAndReconnect') : t('runtime.update')) : confirm.action === 'apply-update' ? t('runtime.applyAndReconnect') : t('runtime.confirm')}</button></div></section>}
 </section>;
}

function APIKeyForm({ busy, onClose, onSubmit }: { busy: boolean; onClose: () => void; onSubmit: (key: string) => Promise<string | undefined> }) {
 const { t } = useI18n();
 const [key, setKey] = useState(''), [error, setError] = useState('');
 return <SettingsDialog title={t('runtime.useOpenAIApiKey')} description={t('runtime.apiKeySavedByCodex')} busy={busy} onClose={() => { setKey(''); onClose(); }}>
  <form onSubmit={e => { e.preventDefault(); const secret = key; setKey(''); void onSubmit(secret).then(message => { if (message) setError(message); }); }}>
   <label>API Key<input type="password" required disabled={busy} value={key} autoComplete="off" spellCheck={false} onChange={e => setKey(e.target.value)}/></label>
   {error && <p role="alert" className="inline-error">{error}</p>}
   <div className="setup-end"><button type="button" disabled={busy} onClick={onClose}>{t('common.cancel')}</button><button className="primary" disabled={busy || !key.trim()}>{busy ? t('runtime.connecting') : t('runtime.connect')}</button></div>
  </form>
 </SettingsDialog>;
}
