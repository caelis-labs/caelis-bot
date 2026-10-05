import { useEffect, useRef, useState, type FormEvent } from 'react';
import { backend, desktop } from './desktop';
import { PermissionSettings } from './PermissionSettings';
import { SetupExtras } from './SetupExtras';
import { RuntimeSettings } from './RuntimeSettings';
import type { BotInitialization, BotIntroduction, Snapshot } from './backend/contract';
import { useI18n } from './i18n';

// The backend journals one ordinary user message. The Bot maintains MEMORY.md;
// this form does not create a second identity settings store.
export function BotSetup({ onDone }: { onDone: () => void }) {
 const {t}=useI18n();
 const [pending,setPending]=useState<{features:boolean;permissions:boolean}|null>(null);
 const [failed,setFailed]=useState(false);
 useEffect(()=>{let active=true;void Promise.all([desktop<boolean>('FeatureGuidePending'),desktop<boolean>('PermissionGuidePending')]).then(([features,permissions])=>{if(active)setPending({features,permissions})}).catch(()=>{if(active)setFailed(true)});return()=>{active=false}},[]);
 if(pending===null)return <p role="status">{t(failed?'settings.permissionLoadFailed':'common.loading')}</p>;
 return <BotIntroductionSetup featuresPending={pending.features} permissionsPending={pending.permissions} onFeaturesDone={()=>setPending(current=>current&&{...current,features:false})} onFeaturesBack={()=>setPending(current=>current&&{...current,features:true})} onPermissionsDone={()=>setPending(current=>current&&{...current,permissions:false})} onPermissionsBack={()=>setPending(current=>current&&{...current,permissions:true})} onDone={onDone}/>;
}
function BotIntroductionSetup({ onDone,featuresPending,permissionsPending,onFeaturesDone,onFeaturesBack,onPermissionsDone,onPermissionsBack }: { onDone: () => void;featuresPending:boolean;permissionsPending:boolean;onFeaturesDone:()=>void;onFeaturesBack:()=>void;onPermissionsDone:()=>void;onPermissionsBack:()=>void }) {
  const {t} = useI18n();
  const [state, setState] = useState<BotInitialization | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const opening = useRef(false), latestDone = useRef(onDone);
  latestDone.current = onDone;

  useEffect(() => {
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
  }, [t]);

  useEffect(() => {
    if (!state || state.required || state.status === 'rejected' || state.status === 'unknown' || featuresPending || permissionsPending || opening.current) return;
    let active = true;
    void backend<Snapshot>('Snapshot').then(async snapshot => {
      if (!active || snapshot.connection !== 'ready' || opening.current) return;
      opening.current = true;
      try { await desktop('OpenHistory'); if (active) latestDone.current(); }
      catch (e) { opening.current = false; if (active) setError(e instanceof Error ? e.message : t('chat.setupLoadFailed')); }
    }).catch(() => {});
    return () => { active = false; };
  }, [state?.required, state?.status, featuresPending, permissionsPending, t]);

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
    if (busy || !name.trim()) return;
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
    } finally { setBusy(false); }
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
  if (state.status === 'rejected' || state.status === 'unknown') return <section className="bot-introduction">
    <h1>{t('chat.setupIncompleteTitle')}</h1>
    <p className="setup-lead" role="status">{state.message}</p>
    <div className="setup-actions">
      {state.status === 'rejected' && <button disabled={busy} onClick={() => void retry()}>{t('chat.setupRetry')}</button>}
      <button onClick={() => void openConversation()}>{t('chat.setupOpenChat')}</button>
    </div>
    {error && <p className="inline-error" role="alert">{error}</p>}
  </section>;
  if (!state.required && featuresPending) return <div className="setup-flow"><SetupProgress step={1}/><SetupExtras onDone={onFeaturesDone}/></div>;
  if (!state.required && permissionsPending) return <div className="setup-flow"><SetupProgress step={2}/><PermissionSettings onBack={onFeaturesBack} onDone={onPermissionsDone}/></div>;
  if (!state.required) return <div className="setup-flow"><SetupProgress step={3}/><button type="button" className="text-action setup-back" onClick={onPermissionsBack}>{t('settings.setupBack')}</button>
    {state.message && <p className="setup-introduction-status" role="status">{state.message}</p>}
    <RuntimeSettings onboarding onDone={onDone} />
  </div>;

  return <div className="setup-flow"><SetupProgress step={0}/><section className="bot-introduction">
    <img className="setup-avatar" src="/icons/caelis-avatar.png" alt="" />
    <h1>{t('chat.setupIntroTitle')}</h1>
    <p className="setup-lead">{t('chat.setupSubtitle')}</p>
    <form onSubmit={event => void submit(event)}>
      <label htmlFor="bot-name">{t('chat.setupNameLabel')}
        <input id="bot-name" autoFocus required maxLength={80} autoComplete="off" value={name}
          disabled={busy} onChange={e => setName(e.target.value)} placeholder={t('chat.setupNamePlaceholder')} />
      </label>
      <label htmlFor="bot-description">{t('chat.setupDescLabel')} <span>{t('chat.setupOptional')}</span>
        <textarea id="bot-description" rows={3} maxLength={2000} value={description}
          disabled={busy} onChange={e => setDescription(e.target.value)} placeholder={t('chat.setupDescPlaceholder')} />
      </label>
      <p className="settings-note">{t('chat.setupHint')}</p>
      {error && <p className="inline-error" role="alert">{error}</p>}
      <div className="setup-end">
        <button className="primary" type="submit" disabled={busy || !name.trim()}>{busy ? t('common.loading') : t('chat.setupContinue')}</button>
      </div>
    </form>
  </section></div>;
}

function SetupProgress({step}:{step:number}) {
 const {t}=useI18n();
 return <ol className="setup-progress" aria-label={t('settings.setupProgress')}>{(['setupIdentity','setupExtras','setupPermissions','setupConnection'] as const).map((key,index)=><li key={key} aria-current={index===step?'step':undefined}><span aria-hidden="true">{index<step?'✓':index+1}</span>{t(`settings.${key}`)}</li>)}</ol>;
}
