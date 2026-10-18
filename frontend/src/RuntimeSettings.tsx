import {Recovery} from './SurfaceBoundary';
import appIcon from '../../internal/desktop/assets/app-icon.png';
import { ExistingSetupModels } from './settings/runtime/ExistingSetupModels';
import { OnboardingProviders } from './settings/runtime/OnboardingProviders';
import type { ConnectionKind, ModelSelection } from './settings/runtime/types';
import { SetupBack } from './SetupBack';
import { SetupButton, SetupLoading } from './SetupLoading';
import { existingModelChoice, selectExistingModel } from './settings/runtime/onboarding-models';
import { presentationWarning } from './presentation-warning';
import { CubeIcon } from '@phosphor-icons/react/dist/csr/Cube';
import { TerminalIcon } from '@phosphor-icons/react/dist/csr/Terminal';
import { StarIcon } from '@phosphor-icons/react/dist/csr/Star';
import { SettingsChevron } from './SettingsIcons';
import { useEffect, useMemo, useRef, useState } from 'react';
import { backend, desktop } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';
import { ConnectionWizard } from './settings/runtime/ConnectionWizard';
import { createRuntimeSettingsClient } from './settings/runtime/client';
import { SettingsDialog } from './settings/runtime/SettingsDialog';
import { ConnectionsPage } from './settings/runtime/ConnectionsPage';
import type { RuntimeSettings as Profile, SetupState, SetupOverview, SetupRequest } from './backend/contract';
import { useI18n } from './i18n';
import {setupStatusKey} from './settings/runtime/setup-status';

const names: Record<string, string> = { caelis: 'built-in', codex: 'Codex' };
const empty = (runtime: string): Profile => ({ runtime, cliPath: '', caelisStore: '' });
const request = (settings: Profile, action: string, fields: Partial<SetupRequest> = {}): SetupRequest => ({ settings, action, provider: '', baseUrl: '', model: '', apiKey: '', ...fields });

export function RuntimeSettings({ onboarding = false, onBack,onDone,onActivating,active=true,refreshKey=0 }: { onboarding?: boolean; onBack?:()=>void; onDone?: () => void; onActivating?:(work:()=>Promise<void>)=>void; active?:boolean; refreshKey?:number }) {
 return onboarding ? <RuntimePreparation onboarding active={active} onBack={onBack} onDone={onDone} onActivating={onActivating}/> : <ConnectionsPage active={active} refreshKey={refreshKey}/>;
}

