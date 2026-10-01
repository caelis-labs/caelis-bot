import { useEffectEvent, useEffect, useId, useRef, useState } from 'react';
import type { APIKeyOptions, ConnectAction, ConnectChoice, ConnectionCatalog, ConnectionFlow, ConnectionKind, FlowInput, RuntimeSettingsClient } from './types';
import { acceptConnectionProgress, messageOf, safeWebURL } from './state';
import { SettingsDialog } from './SettingsDialog';
import { useI18n } from '../../i18n';

const emptyCatalog: ConnectionCatalog = { choices: [], unavailable: '' };
export function ConnectionWizard({ client, onClose, onConnected }: { client: RuntimeSettingsClient; onClose: () => void; onConnected: () => Promise<void> }) {
 const { t, date } = useI18n();
 const actionFailed=useEffectEvent(()=>t('runtime.actionFailed'));
 const [kind, setKind] = useState<ConnectionKind>('account'), [catalog, setCatalog] = useState(emptyCatalog), [choice, setChoice] = useState<ConnectChoice | null>(null);
 const [options, setOptions] = useState<APIKeyOptions>({ endpoints: [], models: [] });
 const [metadata, setMetadata] = useState(false), [contextWindow, setContextWindow] = useState(''), [maxOutput, setMaxOutput] = useState(''), [imageInput, setImageInput] = useState(false), [reasoningLevels, setReasoningLevels] = useState('');
 const [baseUrl, setBaseUrl] = useState(''), [model, setModel] = useState(''), [apiKey, setAPIKey] = useState(''), [command, setCommand] = useState('');
 const [flow, setFlow] = useState<ConnectionFlow | null>(null), [code, setCode] = useState(''), [method, setMethod] = useState(''), [destination, setDestination] = useState(''), [manual, setManual] = useState(false);
 const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false), [error, setError] = useState(''), [canceling, setCanceling] = useState(false), [now, setNow] = useState(Date.now());
 const finishing=useRef(false),cancelWorking=useRef(false),mountedClient=useRef(client);mountedClient.current=client;
 const alive = useRef(true), working = useRef(false), serial = useRef(0), abort = useRef<AbortController | null>(null), optionsSerial = useRef(0), modelListID = useId(), endpointListID = useId();
 useEffect(() => { const clear = () => { setAPIKey(''); setCode(''); }; window.addEventListener('settings-close', clear); return () => window.removeEventListener('settings-close', clear); }, []);
 useEffect(() => { alive.current = true; return () => { alive.current = false; serial.current++; abort.current?.abort();queueMicrotask(()=>{if(!alive.current||mountedClient.current!==client)void client.closeConnection?.().catch(()=>{});}); }; }, [client]);
 useEffect(() => {
  let valid = true; setLoading(true); setCatalog(emptyCatalog); setChoice(null); setFlow(null); setAPIKey(''); setError('');
  void client.catalog(kind).then(value => { if (valid) setCatalog(value); }).catch(e => { if (valid) setError(messageOf(e, actionFailed())); }).finally(() => { if (valid) setLoading(false); });
  return () => { valid = false; };
 }, [client, kind]);
 useEffect(() => {
  if (!choice || kind !== 'api-key') return;
  const seq = ++optionsSerial.current;
  setLoading(true); setOptions({ endpoints: [], models: [] }); setAPIKey(''); setError('');
  void client.apiKeyOptions(choice.id, baseUrl).then(value => {
   if (!alive.current || seq !== optionsSerial.current) return;
   setOptions(value);
   if (!baseUrl && value.endpoints[0]?.value) setBaseUrl(value.endpoints[0].value);
  }).catch(e => { if (alive.current && seq === optionsSerial.current) setError(messageOf(e, actionFailed())); }).finally(() => { if (alive.current && seq === optionsSerial.current) setLoading(false); });
  return () => { optionsSerial.current++; };
 }, [client, choice, kind, baseUrl]);
 useEffect(() => { if (!flow?.authorization?.expiresAt) return; const timer = window.setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, [flow?.authorization?.expiresAt]);
 const expired = !!flow?.authorization?.expiresAt && Date.parse(flow.authorization.expiresAt) <= now;
 const reuseAuth = options.endpoints.some(endpoint => endpoint.value === baseUrl && endpoint.reuseAuth);
 const select = (value: ConnectChoice) => { setChoice(value); setBaseUrl(''); setModel(''); setCommand(''); setMetadata(false); setContextWindow(''); setMaxOutput(''); setImageInput(false); setReasoningLevels(''); setAPIKey(''); setError(''); };
 const observe = (seq: number, update: ConnectionFlow) => {
  if (!alive.current || seq !== serial.current) return;
  setFlow(current => acceptConnectionProgress(current, update));
  if (update.installation) setDestination(current => current || update.installation!.destination);
  if (update.stage !== 'authorization' || !update.authorization?.canSubmit) setCode('');
 };
 const run = async (action?: ConnectAction, input: FlowInput = {}) => {
  if (working.current || finishing.current || cancelWorking.current || !choice) return;
  abort.current?.abort();
  const seq = ++serial.current, controller = new AbortController(); abort.current = controller;
  working.current = true; setBusy(true); setError('');
  // Secret drafts are only write arguments. Never put them in a flow snapshot,
  // URL, local storage, logs, operation key or a retry closure.
  const secret = apiKey; setAPIKey(''); setCode('');
  try {
   const next = flow && action ? await client.advanceConnection(flow, action, input, controller.signal, value => observe(seq, value)) :
    await client.startConnection({ kind, choice: choice.id, ...(choice.custom ? { command } : {}), ...(kind === 'api-key' ? { baseUrl, model, apiKey: secret, ...(metadata ? { contextWindowTokens: Number(contextWindow), maxOutputTokens: Number(maxOutput), imageInput, reasoningLevels: reasoningLevels.split(',').map(s => s.trim()).filter(Boolean) } : {}) } : {}) }, controller.signal, value => observe(seq, value));
   observe(seq, next);
  } catch {
   if (alive.current && seq === serial.current && !controller.signal.aborted) {
    setFlow(current => current?.stage === 'complete' ? current : ({ id: current?.id || '', revision: current?.revision || '', sequence: current?.sequence || 0, stage: 'unknown', title: t('connections.unknownResultTitle'), message: t('connections.unknownResultMessage') }));
   }
  } finally { if (alive.current && seq === serial.current) { working.current = false; setBusy(false); } }
 };
 const finish = async () => {
  if(finishing.current)return;finishing.current=true;setCanceling(true);setAPIKey('');setCode('');
  try {await client.closeConnection?.();if(alive.current){onClose();await onConnected();}}
  catch {if(alive.current)setError(t('connections.cancelNotConfirmed'));}
  finally {finishing.current=false;if(alive.current)setCanceling(false);}
 };
 const cancel = async () => {
  if (cancelWorking.current||finishing.current) return;
  if (!flow?.id || ['complete', 'failed', 'unknown'].includes(flow.stage)) { await finish(); return; }
  cancelWorking.current=true;setCanceling(true); setError('');
  try {
   await client.cancelConnection(flow);
   serial.current++; abort.current?.abort(); setCode(''); await finish();
  } catch { if (alive.current) setError(t('connections.cancelNotConfirmed')); }
  finally { cancelWorking.current=false;if (alive.current) setCanceling(false); }
 };
 const open = async (url: string) => { if (!safeWebURL(url)) { setError(t('connections.invalidUrl')); return; } try { await client.openURL(url); } catch { setError(t('connections.cannotOpenBrowser')); } };
 const close = () => { setAPIKey(''); setCode(''); if (!flow) void finish(); else void cancel(); };
 const blocked=busy||canceling;
 return <SettingsDialog title={flow?.title || t('connections.addConnection')} description={flow?.message || undefined} busy={busy || canceling} onClose={close}>
  {!flow && <>
   <div className="runtime-segments" aria-label={t('connections.connectionTypeAria')}><button disabled={blocked} aria-pressed={kind !== 'agent'} onClick={() => setKind('account')}>{t('connections.modelService')}</button><button disabled={blocked} aria-pressed={kind === 'agent'} onClick={() => setKind('agent')}>{t('connections.externalAgent')}</button></div>
   {kind !== 'agent' && <div className="runtime-auth-tabs"><button disabled={blocked} aria-pressed={kind === 'account'} onClick={() => setKind('account')}>{t('connections.accountLogin')}</button><button disabled={blocked} aria-pressed={kind === 'api-key'} onClick={() => setKind('api-key')}>{t('connections.apiKey')}</button></div>}
   {loading && !choice && <p role="status">{t('connections.loadingChoices')}</p>}
   {catalog.unavailable && <p className="settings-note" role="status">{catalog.unavailable}</p>}
   {!loading && !catalog.unavailable && !catalog.choices.length && <p className="settings-note">{t('connections.noChoices')}</p>}
   {!choice ? <div className="runtime-connect-catalog">{catalog.choices.map(c => <button key={c.id} disabled={blocked} onClick={() => select(c)}><span><strong>{c.name}</strong><small>{c.description}</small></span><span aria-hidden="true">›</span></button>)}</div> : <>
    <button className="text-action" disabled={blocked} onClick={() => { optionsSerial.current++; setChoice(null); setAPIKey(''); setError(''); }}>{t('connections.backToChoices')}</button>
    <h3 className="runtime-connect-choice">{choice.name}</h3>
    {choice.custom && <><label>{t('connections.startCommand')}<input disabled={blocked} value={command} onChange={e => setCommand(e.target.value)} placeholder={t('connections.startCommandPlaceholder')}/></label></>}
    {kind === 'api-key' ? <>
     <label>{t('connections.serviceUrl')}<input type="url" list={endpointListID} disabled={blocked} value={baseUrl} onChange={e => { setBaseUrl(e.target.value); setAPIKey(''); setModel(''); }} placeholder="https://…/v1"/><datalist id={endpointListID}>{options.endpoints.map(endpoint => <option key={endpoint.value} value={endpoint.value}>{endpoint.name}</option>)}</datalist></label>
     <label>{t('connections.model')}<input disabled={blocked || loading} value={model} list={modelListID} onChange={e => setModel(e.target.value)} placeholder={t('connections.modelPlaceholder')}/><datalist id={modelListID}>{options.models.map(m => <option key={m.value} value={m.value}>{m.name}</option>)}</datalist></label>
     <label>{t('connections.apiKey')}{reuseAuth && <small>{t('connections.reuseAuthHint')}</small>}<input disabled={blocked} type="password" autoComplete="off" spellCheck={false} value={apiKey} onChange={e => setAPIKey(e.target.value)} placeholder={reuseAuth ? t('connections.handledByCaelis') : t('connections.enterApiKey')}/></label>
     <details onToggle={e => setMetadata(e.currentTarget.open)}><summary>{t('connections.customCapabilities')}</summary><p className="settings-note">{t('connections.customCapabilitiesHint')}</p><div className="runtime-model-options-row"><label>{t('connections.contextWindowTokens')}<input type="number" min="1" step="1" value={contextWindow} onChange={e => setContextWindow(e.target.value)}/></label><label>{t('connections.maxOutputTokens')}<input type="number" min="1" step="1" value={maxOutput} onChange={e => setMaxOutput(e.target.value)}/></label></div><label>{t('connections.reasoningLevels')}<input value={reasoningLevels} onChange={e => setReasoningLevels(e.target.value)} placeholder={t('connections.reasoningLevelsPlaceholder')}/></label><label className="runtime-check"><input type="checkbox" checked={imageInput} onChange={e => setImageInput(e.target.checked)}/>{t('connections.imageInput')}</label></details>
    </> : null}
    <div className="setup-end"><button disabled={blocked} onClick={close}>{t('common.cancel')}</button><button className="primary" disabled={blocked || loading || (choice.custom && !command.trim()) || (kind === 'api-key' && (!model.trim() || !safeWebURL(baseUrl) || (!reuseAuth && !apiKey.trim())))} onClick={() => void run()}>{busy ? t('connections.preparing') : kind === 'account' ? t('connections.continueLogin') : kind === 'agent' ? t('connections.checkAgent') : t('connections.connect')}</button></div>
   </>}
  </>}
  {flow?.stage === 'preparing' && <p className="settings-note" role="status">{t('connections.waitingLocalCaelis')}</p>}
  {flow?.stage === 'launcher' && <><div className="runtime-connect-catalog">{flow.launchers?.map(launcher => <button disabled={blocked} key={launcher.id} onClick={() => void run('choose-launcher', { launcher: launcher.id })}><span><strong>{launcher.name || launcher.id}</strong><small>{launcher.description}</small></span><span aria-hidden="true">›</span></button>)}</div></>}
  {flow?.stage === 'installation' && flow.installation && <>
   <div className="runtime-install-details"><strong>{flow.installation.platform}</strong>{flow.installation.source && <><p>{t('connections.installSource')}</p><button className="text-action runtime-url" onClick={() => void open(flow.installation!.source)}>{flow.installation.source}</button></>}</div>
   <label>{t('connections.installDirectory')}<input value={destination} disabled={blocked} onChange={e => setDestination(e.target.value)}/></label>
   <p className="settings-note">{flow.installation.canInstall ? t('connections.canInstallHint') : t('connections.manualInstallHint')}</p>
   <button className="text-action" disabled={blocked} onClick={() => setManual(!manual)}>{manual ? t('connections.hideManualInstructions') : t('connections.manualInstall')}</button>
   {(manual || !flow.installation.canInstall) && <pre className="runtime-install-instructions">{flow.installation.instructions}</pre>}
   <div className="setup-end"><button disabled={blocked} onClick={() => void run('check-installation', { destination })}>{t('connections.checkInstallation')}</button>{!manual && <button className="primary" disabled={blocked || !destination.trim() || !flow.installation.canInstall} onClick={() => void run('install', { destination })}>{busy ? t('connections.installing') : t('connections.installAndContinue')}</button>}</div>
  </>}
  {flow?.stage === 'auth-method' && <>
   <fieldset disabled={blocked} className="runtime-auth-methods"><legend>{t('connections.chooseAuthMethod')}</legend>{flow.methods?.map(m => <label key={m.id}><input type="radio" name="runtime-auth-method" value={m.id} checked={method === m.id} disabled={!m.available} onChange={() => setMethod(m.id)}/><span><strong>{m.name}</strong><small>{m.reason || m.description}</small></span></label>)}</fieldset>
   <div className="setup-end"><button className="primary" disabled={blocked || !flow.methods?.some(m => m.id === method && m.available)} onClick={() => void run('authenticate', { method })}>{t('connections.continueAuth')}</button></div>
  </>}
  {flow?.stage === 'authorization' && flow.authorization && <>
   <div className="runtime-authorization"><p>{expired ? t('connections.authExpired') : t('connections.completeAuthInBrowser')}</p>{flow.authorization.userCode && <div className="runtime-device-code"><span>{t('connections.deviceCode')}</span><code>{flow.authorization.userCode}</code></div>}
    <button disabled={expired || !safeWebURL(flow.authorization.url)} onClick={() => void open(flow.authorization!.url)}>{t('connections.openLoginPage')}</button>
    <p className="runtime-url">{flow.authorization.url}</p>{flow.authorization.expiresAt && <small>{t('connections.validUntil', { time: date(new Date(flow.authorization.expiresAt), { hour: 'numeric', minute: '2-digit', second: '2-digit' }) })}</small>}
   </div>
   {flow.authorization.inputLabel && <form onSubmit={e => { e.preventDefault(); if (!expired && flow.authorization?.canSubmit) void run('submit-code', { code }); }}><label>{flow.authorization.inputLabel}<input type="password" autoComplete="off" spellCheck={false} value={code} disabled={blocked || expired || !flow.authorization.canSubmit} onChange={e => setCode(e.target.value)}/></label><div className="setup-end"><button disabled={blocked || expired || !code.trim() || !flow.authorization.canSubmit}>{t('connections.submitAuthInfo')}</button></div></form>}
   {!flow.authorization.inputLabel && <p className="settings-note" role="status">{t('connections.waitingRuntimeConfirmation')}</p>}
   <button className="text-action" disabled={blocked} onClick={() => void run('refresh')}>{t('connections.checkAuthStatus')}</button>
  </>}
  {flow?.stage === 'models' && <><label>{t('connections.useModel')}<select disabled={blocked} value={model} onChange={e => setModel(e.target.value)}><option value="">{t('connections.selectModel')}</option>{flow.models?.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</select></label><div className="setup-end"><button className="primary" disabled={blocked || !flow.models?.some(m => m.id === model)} onClick={() => void run('connect', { model })}>{t('connections.connectModel')}</button></div></>}
  {flow && ['complete', 'failed', 'unknown'].includes(flow.stage) && <div className="setup-end">{flow.stage === 'unknown' && flow.id && <button disabled={blocked} onClick={() => void run('refresh')}>{t('connections.reconcileResult')}</button>}<button className={flow.stage === 'complete' ? 'primary' : ''} disabled={blocked} onClick={() => void finish()}>{flow.stage === 'complete' ? t('connections.done') : t('connections.closeAndRefresh')}</button></div>}
  {flow?.id && !['complete', 'failed', 'unknown'].includes(flow.stage) && <button className="text-action" disabled={canceling} onClick={() => void cancel()}>{canceling ? t('connections.canceling') : t('connections.cancelConnection')}</button>}
  {error && <p role="alert" className="inline-error">{error}</p>}
 </SettingsDialog>;
}
