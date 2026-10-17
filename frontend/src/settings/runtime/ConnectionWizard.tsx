import appIcon from '../../../../internal/desktop/assets/app-icon.png';
import type { ModelOption } from '../../backend/contract';
import { ModelParameters } from './ModelPicker';
import type { ModelSelection } from './types';
import { chooseModel, onboardingEffort, modelDescription, validCustomLimits } from './state';
import { ProviderLogo, providerNames } from './OnboardingProviders';
import {connectModels} from './connect-models';
import { SettingsSelect } from '../../SettingsSelect';
import { useEffectEvent, useEffect, useId, useRef, useState } from 'react';
import type { APIKeyOptions, ConnectAction, ConnectChoice, ConnectionCatalog, ConnectionFlow, ConnectionKind, FlowInput, RuntimeSettingsClient } from './types';
import { acceptConnectionProgress, messageOf, safeWebURL } from './state';
import { localMagpie } from './gateway';
import { SettingsPage } from './SettingsPage';
import { SettingsDialog } from './SettingsDialog';
import { useI18n } from '../../i18n';
import { SettingsChevron } from '../../SettingsIcons';
import { SetupButton, SetupLoading } from '../../SetupLoading';

function friendlyModel(name:string) {
 const id=name.includes('/')?name.slice(name.lastIndexOf('/')+1):name;
 if(/^gpt-/i.test(id))return id.replace(/^gpt-/i,'GPT-');
 if(/^grok-/i.test(id))return id.replace(/^grok-/i,'Grok ');
 if(/^mimo-/i.test(id))return id.replace(/^mimo-/i,'MiMo-');
 if(/^deepseek-/i.test(id))return id.replace(/^deepseek-/i,'DeepSeek ');
 return id;
}
const emptyCatalog: ConnectionCatalog = { choices: [], unavailable: '' };
export function ConnectionWizard({ client, onClose, onConnected,active=true,inline=false,initialKind='api-key',initialChoice,hideHeader=false,onboarding=false,onBusyChange }: { onboarding?:boolean; onBusyChange?:(busy:boolean)=>void; hideHeader?:boolean; client: RuntimeSettingsClient; onClose: () => void; onConnected: (model?:string, parameters?:ModelSelection) => Promise<void>;active?:boolean;inline?:boolean;initialKind?:ConnectionKind;initialChoice?:string }) {
 const { t, date } = useI18n();
 const actionFailed=useEffectEvent(()=>t('runtime.actionFailed'));
 const [kind, setKind] = useState<ConnectionKind>(initialKind), [catalog, setCatalog] = useState(emptyCatalog), [choice, setChoice] = useState<ConnectChoice | null>(null);
 const [parameters,setParameters]=useState<ModelSelection>({model:'',effort:'',serviceTier:''});
 const [selectedModels,setSelectedModels]=useState<string[]>([]),[completedModels,setCompletedModels]=useState<string[]>([]);
 const [options, setOptions] = useState<APIKeyOptions>({ endpoints: [], models: [] });
 const [contextWindow, setContextWindow] = useState(''), [maxOutput, setMaxOutput] = useState(''), [imageInput, setImageInput] = useState(false), [reasoningLevels, setReasoningLevels] = useState('');
 const [baseUrl, setBaseUrl] = useState(''), [model, setModel] = useState(''), [apiKey, setAPIKey] = useState(''), [command, setCommand] = useState('');
 const [flow, setFlow] = useState<ConnectionFlow | null>(null), [code, setCode] = useState(''), [method, setMethod] = useState(''), [destination, setDestination] = useState(''), [manual, setManual] = useState(false);
 const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false), [error, setError] = useState(''), [canceling, setCanceling] = useState(false), [now, setNow] = useState(Date.now()), [optionsRefresh, setOptionsRefresh] = useState(0);
 const [optionsFor,setOptionsFor]=useState('');
 const optionsClient=useRef(client);
 const [accountModels,setAccountModels]=useState<ConnectionFlow['models']>([]);
 const reportBusy=useEffectEvent((value:boolean)=>onBusyChange?.(value));
 const navigationBusy=busy||canceling||!!flow&&!['complete','failed','unknown'].includes(flow.stage);
 useEffect(()=>{reportBusy(navigationBusy);return()=>reportBusy(false)},[navigationBusy]);
 const alive = useRef(true), working = useRef(false), serial = useRef(0), abort = useRef<AbortController | null>(null), optionsSerial = useRef(0), modelListID = useId(), endpointListID = useId();
 useEffect(() => { const clear = () => { setAPIKey(''); setCode(''); }; window.addEventListener('settings-close', clear); return () => window.removeEventListener('settings-close', clear); }, []);
 useEffect(() => { alive.current = true; return () => { alive.current = false; serial.current++; abort.current?.abort(); }; }, []);
 useEffect(() => {
  let valid = true; setLoading(true); setCatalog(emptyCatalog); setChoice(null); setFlow(null); setAPIKey(''); setOptionsFor(''); setError('');
  void client.catalog(kind).then(value => { if (valid) {setCatalog(value);if(initialChoice){const found=value.choices.find(c=>c.id===initialChoice);if(found){setChoice(found);setBaseUrl(found.id==='magpie'?'http://127.0.0.1:3425/v1':'');}}} }).catch(e => { if (valid) setError(messageOf(e, actionFailed())); }).finally(() => { if (valid) setLoading(false); });
  return () => { valid = false; };
 }, [client, kind]);
 useEffect(() => {
  if (!choice || kind !== 'api-key') return;
  const key=`${choice.id}:${baseUrl}:${optionsRefresh}`;
  if(optionsClient.current===client&&optionsFor===key)return;
  const seq = ++optionsSerial.current;
  setLoading(true); setAPIKey(''); setError('');
  const timer = window.setTimeout(() => { void client.apiKeyOptions(choice.id, baseUrl).then(value => {
   if (!alive.current || seq !== optionsSerial.current) return;
   setOptions(value);
   optionsClient.current=client;
   setOptionsFor(`${choice.id}:${baseUrl||value.endpoints[0]?.value||''}:${optionsRefresh}`);
   if (!baseUrl && value.endpoints[0]?.value) setBaseUrl(value.endpoints[0].value);
  }).catch(e => { if (alive.current && seq === optionsSerial.current) { setOptionsFor(key); setOptions(current=>({...current,models:[]})); setError(choice.id==='magpie'?t('connections.gatewayEmpty'):messageOf(e, actionFailed())); } }).finally(() => { if (alive.current && seq === optionsSerial.current) setLoading(false); }); }, choice.custom&&baseUrl?300:0);
  return () => { clearTimeout(timer); optionsSerial.current++; };
 }, [client, choice, kind, baseUrl, optionsRefresh]);
 useEffect(() => { if (!active||!flow?.authorization?.expiresAt) return; setNow(Date.now()); const timer = window.setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, [active,flow?.authorization?.expiresAt]);
 const expired = !!flow?.authorization?.expiresAt && Date.parse(flow.authorization.expiresAt) <= now;
 const builtinProvider=kind==='api-key'&&['deepseek','xiaomi'].includes(choice?.id||'');
 const gateway = choice?.id === 'magpie';
 const ollama=choice?.id==='ollama';
 const multipleModels=builtinProvider||gateway||ollama;
 const localGateway = localMagpie(choice?.id, baseUrl);
 const reuseAuth = options.endpoints.some(endpoint => endpoint.value === baseUrl && endpoint.reuseAuth);
 const select = (value: ConnectChoice) => { setChoice(value); setSelectedModels([]);setCompletedModels([]); setBaseUrl(value.id === 'magpie' ? 'http://127.0.0.1:3425/v1' : ''); setModel(''); setCommand(''); setContextWindow(''); setMaxOutput(''); setImageInput(false); setReasoningLevels(''); setAPIKey(''); setError(''); };
 const observe = (seq: number, update: ConnectionFlow) => {
  if (!alive.current || seq !== serial.current) return;
  setFlow(current => acceptConnectionProgress(current, update));
  if (update.installation) setDestination(current => current || update.installation!.destination);
  if (update.stage !== 'authorization' || !update.authorization?.canSubmit) setCode('');
 };
 const run = async (action?: ConnectAction, input: FlowInput = {}) => {
  if (working.current || !choice) return;
  if (!flow && choice.id==='custom' && !validCustomLimits(contextWindow,maxOutput)) return;
  abort.current?.abort();
  const seq = ++serial.current, controller = new AbortController(); abort.current = controller;
  working.current = true; setBusy(true); setError('');
  // Secret drafts are only write arguments. Never put them in a flow snapshot,
  // URL, local storage, logs, operation key or a retry closure.
  const secret = localGateway && !reuseAuth ? 'magpie' : apiKey; setAPIKey(''); setCode('');
  try {
   if(multipleModels&&!flow&&!action){
    const next=await connectModels(client,{kind,choice:choice.id,baseUrl,apiKey:secret},selectedModels,controller.signal,update=>observe(seq,update),id=>{if(alive.current&&seq===serial.current)setCompletedModels(old=>[...old,id])});
    observe(seq,next);return;
   }
   const next = flow && action ? await client.advanceConnection(flow, action, input, controller.signal, value => observe(seq, value)) :
    await client.startConnection({ kind, choice: choice.id, ...(choice.custom ? { command } : {}), ...(kind === 'api-key' ? { baseUrl, model, apiKey: secret, ...(choice.id==='custom' ? { contextWindowTokens: Number(contextWindow), maxOutputTokens: Number(maxOutput), imageInput, reasoningLevels: reasoningLevels.split(',').map(s => s.trim()).filter(Boolean) } : {}) } : {}) }, controller.signal, value => observe(seq, value));
   observe(seq, next);
  } catch {
   if (alive.current && seq === serial.current && !controller.signal.aborted) {
    setFlow(current => !multipleModels && current?.stage === 'complete' ? current : ({ id: multipleModels ? '' : current?.id || '', revision: multipleModels ? '' : current?.revision || '', sequence: multipleModels ? 0 : current?.sequence || 0, stage: 'unknown', title: t('connections.unknownResultTitle'), message: t('connections.unknownResultMessage') }));
   }
  } finally { if (alive.current && seq === serial.current) { working.current = false; setBusy(false); } }
 };
 useEffect(()=>{if(flow?.stage==='models'){setAccountModels(flow.models||[]);setModel(current=>flow.models?.some(m=>m.id===current)?current:flow.models?.[0]?.id||'')}},[flow?.stage,flow?.models]);
 const startAccount = useEffectEvent(() => { void run(); });
 const accountStarted = useRef(false);
 useEffect(() => {
  if (onboarding && kind === 'account' && choice && !loading && !accountStarted.current) {
   accountStarted.current = true;
   startAccount();
  }
 }, [onboarding, kind, choice, loading]);
 const selectedID=selectedModels[0]||model;
 const [discoveredParameters,setDiscoveredParameters]=useState<{id:string;value:ModelOption|null}|null>(null);
 const selectedCapability=flow?.models?.find(m=>m.id===selectedID)?.parameters||options.models.find(m=>m.value===selectedID)?.parameters||(discoveredParameters?.id===selectedID?discoveredParameters.value:null);
 useEffect(()=>{
  if(onboarding&&multipleModels&&!loading&&!flow)setSelectedModels(current=>options.models.some(m=>m.value===current[0])?current:options.models.length?[options.models[0].value]:[]);
 },[onboarding,multipleModels,loading,flow,options.models]);
 useEffect(()=>{
  setParameters(current=>{
   const defaults=selectedCapability?chooseModel(selectedCapability):{model:selectedID,effort:'',serviceTier:''};
   return {...defaults,model:selectedID,effort:current.effort&&selectedCapability?.efforts.includes(current.effort)?current.effort:onboarding?onboardingEffort(selectedCapability?.efforts||[]):defaults.effort,serviceTier:selectedCapability?.serviceTiers.some(t=>t.id===current.serviceTier)?current.serviceTier:''};
  });
 },[selectedID,selectedCapability,onboarding]);
 useEffect(()=>{
  if(!onboarding||!choice||!selectedID||!client.modelParameters)return;
  let valid=true;
  void client.modelParameters(choice.id,baseUrl,selectedID).then(value=>{if(valid)setDiscoveredParameters({id:selectedID,value})}).catch(()=>{if(valid)setDiscoveredParameters({id:selectedID,value:null})});
  return()=>{valid=false;};
 },[client,onboarding,choice,baseUrl,selectedID]);
 const modelParameters=<ModelParameters model={selectedCapability||undefined} value={parameters} disabled={busy||loading||onboarding&&flow?.stage==='complete'} onChange={patch=>setParameters(current=>({...current,...patch}))}/>;
 const parameterControls=onboarding?(selectedCapability&&<details className="provider-advanced"><summary>{t('settings.setupMoreOptions')}</summary>{modelParameters}</details>):modelParameters;
 const finish = async () => { if(onboarding){try{setBusy(true);await onConnected(selectedModels[0]||model,parameters);}catch(e){setError(messageOf(e,actionFailed()));}finally{if(alive.current)setBusy(false);}}else{onClose();await onConnected();} };
 const completed = useRef(false);
 useEffect(()=>{if(onboarding&&flow?.stage==='complete'&&!busy&&!completed.current){completed.current=true;void finish();}},[onboarding,flow?.stage,busy]);
 const cancel = async () => {
  if (canceling) return;
  if (!flow?.id || ['complete', 'failed', 'unknown'].includes(flow.stage)) { if(onboarding)onClose();else await finish(); return; }
  setCanceling(true); setError('');
  try {
   await client.cancelConnection(flow);
   serial.current++; abort.current?.abort(); setCode(''); onClose();
  } catch { if (alive.current) setError(t('connections.cancelNotConfirmed')); }
  finally { if (alive.current) setCanceling(false); }
 };
 const open = async (url: string) => { if (!safeWebURL(url)) { setError(t('connections.invalidUrl')); return; } try { await client.openURL(url); } catch { setError(t('connections.cannotOpenBrowser')); } };
 const close = () => { setAPIKey(''); setCode(''); if (!flow) onClose(); else void cancel(); };
 const Surface = inline ? SettingsPage : SettingsDialog;
 const oauthPage = onboarding && flow?.stage === 'authorization';
 const completing=onboarding&&flow?.stage==='complete';
 const submitting=busy||completing&&!error;
 const keepAPIForm=onboarding&&kind==='api-key'&&!!flow&&['preparing','complete'].includes(flow.stage);
 const showModels=flow?.stage==='models'||completing&&kind==='account';
 const modelChoices=completing?accountModels:flow?.models;
 if(onboarding&&!flow&&!error&&(!choice&&loading||choice&&kind==='api-key'&&!optionsFor.startsWith(`${choice.id}:`)))return <SetupLoading/>;
 if(onboarding&&kind==='account'&&(!flow||flow.stage==='preparing'))return <SetupLoading/>;
 return <Surface hideHeader={hideHeader} className={`provider-connection${onboarding ? ' onboarding-connection' : ''}${oauthPage ? ' provider-oauth-page' : ''}`} title={onboarding&&choice?<span className="provider-brand-title"><ProviderLogo id={choice.id}/>{providerNames[choice.id]||(choice.id==='custom'?t('runtime.providerCustom'):choice.name)}</span>:flow?.title||((inline&&choice)?providerNames[choice.id]||choice.name:t('connections.addConnection'))} description={inline||flow?.stage==='complete'?undefined:flow?.message || undefined} busy={busy || canceling} onClose={close}>
  {inline&&!onboarding&&flow&&<section className="provider-flow-status" role="status"><h3>{flow.title}</h3>{flow.stage!=='complete'&&flow.message&&<p>{flow.message}</p>}</section>}
  {onboarding&&!oauthPage&&flow&&!['preparing','models','complete'].includes(flow.stage)&&flow.message&&<p className="settings-note" role="status">{flow.message}</p>}
  {(!flow||keepAPIForm) && <>
   {choice&&!onboarding&&<div className="provider-config-intro">{!onboarding&&<ProviderLogo id={choice.id}/>}<p>{t(onboarding&&kind==='api-key'?(gateway?'runtime.onboardingGatewayIntro':ollama?'runtime.onboardingOllamaIntro':choice.id==='custom'?'runtime.connectCustomIntro':'runtime.onboardingApiIntro'):kind==='account'?(choice.id==='grok'?'runtime.connectGrokIntro':'runtime.connectCodexIntro'):choice.id==='magpie'?'runtime.connectGatewayIntro':choice.id==='ollama'?'runtime.connectOllamaIntro':choice.id==='custom'?'runtime.connectCustomIntro':'runtime.connectApiIntro')}</p></div>}
   {!choice && !inline && <div className="runtime-segments" aria-label={t('connections.connectionTypeAria')}>{(onboarding ? ['api-key','account'] as const : ['api-key','account','agent'] as const).map(value=><button key={value} disabled={busy} aria-pressed={kind===value} onClick={()=>setKind(value)}>{t(value==='api-key'?'connections.modelService':value==='account'?'connections.accountLogin':'connections.externalAgent')}</button>)}</div>}
   {loading && !choice && <p role="status">{t('connections.loadingChoices')}</p>}
   {catalog.unavailable && <p className="settings-note" role="status">{catalog.unavailable}</p>}
   {!loading && !catalog.unavailable && !catalog.choices.length && <p className="settings-note">{t('connections.noChoices')}</p>}
   {!choice ? <div className="runtime-connect-catalog">{[...catalog.choices].sort((a,b)=>Number(b.id==='magpie')-Number(a.id==='magpie')).map(c => <button key={c.id} disabled={busy} onClick={() => select(c)}><ProviderLogo id={c.id}/><span><strong>{providerNames[c.id] || c.name}</strong>{c.id==='magpie'&&<small>{t('connections.magpieHint')}</small>}</span><SettingsChevron/></button>)}</div> : <>
    {!inline && !initialChoice && <><button className="text-action" disabled={busy} onClick={() => { optionsSerial.current++; setLoading(false); setChoice(null); setAPIKey(''); setError(''); }}>{t('connections.backToChoices')}</button>
    {!onboarding&&<h3 className="runtime-connect-choice">{providerNames[choice.id] || (choice.id==='custom'?t('runtime.providerCustom'):choice.name)}</h3>}</>}
    {choice.custom && <><label>{t('connections.startCommand')}<input disabled={busy} value={command} onChange={e => setCommand(e.target.value)} placeholder={t('connections.startCommandPlaceholder')}/></label></>}
    {onboarding&&kind==='api-key'&&reuseAuth&&<p className="provider-saved-credentials">{t('settings.setupSavedCredentials')}</p>}
    {kind === 'api-key' ? <div className={onboarding?`provider-config-group${choice.id==='custom'?' provider-custom-config':''}`:'provider-settings-config'}>
     {onboarding&&builtinProvider&&<>
      {choice.id==='xiaomi'&&<div className="provider-plan-row"><span>{t('runtime.billingMode')}</span><div className="runtime-segments provider-plan-switch" aria-label="MiMo">{options.endpoints.map(endpoint=><button key={endpoint.value} disabled={busy||loading||onboarding&&flow?.stage==='complete'} aria-pressed={baseUrl===endpoint.value} onClick={()=>{setBaseUrl(endpoint.value);setAPIKey('');}}>{/token.plan/i.test(endpoint.value+' '+endpoint.name)?'Token Plan':t('runtime.payAsYouGo')}</button>)}</div></div>}
      {!reuseAuth&&<label className="provider-key-row"><span>{t('connections.apiKey')}</span><input aria-label={t('connections.apiKey')} disabled={busy||loading||onboarding&&flow?.stage==='complete'} type="password" autoComplete="off" spellCheck={false} value={apiKey} onChange={e=>setAPIKey(e.target.value)} placeholder={t('connections.enterApiKey')}/></label>}
     </>}

     {!builtinProvider&&<label>{t(gateway?'runtime.gatewayAddress':'connections.serviceUrl')}<input type="url" list={endpointListID} disabled={busy} value={baseUrl} onChange={e => { setBaseUrl(e.target.value); setAPIKey(''); setModel('');setSelectedModels([]); }} placeholder="https://api.example.com/v1"/><datalist id={endpointListID}>{options.endpoints.map(endpoint => <option key={endpoint.value} value={endpoint.value}>{endpoint.name}</option>)}</datalist></label>}
     {onboarding&&multipleModels?<><label>{t('connections.selectModel')}<SettingsSelect centered={onboarding} searchable={options.models.length>8} aria-label={t('connections.selectModel')} disabled={busy||loading||!options.models.length} value={selectedModels[0]||''} onChange={e=>setSelectedModels([e.target.value])}>{!options.models.length&&<option value="">{t(loading?'common.loading':'runtime.noModelsFound')}</option>}{options.models.map(m=><option key={m.value} value={m.value} title={m.value} data-description={modelDescription(m.parameters?.description,m.value,m.name,friendlyModel(m.name||m.value))}>{onboarding?friendlyModel(m.name||m.value):m.name}</option>)}</SettingsSelect></label>{parameterControls}</>:multipleModels?<fieldset className="provider-model-list" disabled={busy||loading||onboarding&&flow?.stage==='complete'}><legend>{t(onboarding?'connections.selectModel':'connections.model')}{(options.models.length>0||!loading)&&<span className="provider-model-count">{options.models.length}</span>}</legend><div className="provider-model-scroll">{loading&&!options.models.length?<p role="status">{t('common.loading')}</p>:options.models.map(m=><label key={m.value}><input type={onboarding?"radio":"checkbox"} name={modelListID} checked={selectedModels.includes(m.value)} onChange={e=>setSelectedModels(old=>onboarding?[m.value]:e.target.checked?[...old,m.value]:old.filter(id=>id!==m.value))}/><span>{m.name}</span></label>)}{!loading&&!options.models.length&&<p>{t('runtime.noModelsFound')}</p>}</div></fieldset>:<label>{t('connections.model')}{gateway ? <SettingsSelect centered={onboarding} disabled={busy || loading} value={model} onChange={e=>setModel(e.target.value)}><option value="">{t(loading?'common.loading':'connections.selectModel')}</option>{options.models.map(m=><option key={m.value} value={m.value} title={m.value} data-description={modelDescription(m.parameters?.description,m.value,m.name,friendlyModel(m.name||m.value))}>{onboarding?friendlyModel(m.name||m.value):m.name}</option>)}</SettingsSelect> : <input disabled={busy || loading} value={model} list={modelListID} onChange={e => setModel(e.target.value)} placeholder={t('runtime.modelIdPlaceholder')}/>}<datalist id={modelListID}>{options.models.map(m => <option key={m.value} value={m.value} title={m.value} data-description={modelDescription(m.parameters?.description,m.value,m.name,friendlyModel(m.name||m.value))}>{onboarding?friendlyModel(m.name||m.value):m.name}</option>)}</datalist></label>}
     {gateway && !onboarding && <div className="gateway-catalog-status">{!loading&&!error&&!options.models.length&&<p role="status">{t('connections.gatewayEmpty')}</p>}<button className="text-action" disabled={busy||loading||onboarding&&flow?.stage==='complete'} onClick={()=>setOptionsRefresh(n=>n+1)}>{t('runtime.recheck')}</button></div>}
     {!(onboarding&&(builtinProvider||reuseAuth))&&!ollama&&(!gateway || !reuseAuth && !localGateway) && <label>{t('connections.apiKey')}{reuseAuth && <small>{t('connections.reuseAuthHint')}</small>}<input disabled={busy} type="password" autoComplete="off" spellCheck={false} value={apiKey} onChange={e => setAPIKey(e.target.value)} placeholder={reuseAuth ? t('connections.handledByCaelis') : t('connections.enterApiKey')}/></label>}
     {choice.id==='custom' && <>
      <label>{t('connections.contextWindowTokens')}<div className="provider-token-field"><input disabled={busy} aria-label={t('connections.contextWindowTokens')} type="number" min="1" step="1" required value={contextWindow} onChange={e=>setContextWindow(e.target.value)} placeholder="131072"/><span className="provider-field-help" tabIndex={0} role="note" aria-label={t('runtime.contextTokensHelp')}>?<span role="tooltip">{t('runtime.contextTokensHelp')}</span></span></div></label>
      <label>{t('connections.maxOutputTokens')}<div className="provider-token-field"><input disabled={busy} aria-label={t('connections.maxOutputTokens')} type="number" min="1" step="1" required value={maxOutput} onChange={e=>setMaxOutput(e.target.value)} placeholder="32768"/><span className="provider-field-help" tabIndex={0} role="note" aria-label={t('runtime.outputTokensHelp')}>?<span role="tooltip">{t('runtime.outputTokensHelp')}</span></span></div></label>
      <label>{t('connections.reasoningLevels')}<input disabled={busy} value={reasoningLevels} onChange={e=>setReasoningLevels(e.target.value)} placeholder={t('runtime.customReasoningPlaceholder')}/></label>
      <div className="provider-plan-row"><span>{t('connections.imageInput')}</span><div className="runtime-segments provider-plan-switch"><button disabled={busy} aria-pressed={!imageInput} onClick={()=>setImageInput(false)}>{t('runtime.imageNo')}</button><button disabled={busy} aria-pressed={imageInput} onClick={()=>setImageInput(true)}>{t('runtime.imageYes')}</button></div></div>
     </>}

    </div> : null}
    {!(onboarding&&kind==='account')&&<div className="setup-end provider-config-footer">{onboarding&&gateway&&<button className="text-action" disabled={busy||loading||onboarding&&flow?.stage==='complete'} onClick={()=>setOptionsRefresh(n=>n+1)}>{t(loading?'common.loading':'runtime.refreshModels')}</button>}{!onboarding&&<button disabled={busy} onClick={close}>{t('common.cancel')}</button>}<SetupButton className="primary" busy={onboarding&&submitting} disabled={busy || loading || !completing && ((choice.id==='custom' && !validCustomLimits(contextWindow,maxOutput)) || (choice.custom && !command.trim()) || (kind === 'api-key' && ((multipleModels ? !selectedModels.length || selectedModels.some(id=>!options.models.some(m=>m.value===id)) : !model.trim()) || !safeWebURL(baseUrl) || (!ollama && !reuseAuth && !localGateway && !apiKey.trim()))))} onClick={() => completing?void finish():void run()}>{!onboarding&&busy ? t('connections.preparing') : kind === 'account' ? t('connections.continueLogin') : kind === 'agent' ? t('connections.checkAgent') : t(onboarding?(reuseAuth?'settings.setupNext':'settings.setupConnectAndContinue'):'connections.connect')}</SetupButton></div>}
   </>}
  </>}
  {!onboarding&&!flow&&kind==='account'&&choice&&<p className="provider-account-hint">{t('settings.providerAccountHint')}</p>}
  {!onboarding&&completedModels.length>0&&<p className="settings-note" role="status">{t('settings.providerConnectedModels',{models:completedModels.map(id=>options.models.find(m=>m.value===id)?.name||id).join('、')})}</p>}
  {!onboarding&&flow?.stage === 'preparing' && <p className="settings-note" role="status">{t('connections.waitingLocalCaelis')}</p>}
  {flow?.stage === 'launcher' && <><div className="runtime-connect-catalog">{flow.launchers?.map(launcher => <button disabled={busy} key={launcher.id} onClick={() => void run('choose-launcher', { launcher: launcher.id })}><span><strong>{launcher.name || launcher.id}</strong><small>{launcher.description}</small></span><SettingsChevron/></button>)}</div></>}
  {flow?.stage === 'installation' && flow.installation && <>
   <div className="runtime-install-details"><strong>{flow.installation.platform}</strong>{flow.installation.source && <><p>{t('connections.installSource')}</p><button className="text-action runtime-url" onClick={() => void open(flow.installation!.source)}>{flow.installation.source}</button></>}</div>
   <label>{t('connections.installDirectory')}<input value={destination} disabled={busy} onChange={e => setDestination(e.target.value)}/></label>
   <p className="settings-note">{flow.installation.canInstall ? t('connections.canInstallHint') : t('connections.manualInstallHint')}</p>
   <button className="text-action" disabled={busy} onClick={() => setManual(!manual)}>{manual ? t('connections.hideManualInstructions') : t('connections.manualInstall')}</button>
   {(manual || !flow.installation.canInstall) && <pre className="runtime-install-instructions">{flow.installation.instructions}</pre>}
   <div className="setup-end"><button disabled={busy} onClick={() => void run('check-installation', { destination })}>{t('connections.checkInstallation')}</button>{!manual && <button className="primary" disabled={busy || !destination.trim() || !flow.installation.canInstall} onClick={() => void run('install', { destination })}>{busy ? t('connections.installing') : t('connections.installAndContinue')}</button>}</div>
  </>}
  {flow?.stage === 'auth-method' && <>
   <fieldset disabled={busy} className="runtime-auth-methods"><legend>{t('connections.chooseAuthMethod')}</legend>{flow.methods?.map(m => <label key={m.id}><input type="radio" name="runtime-auth-method" value={m.id} checked={method === m.id} disabled={!m.available} onChange={() => setMethod(m.id)}/><span><strong>{m.name}</strong><small>{m.reason || m.description}</small></span></label>)}</fieldset>
   <div className="setup-end"><SetupButton className="primary" busy={onboarding&&busy} disabled={busy || !flow.methods?.some(m => m.id === method && m.available)} onClick={() => void run('authenticate', { method })}>{t('connections.continueAuth')}</SetupButton></div>
  </>}
  {oauthPage && flow.authorization && <section className="onboarding-oauth" aria-labelledby="provider-oauth-title">
   <img className="onboarding-oauth-app-icon" src={appIcon} alt="Caelis Bot"/>
   <h1 id="provider-oauth-title">{t('connections.oauthTitle',{provider:choice?.id==='grok'?'Grok':'ChatGPT'})}</h1>
   <p className="setup-lead" role="status">{t(expired?'connections.authExpired':'connections.oauthBrowserHint')}</p>
   {flow.authorization.userCode && <div className="runtime-device-code"><span>{t('connections.deviceCode')}</span><code>{flow.authorization.userCode}</code></div>}
   <div className="onboarding-oauth-actions"><button className="primary" disabled={expired || !safeWebURL(flow.authorization.url)} onClick={() => void open(flow.authorization!.url)}>{t('runtime.oauthBrowserAction')}</button></div>
   <p className="onboarding-oauth-footnote">{t('runtime.oauthContinueHint')}</p>
   {flow.authorization.inputLabel && <details className="oauth-manual"><summary>{t('connections.oauthManual')}</summary><form onSubmit={e=>{e.preventDefault();if(!expired&&flow.authorization?.canSubmit)void run('submit-code',{code});}}><label>{t('connections.oauthResponse')}<input type="password" autoComplete="off" spellCheck={false} value={code} disabled={busy||expired||!flow.authorization.canSubmit} onChange={e=>setCode(e.target.value)}/></label><SetupButton busy={busy} disabled={expired||!code.trim()||!flow.authorization.canSubmit}>{t('connections.submitAuthInfo')}</SetupButton></form></details>}
   <SetupButton className="text-action oauth-refresh" busy={busy} onClick={()=>void run('refresh')}>{t('connections.checkAuthStatus')}</SetupButton>
  </section>}
  {!onboarding && flow?.stage === 'authorization' && flow.authorization && <>
   <div className="runtime-authorization"><p>{expired ? t('connections.authExpired') : t('connections.completeAuthInBrowser')}</p>{flow.authorization.userCode && <div className="runtime-device-code"><span>{t('connections.deviceCode')}</span><code>{flow.authorization.userCode}</code></div>}
    <button disabled={expired || !safeWebURL(flow.authorization.url)} onClick={() => void open(flow.authorization!.url)}>{t('connections.openLoginPage')}</button>
    <p className="runtime-url">{flow.authorization.url}</p>{flow.authorization.expiresAt && <small>{t('connections.validUntil', { time: date(new Date(flow.authorization.expiresAt), { hour: 'numeric', minute: '2-digit', second: '2-digit' }) })}</small>}
   </div>
   {flow.authorization.inputLabel && <form onSubmit={e => { e.preventDefault(); if (!expired && flow.authorization?.canSubmit) void run('submit-code', { code }); }}><label>{flow.authorization.inputLabel}<input type="password" autoComplete="off" spellCheck={false} value={code} disabled={busy || expired || !flow.authorization.canSubmit} onChange={e => setCode(e.target.value)}/></label><div className="setup-end"><button disabled={busy || expired || !code.trim() || !flow.authorization.canSubmit}>{t('connections.submitAuthInfo')}</button></div></form>}
   {!flow.authorization.inputLabel && <p className="settings-note" role="status">{t('connections.waitingRuntimeConfirmation')}</p>}
   <button className="text-action" disabled={busy} onClick={() => void run('refresh')}>{t('connections.checkAuthStatus')}</button>
  </>}
  {showModels && <>{onboarding?<div className="provider-config-group"><label>{t('connections.selectModel')}<SettingsSelect centered={onboarding} searchable={(modelChoices?.length||0)>8} aria-label={t('connections.selectModel')} disabled={busy||completing} value={model} onChange={e=>setModel(e.target.value)}>{modelChoices?.map(m=><option key={m.id} value={m.id} title={m.id} data-description={modelDescription(m.description,m.id,m.name,friendlyModel(m.name||m.id))}>{friendlyModel(m.name||m.id)}</option>)}</SettingsSelect></label>{parameterControls}</div>:<fieldset className="provider-model-list provider-account-models" disabled={busy||completing}><legend>{t('connections.selectModel')}</legend><div className="provider-model-scroll">{modelChoices?.map(m=><label key={m.id}><input type="radio" name={modelListID} checked={model===m.id} onChange={()=>setModel(m.id)}/><span>{m.name}{!onboarding&&m.description&&<small>{m.description}</small>}</span></label>)}</div>{!modelChoices?.length&&<p role="status">{t('runtime.noModelsFound')}</p>}</fieldset>}<div className="setup-end"><SetupButton className="primary" busy={onboarding&&submitting} disabled={busy || !modelChoices?.some(m => m.id === model)} onClick={() => completing?void finish():void run('connect', { model })}>{t(onboarding?'settings.setupNext':'connections.connectModel')}</SetupButton></div></>}

  {flow && (!onboarding||flow.stage!=='complete') && ['complete', 'failed', 'unknown'].includes(flow.stage) && <div className="setup-end">{flow.stage === 'unknown' && flow.id && <button disabled={busy} onClick={() => void run('refresh')}>{t('connections.reconcileResult')}</button>}<button className={flow.stage === 'complete' ? 'primary' : ''} disabled={busy} onClick={() => void finish()}>{flow.stage === 'complete' ? t('connections.done') : t('connections.closeAndRefresh')}</button></div>}
  {!onboarding&&flow?.id && !['complete', 'failed', 'unknown'].includes(flow.stage) && <button className="text-action" disabled={canceling} onClick={() => void cancel()}>{canceling ? t('connections.canceling') : t('connections.cancelConnection')}</button>}
  {error && <p role="alert" className="inline-error">{error}</p>}
 </Surface>;
}
