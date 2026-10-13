import { useEffect, useRef, useState, type FormEvent } from 'react';
import { backend, desktop } from './desktop';
import { RuntimeSettings } from './RuntimeSettings';
import type { BotInitialization, BotIntroduction, SetupOverview, Snapshot } from './backend/contract';
import { useI18n } from './i18n';

// The backend journals one ordinary user message. The Bot maintains MEMORY.md;
// this form does not create a second identity settings store.
export function BotSetup({ onDone,active=true }: { onDone: () => void;active?:boolean;focusVersion?:number }) {
 const {t}=useI18n();
 const [overview,setOverview]=useState<SetupOverview|null>(null);
 const [failed,setFailed]=useState(false);
 useEffect(()=>{if(!active)return;let live=true;void backend<SetupOverview>('SetupOverview').then(value=>{if(live)setOverview(value)}).catch(()=>{if(live)setFailed(true)});return()=>{live=false}},[active]);
 if(overview===null)return <p role="status">{t(failed?'runtime.loadRuntimeFailed':'common.loading')}</p>;
 if(overview.onboarding || !overview.hasRuntimeChoice) return <div className="setup-flow"><SetupProgress step={0}/><RuntimeSettings onboarding active={active} onDone={()=>{void backend<SetupOverview>('SetupOverview').then(setOverview).catch(()=>setFailed(true));}}/></div>;
 return <BotIntroductionSetup active={active} onDone={onDone}/>;
}
function BotIntroductionSetup({ onDone,active }: { onDone: () => void;active:boolean }) {
  const {t} = useI18n();
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
    if (!active || !state || state.required || state.status === 'rejected' || state.status === 'unknown' || opening.current) return;
    let live = true;
    void backend<Snapshot>('Snapshot').then(async snapshot => {
      if (!live || snapshot.connection !== 'ready' || opening.current) return;
      opening.current = true;
      try { await desktop('OpenHistory'); if (live) latestDone.current(); }
      catch (e) { opening.current = false; if (live) setError(e instanceof Error ? e.message : t('chat.setupLoadFailed')); }
    }).catch(() => {});
    return () => { live = false; };
  }, [active, state?.required, state?.status, t]);

  const openConversation = async () => {
    if (opening.current) return;
    opening.current = true;
    try { await desktop('OpenHistory'); latestDone.current(); }
    catch (e) { opening.current = false; setError(e instanceof Error ? e.message : t('chat.setupLoadFailed')); }
  };
  const continueWhenReady = async () => {
    const snapshot = await backend<Snapshot>('Snapshot');
    if (snapshot.connection === 'ready') await openConversation();
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
  if (!state.required) return <div className="setup-flow"><SetupProgress step={1}/>
    {state.message && <p className="setup-introduction-status" role="status">{state.message}</p>}
    <section className="bot-introduction"><h1>{t('chat.setupPreparing')}</h1><button className="primary" onClick={() => void openConversation()}>{t('chat.setupOpenChat')}</button>{error&&<p role="alert" className="inline-error">{error}</p>}</section>
  </div>;

  return <div className="setup-flow"><SetupProgress step={1}/><section className="bot-introduction">
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
 return <ol className="setup-progress" aria-label={t('settings.setupProgress')}>{(['setupConnection','setupIdentity'] as const).map((key,index)=><li key={key} aria-current={index===step?'step':undefined}><span aria-hidden="true">{index<step?'✓':index+1}</span>{t(`settings.${key}`)}</li>)}</ol>;
}
