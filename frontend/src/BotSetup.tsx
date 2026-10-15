import {SetupBack} from './SetupBack';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { backend, desktop } from './desktop';
import { PermissionSettings } from './PermissionSettings';
import { SetupExtras } from './SetupExtras';
import {setupStage} from './setup-stage';
import { RuntimeSettings } from './RuntimeSettings';
import type { BotInitialization, BotIntroduction, Snapshot, SetupOverview } from './backend/contract';
import { useI18n } from './i18n';

// The backend journals one ordinary user message. The Bot maintains MEMORY.md;
// this form does not create a second identity settings store.
export function BotSetup({ onDone,active=true,focusVersion=0 }: { onDone: () => void;active?:boolean;focusVersion?:number }) {
 const {t}=useI18n();
 const [pending,setPending]=useState<{features:boolean;permissions:boolean;runtime:boolean}|null>(null);
 const [failed,setFailed]=useState(false);
 useEffect(()=>{if(!active)return;let live=true;void Promise.all([desktop<boolean>('FeatureGuidePending'),desktop<boolean>('PermissionGuidePending'),backend<SetupOverview>('SetupOverview'),backend<Snapshot>('Snapshot')]).then(([features,permissions,overview,snapshot])=>{if(live)setPending({features,permissions,runtime:overview.hasRuntimeChoice&&snapshot.connection==='ready'})}).catch(()=>{if(live)setFailed(true)});return()=>{live=false}},[active]);
 if(pending===null)return <p role="status">{t(failed?'settings.permissionLoadFailed':'common.loading')}</p>;
 return <BotIntroductionSetup runtimeInitiallyReady={pending.runtime} active={active} focusVersion={focusVersion} featuresPending={pending.features} permissionsPending={pending.permissions} onFeaturesDone={()=>setPending(current=>current&&{...current,features:false})} onFeaturesBack={()=>setPending(current=>current&&{...current,features:true})} onPermissionsDone={()=>setPending(current=>current&&{...current,permissions:false})} onPermissionsBack={()=>setPending(current=>current&&{...current,permissions:true})} onDone={onDone}/>;
}
function BotIntroductionSetup({ runtimeInitiallyReady,onDone,active,focusVersion,featuresPending,permissionsPending,onFeaturesDone,onFeaturesBack,onPermissionsDone,onPermissionsBack }: { runtimeInitiallyReady:boolean;onDone: () => void;active:boolean;focusVersion:number;featuresPending:boolean;permissionsPending:boolean;onFeaturesDone:()=>void;onFeaturesBack:()=>void;onPermissionsDone:()=>void;onPermissionsBack:()=>void }) {
  const {t} = useI18n();
  const [runtimeReady,setRuntimeReady]=useState(runtimeInitiallyReady);
  const submitting=useRef(false);
  const [state, setState] = useState<BotInitialization | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const opening = useRef(false), latestDone = useRef(onDone);
  latestDone.current = onDone;

  useEffect(() => {
    if(!active||state&&!['pending','dispatching','unknown'].includes(state.status))return;
    let alive = true;
    const load = async () => {
      try {
        const value = await backend<BotInitialization>('BotInitialization');
        if (alive) setState(value);
      } catch (e) {
        if (alive) setError(e instanceof Error ? e.message : t('chat.setupLoadFailed'));
      }
    };
    void load();
    const timer = window.setInterval(() => void load(), 1500);
    return () => { alive = false; clearInterval(timer); };
  }, [active,state?.status,t]);

  useEffect(() => {
    if (!active || !runtimeReady || !state || state.required || state.status === 'rejected' || state.status === 'unknown' || featuresPending || permissionsPending || opening.current) return;
    let live = true;
    void backend<Snapshot>('Snapshot').then(async snapshot => {
      if (!live || snapshot.connection !== 'ready' || opening.current) return;
      opening.current = true;
      try { await desktop('OpenHistory'); if (live) latestDone.current(); }
      catch (e) { opening.current = false; if (live) setError(e instanceof Error ? e.message : t('chat.setupLoadFailed')); }
    }).catch(() => {});
    return () => { live = false; };
  }, [active, runtimeReady, state?.required, state?.status, featuresPending, permissionsPending, t]);

  const openConversation = async () => {
    if (opening.current) return;
    opening.current = true;
    try { await desktop('OpenHistory'); latestDone.current(); }
    catch (e) { opening.current = false; setError(e instanceof Error ? e.message : t('chat.setupLoadFailed')); }
  };
  const continueWhenReady = async () => {
    const snapshot = await backend<Snapshot>('Snapshot');
    if (!featuresPending && !permissionsPending && snapshot.connection === 'ready') await openConversation();
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (submitting.current || busy || !name.trim()) return;
    submitting.current=true;
    setBusy(true);
    setError('');
    try {
      const input: BotIntroduction = { name: name.trim(), description: description.trim() };
      setState(await backend<BotInitialization>('InitializeBot', input));
      setName('');
      setDescription('');
      await continueWhenReady();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('chat.setupSaveFailed'));
    } finally { submitting.current=false; setBusy(false); }
  };
  const retry = async () => {
    setBusy(true);
    setError('');
    try {
      setState(await backend<BotInitialization>('RetryBotIntroduction'));
      await continueWhenReady();
    } catch (e) {
      setError(e instanceof Error ? e.message : t('chat.setupRetryFailed'));
    } finally { setBusy(false); }
  };

  if (!state) return <section className="runtime-welcome" role="status">{error || t('chat.setupPreparing')}</section>;
  const stage=setupStage(runtimeReady,featuresPending,permissionsPending);
  if(stage==='runtime')return <div className="setup-flow"><RuntimeSettings onboarding active={active} onDone={()=>setRuntimeReady(true)}/></div>;
  if (state.status === 'rejected' || state.status === 'unknown') return <section className="bot-introduction">
    <h1>{t('chat.setupIncompleteTitle')}</h1>
    <p className="setup-lead" role="status">{state.message}</p>
    <div className="setup-actions">
      {state.status === 'rejected' && <button disabled={busy} onClick={() => void retry()}>{t('chat.setupRetry')}</button>}
      <button onClick={() => void openConversation()}>{t('chat.setupOpenChat')}</button>
    </div>
    {error && <p className="inline-error" role="alert">{error}</p>}
  </section>;
  if (stage==='features') return <div className="setup-flow"><SetupExtras onBack={()=>setRuntimeReady(false)} onDone={onFeaturesDone}/></div>;
  if (stage==='permissions') return <div className="setup-flow"><PermissionSettings active={active} focusVersion={focusVersion} onBack={onFeaturesBack} onDone={onPermissionsDone}/></div>;
  if (!state.required) return <section className="runtime-welcome" role="status">{state.message||t('chat.setupPreparing')}<button onClick={()=>void openConversation()}>{t('chat.setupOpenChat')}</button></section>;

  return <div className="setup-flow"><section className="bot-introduction">
    <SetupBack disabled={busy} onClick={onPermissionsBack}/>
    <img className="setup-avatar" src="/icons/caelis-avatar.png" alt="" />
    <h1>{t('chat.setupIntroTitle')}</h1>
    <p className="setup-lead">{t('chat.setupSubtitle')}</p>
    <form onSubmit={event => void submit(event)}>
      <label htmlFor="bot-name">{t('chat.setupNameLabel')}
        <input id="bot-name" autoFocus required maxLength={80} autoComplete="off" value={name}
          disabled={busy} onChange={e => setName(e.target.value)} placeholder={t('chat.setupNamePlaceholder')} />
      </label>
      <label htmlFor="bot-description">{t('chat.setupDescLabel')} <span>{t('chat.setupOptional')}</span>
        <textarea id="bot-description" rows={2} maxLength={2000} value={description}
          disabled={busy} onChange={e => setDescription(e.target.value)} placeholder={t('chat.setupDescPlaceholder')} />
      </label>
      <p className="settings-note">{t('settings.setupStartHint')}</p>
      {error && <p className="inline-error" role="alert">{error}</p>}
      <div className="setup-end">
        <button className="primary" type="submit" disabled={busy || !name.trim()}>{busy ? t('common.loading') : t('settings.setupStartChat')}</button>
      </div>
    </form>
  </section></div>;
}
