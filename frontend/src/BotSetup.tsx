import {isWebSurface} from './transport';
import {Recovery} from './SurfaceBoundary';
import {recoverRead} from './read-recovery';
import {SetupButton,SetupLoading} from './SetupLoading';
import {SetupBack} from './SetupBack';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { backend, desktop } from './desktop';
import { SetupExtras } from './SetupExtras';
import {setupStage} from './setup-stage';
import {SetupActivation} from './setup-activation';
import {selectedSetupReady,readIntroductionDraft,saveIntroductionDraft,introductionStorage,activationNeedsRecovery} from './setup-connection';
import {presentationWarning} from './presentation-warning';
import { RuntimeSettings } from './RuntimeSettings';
import type { BotInitialization, BotIntroduction, Snapshot, SetupOverview } from './backend/contract';
import { useI18n } from './i18n';

// The backend journals one ordinary user message. The Bot maintains MEMORY.md;
// this form does not create a second identity settings store.
export function BotSetup({ onDone,active=true }: { onDone: () => void;active?:boolean;focusVersion?:number }) {
 const {t}=useI18n();
 const [pending,setPending]=useState<{features:boolean;runtime:boolean;runtimeID:string}|null>(null);
 const [failed,setFailed]=useState(false),[readAttempt,setReadAttempt]=useState(0);
 useEffect(()=>{if(!active)return;let live=true;void recoverRead(()=>Promise.all([desktop<boolean>('FeatureGuidePending'),backend<SetupOverview>('SetupOverview'),backend<Snapshot>('Snapshot')])).then(([features,overview,snapshot])=>{if(live)setPending({features,runtime:overview.hasRuntimeChoice&&snapshot.connection==='ready'&&snapshot.phase!=='unknown',runtimeID:overview.active})}).catch(()=>{if(live)setFailed(true)});return()=>{live=false}},[active,readAttempt]);
 if(pending===null)return failed?<Recovery message={t('chat.setupLoadFailed')} onRetry={()=>{setFailed(false);setReadAttempt(v=>v+1)}}/>:<SetupLoading/>;
 return <BotIntroductionSetup initialRuntime={pending.runtimeID} runtimeInitiallyReady={pending.runtime} active={active} featuresPending={pending.features} onFeaturesDone={()=>setPending(current=>current&&{...current,features:false})} onFeaturesBack={()=>setPending(current=>current&&{...current,features:true})} onDone={onDone}/>;
}
function BotIntroductionSetup({ initialRuntime,runtimeInitiallyReady,onDone,active,featuresPending,onFeaturesDone,onFeaturesBack }: { initialRuntime:string;runtimeInitiallyReady:boolean;onDone: () => void;active:boolean;featuresPending:boolean;onFeaturesDone:()=>void;onFeaturesBack:()=>void }) {
  const {t} = useI18n();
  const [runtimeReady,setRuntimeReady]=useState(runtimeInitiallyReady);
  const [runtimeSelected,setRuntimeSelected]=useState(runtimeInitiallyReady);
  const [needsConnection,setNeedsConnection]=useState(false);
  const activation=useRef(new SetupActivation(runtimeInitiallyReady));
  const activationEpoch=useRef(0),selectedRuntime=useRef(initialRuntime);
  const mounted=useRef(true);
  useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[]);
  const selectRuntime=()=>{activationEpoch.current++;activation.current.reset();setRuntimeReady(false);setRuntimeSelected(false);setNeedsConnection(false)};
  const activateInBackground=(work:()=>Promise<void>,runtime:string)=>{
    const epoch=++activationEpoch.current;selectedRuntime.current=runtime;
    setRuntimeReady(false);setRuntimeSelected(true);setNeedsConnection(false);
    setError('');
    let failure=t('settings.setupActivationFailed');
    void activation.current.start(async()=>{try{await work()}catch(error){if(activationNeedsRecovery(error))failure=t('settings.setupActivationRecovery');throw error}}).then(ready=>{
      if(!mounted.current||epoch!==activationEpoch.current)return;
      if(ready)setRuntimeReady(true);
      else {setNeedsConnection(true);setError(failure);}
    });
  };
  const submitting=useRef(false);
  const [state, setState] = useState<BotInitialization | null>(null);
  const [draft]=useState(()=>readIntroductionDraft(introductionStorage()));
  const [name, setName] = useState(draft.name);
  const [description, setDescription] = useState(draft.description);
  useEffect(()=>saveIntroductionDraft(introductionStorage(),{name,description}),[name,description]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [readAttempt,setReadAttempt]=useState(0);
  const opening = useRef(false), latestDone = useRef(onDone);
  latestDone.current = onDone;

  useEffect(() => {
    if(!active||state&&!['pending','dispatching','unknown'].includes(state.status))return;
    let alive = true, timer:ReturnType<typeof setTimeout>;
    const controller=new AbortController();
    const load = async () => {
      try {
        const value = await recoverRead(()=>backend<BotInitialization>('BotInitialization'),controller.signal);
        if (alive) {setState(value);setError('');timer=setTimeout(()=>void load(),1500);}
      } catch (e) {
        if (alive) setError(e instanceof Error ? e.message : t('chat.setupLoadFailed'));
      }
    };
    void load();
    return () => { alive = false;controller.abort();clearTimeout(timer); };
  }, [active,state?.status,t,readAttempt]);

  useEffect(() => {
    if (!active || !runtimeReady || !state || state.required || state.status === 'rejected' || state.status === 'unknown' || featuresPending || opening.current) return;
    let live = true;
    void selectedSetupReady(backend,selectedRuntime.current).then(async ready => {
      if (!live || !ready || opening.current) return;
      opening.current = true;
      try { await desktop('OpenHistory'); if (live) latestDone.current(); }
      catch (e) { opening.current = false; if (live) setError(e instanceof Error ? e.message : t('chat.setupLoadFailed')); }
    }).catch(() => {});
    return () => { live = false; };
  }, [active, runtimeReady, state?.required, state?.status, featuresPending, t]);

  const openConversation = async () => {
    if (opening.current) return;
    if(!await selectedSetupReady(backend,selectedRuntime.current)){setNeedsConnection(true);setError(t('settings.setupActivationFailed'));return;}
    opening.current = true;
    try { await desktop('OpenHistory'); latestDone.current(); }
    catch (e) { opening.current = false; setError(e instanceof Error ? e.message : t('chat.setupLoadFailed')); }
  };
  const continueWhenReady = async () => {
    if (!featuresPending && await selectedSetupReady(backend,selectedRuntime.current)) await openConversation();
  };
  const checkConnection=async()=>{
    setBusy(true);
    try{
      if(await selectedSetupReady(backend,selectedRuntime.current)){setNeedsConnection(false);setRuntimeReady(true);setError('');}
      else {setNeedsConnection(true);setError(t('settings.setupActivationFailed'));}
    }catch{setError(t('settings.setupActivationFailed'));}
    finally{setBusy(false);}
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (submitting.current || busy || !name.trim()) return;
    submitting.current=true;
    setBusy(true);
    setError('');
    try {
      // Selection unlocks editing, never work. Keep the introduction on failure
      // and preserve the backend's journal/receipt rules for InitializeBot.
      await activation.current.ready();
      if(!await selectedSetupReady(backend,selectedRuntime.current)){
        setNeedsConnection(true);setError(t('settings.setupActivationFailed'));return;
      }
      setRuntimeReady(true);
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

  if (!state) return error?<Recovery message={error} onRetry={()=>{setError('');setReadAttempt(v=>v+1)}}/>:<SetupLoading/>;
  const stage=setupStage(runtimeSelected,featuresPending);
  if(stage==='runtime')return <div className="setup-flow"><RuntimeSettings onboarding active={active} onActivating={(work,runtime)=>activateInBackground(async()=>{try{await work()}catch(e){presentationWarning('setup_activation_failed');throw e}},runtime)}/></div>;
  if (state.status === 'rejected' || state.status === 'unknown') return <section className="bot-introduction">
    <h1>{t('chat.setupIncompleteTitle')}</h1>
    <Recovery message={error||t(state.status==='unknown'?'chat.sendPending':'chat.setupSaveFailed')} onRetry={state.status==='rejected'?()=>{if(!busy)void retry()}:()=>{setError('');setReadAttempt(v=>v+1)}}/>
  </section>;
  if (stage==='features') return <div className="setup-flow"><SetupExtras onBack={selectRuntime} onDone={onFeaturesDone}/></div>;
  if (!state.required) return <section className="runtime-welcome" role="status">{t('chat.setupPreparing')}<button onClick={()=>void openConversation()}>{t('chat.setupOpenChat')}</button></section>;

  return <div className="setup-flow"><section className="bot-introduction">
    <SetupBack disabled={busy} onClick={needsConnection||isWebSurface()?selectRuntime:onFeaturesBack}/>
    <header className="onboarding-route-heading onboarding-introduction-heading">
      <img className="setup-avatar" src="/icons/caelis-avatar.png" alt="" />
      <div><h1>{t('chat.setupIntroTitle')}</h1></div>
    </header>
    <form onSubmit={event => void submit(event)}>
      <div className="onboarding-introduction-fields">
      <label htmlFor="bot-name">{t('chat.setupNameLabel')}
        <input id="bot-name" autoFocus required maxLength={80} autoComplete="off" value={name}
          disabled={busy} onChange={e => setName(e.target.value)} placeholder={t('chat.setupNamePlaceholder')} />
      </label>
      <label htmlFor="bot-description">{t('chat.setupDescLabel')} <span>{t('chat.setupOptional')}</span>
        <textarea id="bot-description" rows={2} maxLength={2000} value={description}
          disabled={busy} onChange={e => setDescription(e.target.value)} placeholder={t('chat.setupDescPlaceholder')} />
      </label>
      </div>
      {error && <Recovery message={error}/>}
      <div className="setup-end">
        {needsConnection&&<button type="button" disabled={busy} onClick={()=>void checkConnection()}>{t('settings.setupCheckConnection')}</button>}
        <SetupButton className="primary" type="submit" busy={busy} disabled={!name.trim()||needsConnection}>{t('settings.setupStartChat')}</SetupButton>
      </div>
    </form>
  </section></div>;
}