export function RuntimePreparation({ onboarding = false, active=true, onBack,onDone,onActivating, initialRuntime = '', onBusy, call = backend, host = desktop }: { onboarding?: boolean; active?:boolean; onBack?:()=>void; onDone?: () => void; onActivating?:(work:()=>Promise<void>)=>void; initialRuntime?: string; onBusy?: (busy: boolean) => void; call?: typeof backend; host?: typeof desktop }) {
 const { t } = useI18n();
 const errorText = (_e: unknown) => 'unavailable';
 const [activeProfile, setActiveProfile] = useState<Profile | null>(null), [reconnectNeeded, setReconnectNeeded] = useState(false);
 const [overview, setOverview] = useState<SetupOverview | null>(null), [id, setID] = useState(initialRuntime);
 const [profile, setProfile] = useState<Profile>(empty('')), [state, setState] = useState<SetupState | null>(null);
 const [installProgress,setInstallProgress]=useState<{phase:string;downloaded:number;total:number;bytesPerSecond:number}|null>(null);
 const cancelRequested=useRef(false);
 const [installCancelled,setInstallCancelled]=useState(false);
 const [installOperation,setInstallOperation]=useState(''),[installError,setInstallError]=useState('');
 const [busy, setBusy] = useState(''), [error, setError] = useState(''), [notice, setNotice] = useState(''), [manual, setManual] = useState(false), [locationOpen,setLocationOpen] = useState(false);
 const [providerSelection,setProviderSelection]=useState<{kind:ConnectionKind;id:string}|null>(null);
 const [draftModel,setDraftModel]=useState('');
 const [form, setForm] = useState<'model' | 'key' | ''>(''), [confirm, setConfirm] = useState<SetupRequest | null>(null);
 const [preparationAttempt,setPreparationAttempt] = useState(0), [preparationFailed,setPreparationFailed] = useState(false);
 useEffect(() => { void call<Profile>('RuntimeSettings').then(setActiveProfile).catch(() => {}); void call<{ connection: string }>('ComposerSnapshot').then(s => { if (!['ready', 'connecting'].includes(s.connection)) setReconnectNeeded(true); }).catch(() => {}); }, []);
 const epoch = useRef(0), working = useRef(false), alive = useRef(true);
 useEffect(()=>{
  if(!active||id!=='caelis'||busy!=='install')return;
  let live=true;const read=()=>void call<{phase:string;downloaded:number;total:number;bytesPerSecond:number}>('SetupInstallProgress').then(p=>{if(live)setInstallProgress(p)}).catch(()=>{});
  read();const timer=setInterval(read,750);return()=>{live=false;clearInterval(timer)};
 },[active,id,busy,call]);
 const measured=!!installProgress?.total&&installProgress.phase==='downloading';
 const progressPercent=measured?Math.min(100,100*installProgress!.downloaded/installProgress!.total):undefined;
 const progressAmount=installProgress?.downloaded?`${(installProgress.downloaded/1048576).toFixed(1)} MB${installProgress.total?` / ${(installProgress.total/1048576).toFixed(1)} MB`:''} · ${((installProgress.bytesPerSecond||0)/1048576).toFixed(1)} MB/s`:'';
 const connectionClient = useMemo(() => createRuntimeSettingsClient(call, profile), [profile]);
 const name = onboarding ? (id === 'caelis' ? 'Caelis CLI' : 'Codex CLI') : names[id] ?? '', blocked = !!busy || !!confirm;
 const selectedActive = !!overview?.hasRuntimeChoice && overview.active === id;
 useEffect(() => { if (confirm) document.querySelector('.runtime-confirm')?.scrollIntoView({ block: 'nearest' }); }, [confirm]);
 useEffect(() => { onBusy?.(blocked || !!form || !!confirm || locationOpen); return () => onBusy?.(false); }, [blocked, form, confirm, locationOpen, onBusy]);
 const refreshOverview = async () => { const v = await call<SetupOverview>('SetupOverview'); if (alive.current) setOverview(v); return v; };
 useEffect(() => { alive.current = true; void refreshOverview().then(v => { if (!onboarding && !initialRuntime) setID(v.pending || (v.hasRuntimeChoice ? v.active : '')); }).catch(e => setError(errorText(e))); const close = () => setForm(''); window.addEventListener('settings-close', close); return () => { alive.current = false; epoch.current++; window.removeEventListener('settings-close', close); }; }, [onboarding]);
 useEffect(() => {
  if (!id) return;
  const generation = ++epoch.current; setState(null); setDraftModel(''); setForm(''); setError(''); setNotice(''); setBusy('detect'); setPreparationFailed(false); working.current = true;
  void call<Profile>('SetupProfile', id).then(async p => {
   if (generation !== epoch.current) return;
   setProfile(p); setManual(!!p.cliPath);
   const next = onboarding ? await call<SetupState>('ApplySetup', request(p, 'prepare')) : await call<SetupState>('InspectSetup', p);
   if (generation !== epoch.current) return;
   setState(next);
  }).catch(e => { if (generation === epoch.current) { setError(errorText(e)); setPreparationFailed(true); } }).finally(() => { if (generation === epoch.current) { working.current = false; setBusy(''); } });
 }, [id, preparationAttempt]);
 const run = async (action: string, fields: Partial<SetupRequest> = {}) => {
  if (working.current) return; working.current = true; setBusy(action); setError(''); setNotice('');
  if(['install','update'].includes(action)){cancelRequested.current=false;setInstallCancelled(false);setInstallOperation(action);setInstallError('');setInstallProgress(null);}
  try {
   const v = action === 'detect' ? await call<SetupState>('InspectSetup', profile) : await call<SetupState>('ApplySetup', request(profile, action, fields));
   if (alive.current) {
    setState(v); setForm('');
    if(['install','update'].includes(action))setInstallOperation('');
    if (action === 'detect' && !onboarding) setNotice(v.state === 'ready' ? t('runtime.connectionCheckPassed') : t(setupStatusKey(v)));
    if (['install', 'update', 'apply-update', 'start'].includes(action)) setNotice(t(setupStatusKey(v)));
    if (action === 'check-update') setNotice(v.installation.updateState === 'available' ? '' : t(setupStatusKey(v)));
    if (overview?.hasRuntimeChoice && profile.runtime === overview.active && (['login', 'api-key', 'logout'].includes(action) || profile.runtime === 'codex' && ['update', 'install'].includes(action))) setReconnectNeeded(true);
    if (profile.runtime === 'caelis' && ['update', 'apply-update', 'start'].includes(action) && v.state === 'ready') setReconnectNeeded(false);
   }
  }
  catch (e) { const message = errorText(e); if (alive.current) { if(['install','update'].includes(action)){setInstallError(cancelRequested.current?t('settings.runtimeInstallCancelled'):message);setInstallCancelled(cancelRequested.current);}else setError(message); try { const current = await call<SetupState>('InspectSetup', profile); if (alive.current) setState(current); } catch { /* Keep the explicit operation failure. */ } } return message; }
  finally { working.current = false; if (alive.current) setBusy(''); }
 };
 useEffect(() => { if (!active||!state?.loginPending) return; const timer = window.setInterval(() => { if (!working.current) void run('detect'); }, 2000); return () => clearInterval(timer); }, [active,state?.loginPending, profile]);
 const pick = async () => { try { const path = await host<string>('PickRuntimeCLI'); if (path) { setManual(true); setProfile(p => ({ ...p, cliPath: path })); setState(null); } } catch (e) { setError(errorText(e)); } };
 const activate = async (model?:string) => {
  if (working.current) return; working.current = true; setBusy('activate'); setError('');
  try {
   if(model&&state)await selectExistingModel(call,profile,state,model);
   if(onboarding&&onActivating){onActivating(async()=>{await call('ActivateRuntime',profile)});return;}
   await call('ActivateRuntime', profile);
   if(onboarding)void refreshOverview().catch(()=>presentationWarning('background_read_failed'));else await refreshOverview();
   setActiveProfile(profile); setReconnectNeeded(false); setNotice(t('runtime.connectionCheckPassed')); onDone?.();
  }
  catch (e) { setError(errorText(e)); }
  finally { working.current = false; if (alive.current) setBusy(''); }
 };
 const finishOnboardingConnection = async (model?:string, parameters?:ModelSelection) => {
  if(!model)throw new Error(t('runtime.selectModel'));
  const catalog=await call<SetupState>('InspectSetup',profile);
  const available=catalog.models.filter(m=>!m.noAuth);
  const matching=available.filter(m=>m.value===model||m.label===model);
  const added=available.filter(m=>!state?.models.some(old=>old.value===m.value));
  const selected=matching.length===1?matching[0]:added.length===1?added[0]:undefined;
  if(!selected)throw new Error(t('runtime.selectModel'));
  const next=await call<SetupState>('ApplySetup',request(profile,'use-model',{model:selected.value}));
  if(next.state!=='ready')throw new Error(t(setupStatusKey(next)));
  setState(next);
  const complete=async()=>{
  await call('ActivateRuntime',profile);
  // Resolve the connected model ID before persisting the user's onboarding parameters.
  if(parameters){
   const view=await connectionClient.read();
   const option=view.models.find(m=>m.model===selected.value);
   const effort=parameters.effort||option?.defaultEffort||'';
   if(parameters.effort&&!option?.efforts.includes(parameters.effort)||parameters.serviceTier&&!option?.serviceTiers.some(t=>t.id===parameters.serviceTier))throw new Error(t('runtime.modelUnavailable'));
   await connectionClient.saveModel('conversation',{model:selected.value,effort,serviceTier:parameters.serviceTier});
  }
  };
  if(onActivating){onActivating(complete);return;}
  await complete();
  void refreshOverview().catch(()=>presentationWarning('background_read_failed'));
  setForm('');onDone?.();
 };
 if(onboarding&&form==='model'&&providerSelection)return <ConnectionWizard inline onboarding initialKind={providerSelection.kind} initialChoice={providerSelection.id} client={connectionClient} active={active} onClose={()=>setForm('')} onConnected={finishOnboardingConnection}/>;
 if (onboarding && !id) return <section className="runtime-welcome">
  <header className="onboarding-route-heading"><img className="setup-avatar" src="/icons/caelis-avatar.png" alt=""/><div><h1>{t('runtime.prepareCaelisBot')}</h1><p className="setup-lead">{t('settings.setupRuntimeLead')}</p></div></header>
  <div className="runtime-choices onboarding-routes">{['caelis', 'codex'].map(value => <button key={value} className={value === 'caelis' ? 'onboarding-route-primary' : 'onboarding-route-secondary'} onClick={() => setID(value)}><span className="onboarding-route-icon" aria-hidden="true">{value === 'caelis' ? <CubeIcon size={28} weight="duotone"/> : <TerminalIcon size={28} weight="bold"/>}</span><div className="onboarding-route-copy"><div className="onboarding-route-title"><strong>{value === 'caelis' ? t('runtime.builtinOnboardingTitle') : 'Codex'}</strong>{value === 'caelis' && <span className="onboarding-recommended"><StarIcon size={12} weight="fill" aria-hidden="true"/>{t('runtime.onboardingRecommended')}</span>}</div><span>{t(value === 'caelis' ? 'runtime.caelisOnboardingLead' : 'runtime.codexOnboardingLead')}</span></div><SettingsChevron/></button>)}</div>
  {error && <Recovery/>}
  <div className="setup-end">{onBack&&<button type="button" onClick={onBack}>{t('settings.setupBack')}</button>}</div>
 </section>;
 if (onboarding && (!state||profile.runtime!==id)) return preparationFailed ? <section className="runtime-onboarding">
  <SetupBack onClick={() => setID('')}/>
  <button className="primary" onClick={() => setPreparationAttempt(v => v + 1)}>{t('settings.runtimeTryAgain')}</button>
 </section> : <SetupLoading/>;
 const serviceNeedsApply = id === 'caelis' && !!state?.installation.installed && !(onboarding && state.installation.bundled && ['ready','models'].includes(state.state)) && (state.state === 'service' || state.serviceUpdateAvailable || state.state === 'incompatible' && state.installation.updateState === 'current');
 const updateAvailable = !state?.installation.bundled && state?.installation.updateState === 'available';
 const {models:existingModels,selected:selectedModel}=existingModelChoice(state,draftModel);
 const reuseModels = onboarding && existingModels.length>0;
 if(onboarding&&state&&!state.loginPending&&['ready','models'].includes(state.state)) {
  if(id==='caelis'&&!reuseModels)return <OnboardingProviders client={connectionClient} disabled={blocked} onBack={()=>setID('')} onSelect={(kind,id)=>{setProviderSelection({kind,id});setForm('model')}}/>;
  return <section className="runtime-onboarding">
   <SetupBack disabled={blocked} onClick={()=>setID('')}/>
   <header><h1>{t('settings.setupChooseModel')}</h1><p className="setup-lead">{t('settings.setupChooseModelHint')}</p></header>
   <div className="onboarding-models"><ExistingSetupModels models={existingModels} selectedModel={selectedModel} disabled={blocked} busy={busy==='activate'} onSelect={setDraftModel} onContinue={()=>void activate(selectedModel)}/></div>
   {!existingModels.length&&<div className="setup-end"><SetupButton busy={busy==='detect'} disabled={blocked} onClick={()=>void run('detect')}>{t('runtime.recheck')}</SetupButton></div>}
  </section>;
 }
 const oauthPage = onboarding && id==='codex' && !!state?.installation.installed && (state.state==='login'||!!state.loginPending);
 const switchDisabled = blocked || state?.state !== 'ready';
 const switchHint = blocked ? t('runtime.waitCurrentAction') : state?.state === 'incompatible' ? t('runtime.incompatibleServiceHint', { name }) : state?.state === 'ready' ? t('runtime.switchTakesEffectRestart', { name }) : state ? t(setupStatusKey(state)) : t('runtime.switchAvailableAfterInstall');
 return <section className={onboarding ? 'runtime-onboarding' : 'runtime-management'} aria-label={t('runtime.runtimeSettingsAria')}>
  {onboarding ? <><SetupBack disabled={blocked} onClick={() => setID('')}/>
   {!oauthPage&&<header className={!state || !state.installation.installed ? 'setup-install-heading' : undefined}>
    {(!state || !state.installation.installed) && <span className="setup-install-icon" aria-hidden="true">{id === 'caelis' ? <CubeIcon size={30} weight="duotone"/> : <TerminalIcon size={30} weight="bold"/>}</span>}
    <div><h1>{id==='caelis'&&state?.installation.installed?t(reuseModels?'settings.setupChooseModel':'settings.setupConnectModel'):!state ? t('settings.setupCheckingTitle') : !state.installation.installed ? t('runtime.onboardingPrepareTitle') : state.state==='ready' ? t('settings.setupReadyTitle') : id==='caelis' ? t('runtime.modelConnections') : t('runtime.loginWithChatGPT')}</h1><p className="setup-lead">{t(id==='caelis'&&state?.installation.installed?(reuseModels?'settings.setupChooseModelHint':'settings.setupConnectModelHint'):!state?'settings.setupCheckingHint':!state.installation.installed?(id==='caelis'?'runtime.onboardingInstallCaelisLead':'runtime.onboardingInstallCodexLead'):state.state==='ready'?'settings.setupReadyHint':id==='caelis'?'settings.setupModelsHint':'settings.setupLoginHint')}</p></div>
   </header>}</> : <>
   {id && overview?.hasRuntimeChoice && id !== overview.active && <div className="runtime-selection"><p className="settings-note">{t('runtime.switchAfterInstallNote', { name })}</p><span className="runtime-switch" title={switchHint}><button className="primary" disabled={switchDisabled} onClick={() => setConfirm(request(profile, 'switch'))}>{t('runtime.switchToRuntime', { name })}</button></span></div>}
  </>}
  {(!onboarding || !state || !state.installation.installed || serviceNeedsApply || ['incompatible','unavailable'].includes(state.state))&&<div className={onboarding && (!state || !state.installation.installed) ? "runtime-program setup-install-card" : "runtime-program"}>
   {onboarding && (!state || !state.installation.installed) && <div className="setup-install-summary"><strong>{name}</strong><p>{t(!state ? 'runtime.detecting' : 'runtime.onboardingInstallRequired')}</p></div>}
   {!onboarding&&<dl className="runtime-version-list">
    <div><dt>{t('runtime.installed')}</dt><dd>{state?.installation.installed ? state.installation.version : busy === 'detect' ? t('runtime.detecting') : t('runtime.notInstalled')}</dd></div>
    {id === 'caelis' && state?.installation.installed && <div><dt>{t('runtime.running')}</dt><dd><span className={`runtime-status-dot ${state.state === 'ready' || state.state === 'models' ? 'ready' : ''}`} aria-hidden="true"/>{state.serviceState === 'running' ? state.serviceVersion || t('runtime.unknownVersion') : state.state === 'service' ? t('runtime.notStarted') : t('runtime.notConnected')}</dd></div>}
   </dl>}
   {serviceNeedsApply && !(onboarding && state?.state==='service') && <p className="runtime-update-hint">{state?.state === 'service' ? t('runtime.readyAfterServiceStart') : t('runtime.newVersionNotActivated')}</p>}
   <div className="runtime-program-actions">
    {!state ? <span role="status" className="settings-note">{t('runtime.detecting')}</span> : !state.installation.installed ? <button className="primary" disabled={blocked || !state} onClick={() => void run('install')}>{onboarding ? t('runtime.onboardingInstallAction') : t('runtime.installRuntime', { name })}</button> : serviceNeedsApply ? <button className="primary" disabled={blocked} onClick={() => onboarding && state.state==='service' ? void run('start') : setConfirm(request(profile, 'apply-update'))}>{t(onboarding && state.state==='service'?'runtime.prepareConnection':'runtime.applyInstalledVersion')}</button> : updateAvailable ? <button className="primary" disabled={blocked} onClick={() => setConfirm(request(profile, 'update'))}>{id === 'caelis' ? t('runtime.updateAndReconnect') : t('runtime.updateCodex')}</button> : !onboarding && !state.installation.bundled ? <button disabled={blocked} onClick={() => void run('check-update')}>{t('runtime.checkUpdate')}</button> : <span className="permission-badge">{t(state.state==='ready'?'runtime.setupReady':'runtime.installed')}</span>}
    <button className="text-action" disabled={blocked || !id} onClick={() => void run('detect')}>{t('runtime.recheck')}</button>
   </div>
   {state?.installation.bundled && <p className="settings-note">{t('runtime.bundledUpdateHint')}</p>}
   {updateAvailable && <p className="settings-note">{t('runtime.newVersionAvailable', { version: state?.installation.latestVersion ?? '' })}</p>}
  </div>}
  {state?.installation.installed && state.state !== 'service' && state.state !== 'incompatible' && state.state !== 'unavailable' && <>
   {id === 'codex' && !onboarding && <SettingGroup title={t('runtime.accountGroup')}><SettingRow label={state.loginPending ? t('runtime.waitingBrowserLogin') : state.state === 'ready' ? t('runtime.accountReady') : t('runtime.loginCodex')} description={state.state === 'ready' ? (state.accountType === 'chatgpt' ? t('runtime.chatgptAccount') : state.accountType === 'apiKey' ? t('runtime.useOpenAIApiKey') : t('runtime.codexConfigConnection')) : undefined}>
    <div className="runtime-actions">{state.loginPending ? <><button disabled={blocked} onClick={() => void run('login')}>{t('runtime.openLoginPage')}</button><button disabled={blocked} onClick={() => void run('cancel-login')}>{t('common.cancel')}</button></> : <><button disabled={blocked} onClick={() => void run('login')}>{state.state === 'ready' ? t('runtime.relogin') : t('runtime.loginWithChatGPT')}</button>{!onboarding && state.state === 'ready' && <button disabled={blocked} onClick={() => setConfirm(request(profile, 'logout'))}>{t('runtime.logout')}</button>}</>}</div>
   </SettingRow>{!state.loginPending && <SettingRow label={t('runtime.useOpenAIApiKey')}><button disabled={blocked} onClick={() => setForm('key')}>{t('runtime.useApiKey')}</button></SettingRow>}</SettingGroup>}
   {oauthPage && <section className="onboarding-oauth" aria-labelledby="onboarding-oauth-title">
    <img className="onboarding-oauth-app-icon" src={appIcon} alt="Caelis Bot"/>
    <h1 id="onboarding-oauth-title">{t(state.loginPending?'runtime.waitingBrowserLogin':'runtime.loginWithChatGPT')}</h1>
    <p className="setup-lead">{t(state.loginPending?'runtime.oauthPendingHint':'runtime.oauthStartHint')}</p>
    <div className="onboarding-oauth-actions"><SetupButton className="primary" busy={busy==='login'} disabled={blocked} onClick={()=>void run('login')}>{t(state.loginPending?'runtime.openLoginPage':'runtime.oauthBrowserAction')}</SetupButton>{state.loginPending&&<button className="text-action" disabled={blocked} onClick={()=>void run('cancel-login')}>{t('common.cancel')}</button>}</div>
    <p className="onboarding-oauth-footnote">{t('runtime.oauthContinueHint')}</p>
    {error&&<Recovery/>}
   </section>}

   {id === 'caelis' && (onboarding || !state.models.length) && <div className="onboarding-models">
    {onboarding ? <OnboardingProviders client={connectionClient} disabled={blocked} onSelect={(kind,id)=>{setProviderSelection({kind,id});setForm('model')}}/> : <button className="primary" disabled={blocked} onClick={()=>setForm('model')}>{t('runtime.connectNewModel')}</button>}
   </div>}

  </>}
  {!onboarding&&<button className="text-action runtime-location-entry" disabled={blocked} onClick={event=>{event.currentTarget.focus();setLocationOpen(true);}}>{t(onboarding?'runtime.advancedConnection':'runtime.programAndDataDirectory')}</button>}
  {locationOpen&&<SettingsDialog title={t(onboarding?'runtime.advancedConnection':'runtime.programAndDataDirectory')} busy={blocked} onClose={()=>setLocationOpen(false)}>
   {onboarding&&id==='codex'&&<button onClick={()=>{setLocationOpen(false);setForm('key')}}>{t('runtime.useOpenAIApiKey')}</button>}
   <SettingGroup><SettingRow label={t('runtime.programLocation')} description={state?.installation.path || t('runtime.autoDiscovery')}><button disabled={blocked} onClick={() => { setManual(v => !v); if (manual) { setProfile(p => ({ ...p, cliPath: '' })); setState(null); } }}>{manual ? t('runtime.useAutoDiscovery') : t('runtime.change')}</button></SettingRow>
    {manual && <div className="setup-path-field"><input aria-label={t('runtime.programPath')} disabled={blocked} value={profile.cliPath} placeholder={t('runtime.programPathPlaceholder')} onChange={e => { setProfile(p => ({ ...p, cliPath: e.target.value })); setState(null); }}/><button disabled={blocked} onClick={() => void pick()}>{t('runtime.chooseFile')}</button></div>}
    {id === 'caelis' && <SettingRow label={t('runtime.caelisDataDirectory')}><input aria-label={t('runtime.caelisDataDirectory')} disabled={blocked} value={profile.caelisStore ?? ''} placeholder={t('runtime.caelisDataDirectoryPlaceholder')} onChange={e => { setProfile(p => ({ ...p, caelisStore: e.target.value })); setState(null); }}/></SettingRow>}
    <SettingRow label={t('runtime.installAndUninstall')}><button onClick={() => void call('OpenMessageLink', id === 'caelis' ? 'https://caelis.dev' : 'https://learn.chatgpt.com/docs/codex/cli')}>{t('runtime.viewOfficialGuide')}</button></SettingRow>
   </SettingGroup>
   <div className="setup-end"><button disabled={blocked} onClick={()=>setLocationOpen(false)}>{t('common.done')}</button><button className="primary" disabled={blocked} onClick={()=>void run('detect')}>{t('runtime.recheck')}</button></div>
  </SettingsDialog>}
  {!onboarding && !oauthPage && busy && !['install','update'].includes(busy) && <p className="settings-note" role="status">{busy === 'install' ? t('runtime.downloadingAndInstalling') : busy === 'update' ? (id === 'caelis' ? t('runtime.updatingCaelisNotice') : t('runtime.updatingCodexNotice')) : busy === 'apply-update' ? t('runtime.activatingServiceNotice') : busy === 'activate' ? t('runtime.preparingRestartNotice') : t('runtime.processing')}</p>}
  {!oauthPage && (error ? <Recovery/> : notice && !(onboarding && state && ['ready','models'].includes(state.state)) ? <p className="settings-note" role="status">{notice}</p> : state && !(onboarding && (!state.installation.installed || state.state==='login')) && !serviceNeedsApply && !['ready', 'models'].includes(state.state) && (state.state !== 'incompatible' || onboarding || selectedActive) && <p className="settings-note" role="status">{t(setupStatusKey(state))}</p>)}
  {!(onboarding&&id==='caelis') && state?.state === 'ready' && (onboarding || (overview && (!overview.hasRuntimeChoice || (selectedActive && (reconnectNeeded || overview.pending || !!activeProfile && (profile.cliPath !== activeProfile.cliPath || (profile.caelisStore ?? '') !== (activeProfile.caelisStore ?? ''))))))) && <div className="setup-end"><SetupButton busy={busy==='activate'} className={onboarding || !overview?.hasRuntimeChoice ? 'primary' : undefined} disabled={blocked} onClick={() => void activate()}>{onboarding || !overview?.hasRuntimeChoice ? t('settings.permissionContinue') : t('runtime.restartAndApply')}</SetupButton></div>}
  {installOperation&&<SettingsDialog className="runtime-install-dialog" title={t(installError?(installCancelled?'settings.runtimeInstallCancelled':'settings.runtimeInstallFailed'):installOperation==='install'?'settings.codexInstalling':'settings.codexUpdating',{name})} busy={!!busy} onClose={()=>{if(!busy){setInstallOperation('');setInstallError('')}}}>
   {installError?<Recovery/>:<><div className={`runtime-install-progress${measured?' is-measured':''}`} role="progressbar" aria-valuemin={measured?0:undefined} aria-valuemax={measured?100:undefined} aria-valuenow={progressPercent} aria-label={t(installOperation==='install'?'settings.codexInstalling':'settings.codexUpdating',{name})}><span style={measured?{width:`${progressPercent}%`}:undefined}/></div>{progressAmount&&<p className="settings-note">{progressAmount}</p>}<p className="settings-note" role="status">{t(id==='caelis'&&installProgress?.phase==='verifying'?'settings.runtimeVerifying':id==='caelis'&&installProgress?.phase==='installing'?'settings.runtimeFinishing':'settings.runtimeInstallWait')}</p><div className="setup-end"><button disabled={installCancelled} onClick={()=>{cancelRequested.current=true;setInstallCancelled(true);void call('CancelSetupOperation').catch(()=>{cancelRequested.current=false;setInstallCancelled(false)})}}>{t('common.cancel')}</button></div></>}
   {installError&&<div className="setup-end"><button onClick={()=>{setInstallOperation('');setInstallError('')}}>{t('common.close')}</button><button className="primary" onClick={()=>void run(installOperation)}>{t('settings.runtimeTryAgain')}</button></div>}
  </SettingsDialog>}
  {form === 'model' && <ConnectionWizard onboarding={onboarding} initialKind={providerSelection?.kind} initialChoice={providerSelection?.id} client={connectionClient} active={active} onClose={() => setForm('')} onConnected={async () => { setForm(''); await run('detect'); }}/>}
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
   {error && <Recovery/>}
   <div className="setup-end"><button type="button" disabled={busy} onClick={onClose}>{t('common.cancel')}</button><button className="primary" disabled={busy || !key.trim()}>{busy ? t('runtime.connecting') : t('runtime.connect')}</button></div>
  </form>
 </SettingsDialog>;
}
