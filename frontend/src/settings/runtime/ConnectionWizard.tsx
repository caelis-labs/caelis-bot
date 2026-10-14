import { useEffectEvent, useEffect, useId, useRef, useState } from 'react';
import type { APIKeyOptions, ConnectAction, ConnectChoice, ConnectionCatalog, ConnectionFlow, ConnectionKind, FlowInput, RuntimeSettingsClient } from './types';
import { acceptConnectionProgress, messageOf, safeWebURL } from './state';
import { SettingsDialog } from './SettingsDialog';
import { useI18n } from '../../i18n';
import type { MessageKey } from '../../i18n/catalogs';
import { SettingsChevron } from '../../SettingsIcons';
import { connectSelectedModels } from './connection-sequence';
import { defaultChatModels, eligibleSelection, requiresManualCapabilities } from './connection-models';
import './connection-wizard.css';

const emptyCatalog: ConnectionCatalog = { choices: [], unavailable: '' };
// The host supplies the same connection client in first-run and settings. Only
// the first-run parent advances to the assistant introduction after refresh.
export function ConnectionWizard({ client, onClose, onConnected, active=true, scene='settings' }: { client: RuntimeSettingsClient; onClose: () => void; onConnected: () => Promise<void>; active?:boolean; scene?:'first-run'|'settings' }) {
 const { t, date } = useI18n();
 const actionFailed=useEffectEvent(()=>t('runtime.actionFailed'));
 const [kind, setKind] = useState<ConnectionKind>('api-key'), [catalog, setCatalog] = useState(emptyCatalog), [choice, setChoice] = useState<ConnectChoice | null>(null);
 const [options, setOptions] = useState<APIKeyOptions>({ endpoints: [], models: [] });
 const [metadata, setMetadata] = useState(false), [contextWindow, setContextWindow] = useState(''), [maxOutput, setMaxOutput] = useState(''), [imageInput, setImageInput] = useState(false), [reasoningLevels, setReasoningLevels] = useState('');
 const [baseUrl, setBaseUrl] = useState(''), [model, setModel] = useState(''), [apiKey, setAPIKey] = useState(''), [command, setCommand] = useState(''), [gatewayManual, setGatewayManual] = useState(false), [discovery, setDiscovery] = useState(0);
 const [providerStep, setProviderStep] = useState<'key'|'models'>('key'), [selectedModels, setSelectedModels] = useState<string[]>([]), [manualModel, setManualModel] = useState(false), [addressOpen, setAddressOpen] = useState(false), [batchCompleted, setBatchCompleted] = useState(0), [batchTotal, setBatchTotal] = useState(0);
 const [flow, setFlow] = useState<ConnectionFlow | null>(null), [code, setCode] = useState(''), [method, setMethod] = useState(''), [destination, setDestination] = useState(''), [manual, setManual] = useState(false), [manualUnverified, setManualUnverified] = useState(false);
 const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false), [error, setError] = useState(''), [canceling, setCanceling] = useState(false), [now, setNow] = useState(Date.now());
 const alive = useRef(true), working = useRef(false), serial = useRef(0), abort = useRef<AbortController | null>(null), optionsSerial = useRef(0), modelListID = useId(), endpointListID = useId(), selectionTouched = useRef(false), completed = useRef(false), confirmedModels = useRef(new Set<string>());
 useEffect(() => { const clear = () => { setAPIKey(''); setCode(''); }; window.addEventListener('settings-close', clear); return () => window.removeEventListener('settings-close', clear); }, []);
 useEffect(() => { alive.current = true; return () => { alive.current = false; serial.current++; abort.current?.abort(); }; }, []);
 useEffect(() => {
  let valid = true; setLoading(true); setCatalog(emptyCatalog); setChoice(null); setFlow(null); setAPIKey(''); setError('');
  void client.catalog(kind).then(value => { if (valid) setCatalog(value); }).catch(e => { if (valid) setError(messageOf(e, actionFailed())); }).finally(() => { if (valid) setLoading(false); });
  return () => { valid = false; };
 }, [client, kind]);
 useEffect(() => {
  if (!choice || kind !== 'api-key') return;
  const seq = ++optionsSerial.current;
  setLoading(true); setOptions({ endpoints: [], models: [] }); setError('');
  void client.apiKeyOptions(choice.id, '').then(value => {
   if (!alive.current || seq !== optionsSerial.current) return;
   setOptions(value);
   if (!selectionTouched.current) setSelectedModels(defaultChatModels(value.models));
   if (!baseUrl && value.endpoints[0]?.value) setBaseUrl(value.endpoints[0].value);
  }).catch(e => { if (alive.current && seq === optionsSerial.current) setError(messageOf(e, actionFailed())); }).finally(() => { if (alive.current && seq === optionsSerial.current) setLoading(false); });
  return () => { optionsSerial.current++; };
 }, [client, choice, kind, discovery]);
 useEffect(() => {
  if (!choice || kind !== 'api-key' || !baseUrl) return;
  const seq = ++optionsSerial.current;
  setLoading(true);
  const timer = window.setTimeout(() => { void client.apiKeyOptions(choice.id, baseUrl).then(value => { if (alive.current && seq === optionsSerial.current) { setOptions(value); if (!selectionTouched.current) setSelectedModels(defaultChatModels(value.models)); } }).catch(() => { if (alive.current && seq === optionsSerial.current) setOptions(current => ({ ...current, models: [] })); }).finally(() => { if (alive.current && seq === optionsSerial.current) setLoading(false); }); }, 350);
  return () => { clearTimeout(timer); optionsSerial.current++; };
 }, [client, choice, kind, baseUrl]);
 useEffect(() => { if (!active||!flow?.authorization?.expiresAt) return; setNow(Date.now()); const timer = window.setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, [active,flow?.authorization?.expiresAt]);
 const expired = !!flow?.authorization?.expiresAt && Date.parse(flow.authorization.expiresAt) <= now;
 const reuseAuth = options.endpoints.some(endpoint => endpoint.value === baseUrl && endpoint.reuseAuth);
 const magpie = choice?.id.toLowerCase() === 'magpie';
 const customProvider = choice?.id.toLowerCase() === 'custom' || magpie;
 const manualChoice = options.models.find(item => item.value === model.trim());
 const needsManualCapabilities = (customProvider || manualModel) && requiresManualCapabilities(manualChoice);
 const customCapabilitiesVisible = choice?.id.toLowerCase() === 'custom' || (magpie || manualModel) && (!manualChoice || manualChoice.chatEligibility !== 'chat');
 const manualCapabilitiesValid = !needsManualCapabilities || metadata && Number.isSafeInteger(Number(contextWindow)) && Number(contextWindow) > 0 && Number.isSafeInteger(Number(maxOutput)) && Number(maxOutput) > 0 && Number(maxOutput) <= Number(contextWindow);
 const catalogSelection = eligibleSelection(options.models, selectedModels, confirmedModels.current);
 const manualChatRejected = manualChoice?.chatEligibility === 'non-chat';
 const gatewayFormVisible = !magpie || gatewayManual || options.models.length > 0;
 const providerChoices = catalog.choices.filter(c => c.id.toLowerCase() !== 'magpie');
 const magpieChoice = catalog.choices.find(c => c.id.toLowerCase() === 'magpie');
 const knownProviders = new Set(['deepseek', 'xiaomi', 'ollama', 'custom']);
 const providerText = (value: ConnectChoice, part: 'name' | 'description') => {
  const provider = value.id.toLowerCase();
  if (kind === 'api-key' && provider === 'magpie') return part === 'name' ? 'Magpie' : t('connections.magpieDescription');
  if (kind === 'api-key' && knownProviders.has(provider)) return t(`connections.provider_${provider}_${part}` as MessageKey);
  return part === 'name' ? value.name || value.id : value.description;
 };
 const select = (value: ConnectChoice) => { selectionTouched.current = false; confirmedModels.current.clear(); setChoice(value); setBaseUrl(''); setModel(''); setCommand(''); setGatewayManual(false); setMetadata(false); setContextWindow(''); setMaxOutput(''); setImageInput(false); setReasoningLevels(''); setAPIKey(''); setProviderStep('key'); setSelectedModels([]); setManualModel(false); setManualUnverified(false); setAddressOpen(false); setBatchCompleted(0); setBatchTotal(0); setError(''); };
 const observe = (seq: number, update: ConnectionFlow) => {
  if (!alive.current || seq !== serial.current) return;
  setFlow(current => acceptConnectionProgress(current, update));
  if (update.installation) setDestination(current => current || update.installation!.destination);
  if (update.stage !== 'authorization' || !update.authorization?.canSubmit) setCode('');
 };
 const run = async (action?: ConnectAction, input: FlowInput = {}) => {
  if (working.current || !choice) return;
  abort.current?.abort();
  const seq = ++serial.current, controller = new AbortController(); abort.current = controller;
  working.current = true; setBusy(true); setError('');
  // Secret drafts are only write arguments. Never put them in a flow snapshot,
  // URL, local storage, logs, operation key or a retry closure.
  const secret = apiKey; setAPIKey(''); setCode('');
  try {
   if (kind === 'api-key' && !flow && !customProvider && !manualModel && catalogSelection.length) {
    setBatchTotal(catalogSelection.length);
    await connectSelectedModels(client, { kind, choice: choice.id, baseUrl, apiKey: secret }, catalogSelection, controller.signal, value => observe(seq, value), count => { for (const id of catalogSelection.slice(0, count)) confirmedModels.current.add(id); selectionTouched.current = true; setSelectedModels(current => current.filter(id => !confirmedModels.current.has(id))); setBatchCompleted(count); });
   } else {
    if (kind === 'api-key' && !flow && (customProvider || manualModel)) setManualUnverified(needsManualCapabilities);
    const next = flow && action ? await client.advanceConnection(flow, action, input, controller.signal, value => observe(seq, value)) :
     await client.startConnection({ kind, choice: choice.id, ...(choice.custom ? { command } : {}), ...(kind === 'api-key' ? { baseUrl, model, apiKey: secret, ...(metadata && customCapabilitiesVisible ? { contextWindowTokens: Number(contextWindow), maxOutputTokens: Number(maxOutput), imageInput, reasoningLevels: reasoningLevels.split(',').map(s => s.trim()).filter(Boolean) } : {}) } : {}) }, controller.signal, value => observe(seq, value));
    observe(seq, next);
   }
  } catch {
   if (alive.current && seq === serial.current && !controller.signal.aborted) {
    setFlow(current => current?.stage === 'complete' ? current : ({ id: current?.id || '', revision: current?.revision || '', sequence: current?.sequence || 0, stage: 'unknown', title: t('connections.unknownResultTitle'), message: t('connections.unknownResultMessage') }));
   }
  } finally { if (alive.current && seq === serial.current) { working.current = false; setBusy(false); } }
 };
 const finish = async () => { onClose(); await onConnected(); };
 useEffect(() => {
  if (flow?.stage !== 'complete' || busy || completed.current || manualUnverified || batchTotal > 0 && batchCompleted < batchTotal) return;
  completed.current = true;
  void finish();
 }, [flow?.stage, busy, manualUnverified, batchTotal, batchCompleted]);
 const cancel = async () => {
  if (canceling) return;
  if (!flow?.id || ['complete', 'failed', 'unknown'].includes(flow.stage)) { await finish(); return; }
  setCanceling(true); setError('');
  try {
   await client.cancelConnection(flow);
   serial.current++; abort.current?.abort(); setCode(''); onClose();
  } catch { if (alive.current) setError(t('connections.cancelNotConfirmed')); }
  finally { if (alive.current) setCanceling(false); }
 };
 const open = async (url: string) => { if (!safeWebURL(url)) { setError(t('connections.invalidUrl')); return; } try { await client.openURL(url); } catch { setError(t('connections.cannotOpenBrowser')); } };
 const close = () => { setAPIKey(''); setCode(''); if (!flow) onClose(); else void cancel(); };
 return <SettingsDialog className={`connection-wizard connection-wizard-${scene}`} title={flow?.stage === 'complete' ? t('connections.addConnection') : flow?.title || t('connections.addConnection')} description={flow?.stage === 'complete' ? undefined : flow?.message || undefined} busy={busy || canceling} onClose={close}>
  {!flow && <>
   <div className="runtime-segments" aria-label={t('connections.connectionTypeAria')}><button disabled={busy} aria-pressed={kind !== 'agent'} onClick={() => setKind('account')}>{t('connections.modelService')}</button><button disabled={busy} aria-pressed={kind === 'agent'} onClick={() => setKind('agent')}>{t('connections.externalAgent')}</button></div>
   {kind !== 'agent' && <div className="runtime-auth-tabs"><button disabled={busy} aria-pressed={kind === 'account'} onClick={() => setKind('account')}>{t('connections.accountLogin')}</button><button disabled={busy} aria-pressed={kind === 'api-key'} onClick={() => setKind('api-key')}>{t('connections.apiKey')}</button></div>}
   {loading && !choice && <p role="status">{t('connections.loadingChoices')}</p>}
   {catalog.unavailable && <p className="settings-note" role="status">{catalog.unavailable}</p>}
   {!loading && !catalog.unavailable && !catalog.choices.length && <p className="settings-note">{t('connections.noChoices')}</p>}
   {!choice ? <><div className="runtime-connect-catalog">{providerChoices.map(c => <button key={c.id} disabled={busy} onClick={() => select(c)}><span><strong>{providerText(c, 'name')}</strong><small>{providerText(c, 'description')}</small></span><SettingsChevron/></button>)}</div>{kind === 'api-key' && magpieChoice && <><h3 className="runtime-gateway-heading">{t('connections.thirdPartyGateway')}</h3><div className="runtime-connect-catalog"><button disabled={busy} onClick={() => select(magpieChoice)}><span><strong>Magpie</strong><small>{t('connections.magpieDescription')}</small></span><SettingsChevron/></button></div></>}</> : <>
    <button className="text-action" disabled={busy} onClick={() => { optionsSerial.current++; setChoice(null); setAPIKey(''); setError(''); }}>{t('connections.backToChoices')}</button>
    <h3 className="runtime-connect-choice">{providerText(choice, 'name')}</h3>
    {choice.custom && <><label>{t('connections.startCommand')}<input disabled={busy} value={command} onChange={e => setCommand(e.target.value)} placeholder={t('connections.startCommandPlaceholder')}/></label></>}
    {kind === 'api-key' ? <>
     {magpie && <p className="settings-note">{t('connections.magpieThirdPartyNotice')}</p>}
     {magpie && !loading && !options.models.length && !gatewayManual && <div className="runtime-gateway-empty"><p>{t('connections.magpieNotFound')}</p><button onClick={() => setGatewayManual(true)}>{t('connections.enterGatewayAddress')}</button><button className="text-action" onClick={() => void open('https://usemagpie.ai')}>{t('connections.magpieGuide')}</button><button className="text-action" onClick={() => setDiscovery(n => n + 1)}>{t('runtime.recheck')}</button></div>}
     {gatewayFormVisible && <>
      {customProvider || addressOpen ? <div className="connection-field"><label htmlFor="connection-base-url">{t('connections.serviceUrl')}</label><div className="connection-field-control"><input id="connection-base-url" type="url" list={endpointListID} disabled={busy} value={baseUrl} onChange={e => { selectionTouched.current = false; setBaseUrl(e.target.value); setSelectedModels([]); setModel(''); }} placeholder="https://…/v1"/><datalist id={endpointListID}>{options.endpoints.map(endpoint => <option key={endpoint.value} value={endpoint.value}>{endpoint.name}</option>)}</datalist></div></div> : <button className="text-action connection-address-toggle" type="button" onClick={() => setAddressOpen(true)}>{t('connections.editServiceUrl')}</button>}
      {(providerStep === 'key' || customProvider || manualModel) && <div className="connection-field"><label htmlFor="connection-api-key">{t('connections.apiKey')}</label><div className="connection-field-control"><input id="connection-api-key" disabled={busy} type="password" autoComplete="off" spellCheck={false} value={apiKey} onChange={e => setAPIKey(e.target.value)} placeholder={reuseAuth ? t('connections.handledByCaelis') : t('connections.enterApiKey')}/>{reuseAuth && <small>{t('connections.reuseAuthHint')}</small>}</div></div>}
      {(customProvider || manualModel) && <><div className="connection-field"><label htmlFor="connection-model">{t('connections.model')}</label><div className="connection-field-control">{!loading && !options.models.length && <small>{t('connections.manualModelFallback')}</small>}<input id="connection-model" disabled={busy || loading} value={model} list={modelListID} onChange={e => setModel(e.target.value)} placeholder={t('connections.modelPlaceholder')}/><datalist id={modelListID}>{options.models.filter(m => m.chatEligibility !== 'non-chat').map(m => <option key={m.value} value={m.value}>{m.name}</option>)}</datalist></div></div>
       {manualChatRejected && <p className="settings-note" role="alert">{t('connections.nonChatModel')}</p>}
       {customCapabilitiesVisible && <details className="connection-capabilities" onToggle={e => setMetadata(e.currentTarget.open)}><summary>{t('connections.customCapabilities')}</summary><p className="settings-note">{t('connections.customCapabilitiesHint')}</p><div className="runtime-model-options-row"><label>{t('connections.contextWindowTokens')}<input type="number" min="1" step="1" value={contextWindow} onChange={e => setContextWindow(e.target.value)}/></label><label>{t('connections.maxOutputTokens')}<input type="number" min="1" step="1" value={maxOutput} onChange={e => setMaxOutput(e.target.value)}/></label></div><label>{t('connections.reasoningLevels')}<input value={reasoningLevels} onChange={e => setReasoningLevels(e.target.value)} placeholder={t('connections.reasoningLevelsPlaceholder')}/></label><label className="runtime-check"><input type="checkbox" checked={imageInput} onChange={e => setImageInput(e.target.checked)}/>{t('connections.imageInput')}</label></details>}</>}
      {!customProvider && providerStep === 'models' && !manualModel && <div className="connection-model-step"><div className="connection-model-heading"><strong>{t('connections.selectModels')}</strong><span>{t('connections.selectedModelCount', { count: catalogSelection.length })}</span></div>{loading ? <p role="status">{t('connections.loadingModels')}</p> : options.models.length ? <div className="connection-model-list">{options.models.map(item => item.chatEligibility === 'chat' ? <label key={item.value} className="connection-model-item"><input type="checkbox" checked={selectedModels.includes(item.value)} onChange={e => { selectionTouched.current = true; setSelectedModels(current => e.target.checked ? [...current, item.value] : current.filter(id => id !== item.value)); }}/><span>{item.name}</span></label> : <div key={item.value} className="connection-model-item connection-model-qualified"><span>{item.name}</span><small>{item.chatEligibility === 'non-chat' ? t('connections.nonChatModel') : t('connections.unknownChatModel')}</small>{item.chatEligibility === 'unknown' && <button type="button" onClick={() => { setManualModel(true); setModel(item.value); }}>{t('connections.configureUnknownModel')}</button>}</div>)}</div> : <p className="settings-note" role="status">{t('connections.noModelsFromHost')}</p>}{!loading && <><button type="button" className="text-action" onClick={() => setDiscovery(n => n + 1)}>{t('runtime.recheck')}</button>{!options.models.length && <button type="button" className="text-action" onClick={() => { setManualModel(true); setModel(''); }}>{t('connections.configureUnknownModel')}</button>}</>}</div>}
     </>}
    </> : null}
    <div className="setup-end"><button disabled={busy} onClick={close}>{t('common.cancel')}</button>{kind === 'api-key' && !customProvider && providerStep === 'models' && <button type="button" disabled={busy} onClick={() => { if (manualModel) { setManualModel(false); setModel(''); setMetadata(false); } else setProviderStep('key'); }}>{t('settings.setupBack')}</button>}{(kind !== 'api-key' || gatewayFormVisible) && <button className="primary" disabled={busy || loading || (choice.custom && !command.trim()) || (kind === 'api-key' && (!safeWebURL(baseUrl) || (providerStep === 'key' && !reuseAuth && !apiKey.trim()) || (customProvider || manualModel ? !model.trim() || !!manualChatRejected || !manualCapabilitiesValid : providerStep === 'models' && !catalogSelection.length)))} onClick={() => { if (kind === 'api-key' && !customProvider && providerStep === 'key') setProviderStep('models'); else void run(); }}>{busy ? t('connections.preparing') : kind === 'account' ? t('connections.continueLogin') : kind === 'agent' ? t('connections.checkAgent') : kind === 'api-key' && !customProvider && providerStep === 'key' ? t('connections.nextModels') : t('connections.connect')}</button>}</div>
   </>}
  </>}
  {flow?.stage === 'preparing' && <p className="settings-note" role="status">{t('connections.waitingLocalCaelis')}{batchCompleted > 0 && ` ${t('connections.connectedModelCount', { count: batchCompleted })}`}</p>}
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
   <div className="setup-end"><button className="primary" disabled={busy || !flow.methods?.some(m => m.id === method && m.available)} onClick={() => void run('authenticate', { method })}>{t('connections.continueAuth')}</button></div>
  </>}
  {flow?.stage === 'authorization' && flow.authorization && <>
   <div className="runtime-authorization"><p>{expired ? t('connections.authExpired') : t('connections.completeAuthInBrowser')}</p>{flow.authorization.userCode && <div className="runtime-device-code"><span>{t('connections.deviceCode')}</span><code>{flow.authorization.userCode}</code></div>}
    <button disabled={expired || !safeWebURL(flow.authorization.url)} onClick={() => void open(flow.authorization!.url)}>{t('connections.openLoginPage')}</button>
    <p className="runtime-url">{flow.authorization.url}</p>{flow.authorization.expiresAt && <small>{t('connections.validUntil', { time: date(new Date(flow.authorization.expiresAt), { hour: 'numeric', minute: '2-digit', second: '2-digit' }) })}</small>}
   </div>
   {flow.authorization.inputLabel && <form onSubmit={e => { e.preventDefault(); if (!expired && flow.authorization?.canSubmit) void run('submit-code', { code }); }}><label>{flow.authorization.inputLabel}<input type="password" autoComplete="off" spellCheck={false} value={code} disabled={busy || expired || !flow.authorization.canSubmit} onChange={e => setCode(e.target.value)}/></label><div className="setup-end"><button disabled={busy || expired || !code.trim() || !flow.authorization.canSubmit}>{t('connections.submitAuthInfo')}</button></div></form>}
   {!flow.authorization.inputLabel && <p className="settings-note" role="status">{t('connections.waitingRuntimeConfirmation')}</p>}
   <button className="text-action" disabled={busy} onClick={() => void run('refresh')}>{t('connections.checkAuthStatus')}</button>
  </>}
  {flow?.stage === 'models' && <><div className="connection-field"><label htmlFor="connection-account-model">{t('connections.useModel')}</label><div className="connection-field-control"><select id="connection-account-model" disabled={busy} value={model} onChange={e => setModel(e.target.value)}><option value="">{t('connections.selectModel')}</option>{flow.models?.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</select></div></div><div className="setup-end"><button className="primary" disabled={busy || !flow.models?.some(m => m.id === model)} onClick={() => void run('connect', { model })}>{t('connections.connectModel')}</button></div></>}
  {flow && (['failed', 'unknown'].includes(flow.stage) || flow.stage === 'complete' && (manualUnverified || batchTotal > batchCompleted) && !busy) && <>{batchTotal > 0 && <p role="status" className="settings-note">{t('connections.partialModels', { count: batchCompleted, total: batchTotal })}</p>}{flow.stage === 'complete' && manualUnverified && <p role="status" className="settings-note">{t('connections.unverifiedChatModel')}</p>}<div className="setup-end">{flow.stage === 'unknown' && flow.id && <button disabled={busy} onClick={() => void run('refresh')}>{t('connections.reconcileResult')}</button>}{flow.stage === 'failed' && <button disabled={busy} onClick={() => { setFlow(null); setAPIKey(''); setProviderStep('key'); setBatchCompleted(0); setBatchTotal(0); }}>{t('connections.editAndRetry')}</button>}<button disabled={busy} onClick={() => void finish()}>{t('connections.closeAndRefresh')}</button></div></>}
  {flow?.id && !['complete', 'failed', 'unknown'].includes(flow.stage) && <button className="text-action" disabled={canceling} onClick={() => void cancel()}>{canceling ? t('connections.canceling') : t('connections.cancelConnection')}</button>}
  {error && <p role="alert" className="inline-error">{error}</p>}
 </SettingsDialog>;
}
