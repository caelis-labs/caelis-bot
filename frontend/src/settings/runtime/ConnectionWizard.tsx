import { useEffect, useId, useRef, useState } from 'react';
import type { APIKeyOptions, ConnectAction, ConnectChoice, ConnectionCatalog, ConnectionFlow, ConnectionKind, FlowInput, RuntimeSettingsClient } from './types';
import { acceptConnectionProgress, messageOf, safeWebURL } from './state';
import { SettingsDialog } from './SettingsDialog';

const emptyCatalog: ConnectionCatalog = { choices: [], unavailable: '' };
export function ConnectionWizard({ client, onClose, onConnected }: { client: RuntimeSettingsClient; onClose: () => void; onConnected: () => Promise<void> }) {
 const [kind, setKind] = useState<ConnectionKind>('account'), [catalog, setCatalog] = useState(emptyCatalog), [choice, setChoice] = useState<ConnectChoice | null>(null);
 const [options, setOptions] = useState<APIKeyOptions>({ endpoints: [], models: [] });
 const [metadata,setMetadata] = useState(false), [contextWindow,setContextWindow] = useState(''), [maxOutput,setMaxOutput] = useState(''), [imageInput,setImageInput] = useState(false), [reasoningLevels,setReasoningLevels] = useState('');
 const [baseUrl, setBaseUrl] = useState(''), [model, setModel] = useState(''), [apiKey, setAPIKey] = useState(''), [command, setCommand] = useState('');
 const [flow, setFlow] = useState<ConnectionFlow | null>(null), [code, setCode] = useState(''), [method, setMethod] = useState(''), [destination, setDestination] = useState(''), [manual, setManual] = useState(false);
 const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false), [error, setError] = useState(''), [canceling, setCanceling] = useState(false), [now, setNow] = useState(Date.now());
 const alive = useRef(true), working = useRef(false), serial = useRef(0), abort = useRef<AbortController | null>(null), optionsSerial = useRef(0), modelListID = useId(), endpointListID = useId();
 useEffect(() => { const clear = () => {setAPIKey('');setCode('');};window.addEventListener('settings-close',clear);return () => window.removeEventListener('settings-close',clear); },[]);
 useEffect(() => { alive.current = true; return () => { alive.current = false; serial.current++; abort.current?.abort(); }; }, []);
 useEffect(() => {
  let valid = true; setLoading(true); setCatalog(emptyCatalog); setChoice(null); setFlow(null); setAPIKey(''); setError('');
  void client.catalog(kind).then(value => { if (valid) setCatalog(value); }).catch(e => { if (valid) setError(messageOf(e)); }).finally(() => { if (valid) setLoading(false); });
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
  }).catch(e => { if (alive.current && seq === optionsSerial.current) setError(messageOf(e)); }).finally(() => { if (alive.current && seq === optionsSerial.current) setLoading(false); });
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
  if (working.current || !choice) return;
  abort.current?.abort();
  const seq = ++serial.current, controller = new AbortController(); abort.current = controller;
  working.current = true; setBusy(true); setError('');
  // Secret drafts are only write arguments. Never put them in a flow snapshot,
  // URL, local storage, logs, operation key or a retry closure.
  const secret = apiKey; setAPIKey(''); setCode('');
  try {
   const next = flow && action ? await client.advanceConnection(flow, action, input, controller.signal, value => observe(seq, value)) :
    await client.startConnection({ kind, choice: choice.id, ...(choice.custom ? { command } : {}), ...(kind === 'api-key' ? { baseUrl, model, apiKey: secret, ...(metadata ? {contextWindowTokens:Number(contextWindow),maxOutputTokens:Number(maxOutput),imageInput,reasoningLevels:reasoningLevels.split(',').map(s=>s.trim()).filter(Boolean)} : {}) } : {}) }, controller.signal, value => observe(seq, value));
   observe(seq, next);
  } catch {
   if (alive.current && seq === serial.current && !controller.signal.aborted) {
    setFlow(current => current?.stage === 'complete' ? current : ({ id: current?.id || '', revision: current?.revision || '', sequence: current?.sequence || 0, stage: 'unknown', title: '操作结果尚未确认', message: '请检查当前连接状态，不要重复提交安装或认证。' }));
   }
  } finally { if (alive.current && seq === serial.current) { working.current = false; setBusy(false); } }
 };
 const finish = async () => { onClose(); await onConnected(); };
 const cancel = async () => {
  if (canceling) return;
  if (!flow?.id || ['complete', 'failed', 'unknown'].includes(flow.stage)) { await finish(); return; }
  setCanceling(true); setError('');
  try {
   await client.cancelConnection(flow);
   serial.current++; abort.current?.abort(); setCode(''); onClose();
  } catch { if (alive.current) setError('取消结果未确认，请检查 Caelis 中的认证状态。'); }
  finally { if (alive.current) setCanceling(false); }
 };
 const open = async (url: string) => { if (!safeWebURL(url)) { setError('运行时提供的链接无效，请重新检查连接。'); return; } try { await client.openURL(url); } catch { setError('无法打开浏览器，请复制链接后自行打开。'); } };
 const close = () => { setAPIKey(''); setCode(''); if (!flow) onClose(); else void cancel(); };
 return <SettingsDialog title={flow?.title || '添加连接'} description={flow?.message || '模型服务与外部 Agent 均由本机 Caelis 管理。'} busy={busy || canceling} onClose={close}>
  {!flow && <>
   <div className="runtime-segments" aria-label="连接类型"><button disabled={busy} aria-pressed={kind !== 'agent'} onClick={() => setKind('account')}>模型服务</button><button disabled={busy} aria-pressed={kind === 'agent'} onClick={() => setKind('agent')}>外部 Agent</button></div>
   {kind !== 'agent' && <div className="runtime-auth-tabs"><button disabled={busy} aria-pressed={kind === 'account'} onClick={() => setKind('account')}>账号登录</button><button disabled={busy} aria-pressed={kind === 'api-key'} onClick={() => setKind('api-key')}>API Key</button></div>}
   {loading && !choice && <p role="status">正在读取可连接项目…</p>}
   {catalog.unavailable && <p className="settings-note" role="status">{catalog.unavailable}</p>}
   {!loading && !catalog.unavailable && !catalog.choices.length && <p className="settings-note">当前没有可连接项目。</p>}
   {!choice ? <div className="runtime-connect-catalog">{catalog.choices.map(c => <button key={c.id} disabled={busy} onClick={() => select(c)}><span><strong>{c.name}</strong><small>{c.description}</small></span><span aria-hidden="true">›</span></button>)}</div> : <>
    <button className="text-action" disabled={busy} onClick={() => { optionsSerial.current++; setChoice(null); setAPIKey(''); setError(''); }}>返回选择</button>
    <h3 className="runtime-connect-choice">{choice.name}</h3>
    {choice.custom && <><label>启动命令<input disabled={busy} value={command} onChange={e => setCommand(e.target.value)} placeholder="已安装的 ACP 程序及参数"/></label></>}
    {kind === 'api-key' ? <>
     <label>服务地址<input type="url" list={endpointListID} disabled={busy} value={baseUrl} onChange={e => { setBaseUrl(e.target.value); setAPIKey(''); setModel(''); }} placeholder="https://…/v1"/><datalist id={endpointListID}>{options.endpoints.map(endpoint => <option key={endpoint.value} value={endpoint.value}>{endpoint.name}</option>)}</datalist></label>
     <label>模型<input disabled={busy || loading} value={model} list={modelListID} onChange={e => setModel(e.target.value)} placeholder="选择或输入模型名称"/><datalist id={modelListID}>{options.models.map(m => <option key={m.value} value={m.value}>{m.name}</option>)}</datalist></label>
     <label>API Key{reuseAuth && <small>可留空，由 Caelis 处理此地址的认证</small>}<input disabled={busy} type="password" autoComplete="off" spellCheck={false} value={apiKey} onChange={e => setAPIKey(e.target.value)} placeholder={reuseAuth ? '由 Caelis 处理' : '输入 API Key'}/></label>
     <details onToggle={e => setMetadata(e.currentTarget.open)}><summary>自定义模型能力</summary><p className="settings-note">目录中未收录的模型可在此补充，数值以服务方提供的信息为准。</p><div className="runtime-model-options-row"><label>上下文上限<input type="number" min="1" step="1" value={contextWindow} onChange={e => setContextWindow(e.target.value)}/></label><label>输出上限<input type="number" min="1" step="1" value={maxOutput} onChange={e => setMaxOutput(e.target.value)}/></label></div><label>推理等级<input value={reasoningLevels} onChange={e => setReasoningLevels(e.target.value)} placeholder="逗号分隔，例如 low, medium, high"/></label><label className="runtime-check"><input type="checkbox" checked={imageInput} onChange={e => setImageInput(e.target.checked)}/>支持图片输入</label></details>
    </> : <p className="settings-note">{kind === 'account' ? '先选择账号模型，再由 Caelis 发起浏览器授权。' : choice.custom ? '先检查本机程序，再完成它声明的认证并选择模型。' : '使用 Caelis 内置的连接方式，先检查本机安装与所需认证。'}</p>}
    <div className="setup-end"><button disabled={busy} onClick={close}>取消</button><button className="primary" disabled={busy || loading || (choice.custom && !command.trim()) || (kind === 'api-key' && (!model.trim() || !safeWebURL(baseUrl) || (!reuseAuth && !apiKey.trim())))} onClick={() => void run()}>{busy ? '正在准备…' : kind === 'account' ? '继续登录' : kind === 'agent' ? '检查 Agent' : '连接'}</button></div>
   </>}
  </>}
  {flow?.stage === 'preparing' && <p className="settings-note" role="status">正在等待本机 Caelis…</p>}
  {flow?.stage === 'launcher' && <><div className="runtime-connect-catalog">{flow.launchers?.map(launcher => <button disabled={busy} key={launcher.id} onClick={() => void run('choose-launcher',{launcher:launcher.id})}><span><strong>{launcher.name || launcher.id}</strong><small>{launcher.description}</small></span><span aria-hidden="true">›</span></button>)}</div></>}
  {flow?.stage === 'installation' && flow.installation && <>
   <div className="runtime-install-details"><strong>{flow.installation.platform}</strong>{flow.installation.source && <><p>安装来源</p><button className="text-action runtime-url" onClick={() => void open(flow.installation!.source)}>{flow.installation.source}</button></>}</div>
   <label>安装目录<input value={destination} disabled={busy} onChange={e => setDestination(e.target.value)}/></label>
   <p className="settings-note">{flow.installation.canInstall ? '确认后才下载完整运行时；已有目录中的文件不会被覆盖。' : '请按手动安装说明准备完整运行时，再检查安装。'}</p>
   <button className="text-action" disabled={busy} onClick={() => setManual(!manual)}>{manual ? '收起手动安装说明' : '手动安装'}</button>
   {(manual || !flow.installation.canInstall) && <pre className="runtime-install-instructions">{flow.installation.instructions}</pre>}
   <div className="setup-end"><button disabled={busy} onClick={() => void run('check-installation', { destination })}>检查安装</button>{!manual && <button className="primary" disabled={busy || !destination.trim() || !flow.installation.canInstall} onClick={() => void run('install', { destination })}>{busy ? '正在安装…' : '安装并继续'}</button>}</div>
  </>}
  {flow?.stage === 'auth-method' && <>
   <fieldset disabled={busy} className="runtime-auth-methods"><legend>选择认证方式</legend>{flow.methods?.map(m => <label key={m.id}><input type="radio" name="runtime-auth-method" value={m.id} checked={method === m.id} disabled={!m.available} onChange={() => setMethod(m.id)}/><span><strong>{m.name}</strong><small>{m.reason || m.description}</small></span></label>)}</fieldset>
   <div className="setup-end"><button className="primary" disabled={busy || !flow.methods?.some(m => m.id === method && m.available)} onClick={() => void run('authenticate', { method })}>继续认证</button></div>
  </>}
  {flow?.stage === 'authorization' && flow.authorization && <>
   <div className="runtime-authorization"><p>{expired ? '授权已过期，请检查结果后重新连接。' : '请在浏览器中完成授权，返回后继续。'}</p>{flow.authorization.userCode && <div className="runtime-device-code"><span>设备代码</span><code>{flow.authorization.userCode}</code></div>}
    <button disabled={expired || !safeWebURL(flow.authorization.url)} onClick={() => void open(flow.authorization!.url)}>打开登录页面</button>
    <p className="runtime-url">{flow.authorization.url}</p>{flow.authorization.expiresAt && <small>有效期至 {new Date(flow.authorization.expiresAt).toLocaleTimeString()}</small>}
   </div>
   {flow.authorization.inputLabel && <form onSubmit={e => { e.preventDefault(); if (!expired && flow.authorization?.canSubmit) void run('submit-code', { code }); }}><label>{flow.authorization.inputLabel}<input type="password" autoComplete="off" spellCheck={false} value={code} disabled={busy || expired || !flow.authorization.canSubmit} onChange={e => setCode(e.target.value)}/></label><div className="setup-end"><button disabled={busy || expired || !code.trim() || !flow.authorization.canSubmit}>提交授权信息</button></div></form>}
   {!flow.authorization.inputLabel && <p className="settings-note" role="status">正在等待运行时确认…</p>}
   <button className="text-action" disabled={busy} onClick={() => void run('refresh')}>检查授权状态</button>
  </>}
  {flow?.stage === 'models' && <><label>使用模型<select disabled={busy} value={model} onChange={e => setModel(e.target.value)}><option value="">选择模型</option>{flow.models?.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</select></label><p className="settings-note">连接后可将此模型用于主模型或 Agent team。</p><div className="setup-end"><button className="primary" disabled={busy || !flow.models?.some(m => m.id === model)} onClick={() => void run('connect', { model })}>连接模型</button></div></>}
  {flow && ['complete', 'failed', 'unknown'].includes(flow.stage) && <div className="setup-end">{flow.stage === 'unknown' && flow.id && <button disabled={busy} onClick={() => void run('refresh')}>核对操作结果</button>}<button className={flow.stage === 'complete' ? 'primary' : ''} disabled={busy} onClick={() => void finish()}>{flow.stage === 'complete' ? '完成' : '关闭并刷新'}</button></div>}
  {flow?.id && !['complete', 'failed', 'unknown'].includes(flow.stage) && <button className="text-action" disabled={canceling} onClick={() => void cancel()}>{canceling ? '正在取消…' : '取消连接'}</button>}
  {error && <p role="alert" className="inline-error">{error}</p>}
 </SettingsDialog>;
}
