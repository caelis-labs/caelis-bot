import { useEffect, useRef, useState, type ReactNode } from 'react';
import type { ConnectionGroup, ConnectionModel, ModelScope, RuntimeSettingsClient, RuntimeView } from './types';
import { messageOf } from './state';
import { runtimeSettingsClient } from './client';
import { ModelPicker, ModelSummary } from './ModelPicker';
import { SettingsDialog } from './SettingsDialog';
import { TeamSettings } from './TeamSettings';
import { ConnectionWizard } from './ConnectionWizard';
import './runtime.css';

export function RuntimeWorkspace({ client = runtimeSettingsClient, preparation }: { client?: RuntimeSettingsClient; preparation: (id: string, onBusy: (busy: boolean) => void) => ReactNode }) {
 const [view, setView] = useState<RuntimeView | null>(null), [tab, setTab] = useState('models');
 const [error, setError] = useState(''), [notice, setNotice] = useState(''), [loading, setLoading] = useState(true);
 const [editing, setEditing] = useState<ModelScope | null>(null), [connecting, setConnecting] = useState(false), [switching, setSwitching] = useState<string | null>(null);
 const [removing, setRemoving] = useState<{ group: ConnectionGroup; model: ConnectionModel } | null>(null), [removeError, setRemoveError] = useState(''), [busy, setBusy] = useState(false);
 const [preparationBusy, setPreparationBusy] = useState(false);
 const generation = useRef(0), working = useRef(false);
 const refresh = async (strict = false) => {
  const id = ++generation.current; setLoading(true); setError('');
  try { const next = await client.read(); if (id === generation.current) setView(next); }
  catch (e) { if (id === generation.current) setError(messageOf(e)); if(strict) throw e; }
  finally { if (id === generation.current) setLoading(false); }
 };
 useEffect(() => { void refresh(); return () => { generation.current++; }; }, [client]);
 // Navigation cannot silently discard an active settings/authentication dialog.
 useEffect(() => {
  const guard = (event: Event) => { if (editing || connecting || removing || switching !== null) event.preventDefault(); };
  window.addEventListener('settings-navigate', guard);
  return () => window.removeEventListener('settings-navigate', guard);
 }, [editing, connecting, removing, switching]);
 const remove = async () => {
  if (!removing || working.current) return;
  working.current = true; setBusy(true); setRemoveError('');
  try { await client.removeModel(removing.group, removing.model, view?.revision); setRemoving(null); setNotice('连接已移除。'); await refresh(); }
  catch { setRemoveError('未能确认移除结果。请关闭面板并刷新连接，核对后再操作。'); }
  finally { working.current = false; setBusy(false); }
 };
 const caelis = view?.profile.runtime === 'caelis', name = caelis ? 'Caelis' : 'Codex';
 const selection = editing === 'conversation' ? view?.conversation : editing === 'runtime' ? view?.main : view?.work;
 return <section className="runtime-workspace">
  <h1>运行时与模型</h1>
  {!view ? <div className="runtime-empty"><p role="status">{loading ? '正在读取运行时…' : '暂时无法读取运行时'}</p>{error && <p role="alert" className="inline-error">{error}</p>}<button disabled={loading} onClick={() => void refresh()}>重新读取</button></div> : <>
   <div className="runtime-active"><span className="runtime-monogram" aria-hidden="true">{caelis ? 'C' : '⌘'}</span><div><strong>{name}</strong><p>{view.setup.state === 'ready' ? '已连接' : view.setup.message || '需要完成设置'}{view.pending && ` · 待切换到 ${view.pending}`}</p></div><div className="runtime-active-actions"><button onClick={() => setSwitching(view.profile.runtime)}>管理</button><button className="text-action" onClick={() => setSwitching('')}>更换</button></div></div>
   {view.setup.state !== 'ready' && <p className="settings-note"><button className="text-action" onClick={() => setSwitching(view.profile.runtime)}>完成运行时设置 →</button></p>}
   <div className="runtime-workspace-tabs" aria-label="运行时设置内容">{[['models', caelis ? '模型与 Team' : '模型'], ['connections', '连接'], ...(caelis ? [['advanced', '高级']] : [])].map(([id, label]) => <button key={id} aria-pressed={tab === id} onClick={() => { setTab(id); setNotice(''); }}>{label}</button>)}</div>
   {tab === 'models' && <>
    <section className="runtime-model-section">
     <div className="runtime-setting-row"><strong>Bot 对话</strong>{view.conversation ? <ModelSummary value={view.conversation} models={view.models} label=" Bot 对话模型" disabled={loading} onClick={() => setEditing('conversation')}/> : <span className="settings-note">未连接</span>}</div>
     <div className="runtime-setting-row"><strong>{caelis ? '工作主模型' : '工作模型'}</strong>{(caelis ? view.main : view.work) ? <ModelSummary value={(caelis ? view.main : view.work)!} models={view.models} label={caelis ? ' Caelis 主模型' : '工作模型'} disabled={loading || caelis && !view.canEditMain} onClick={() => setEditing(caelis ? 'runtime' : 'work')}/> : <span className="settings-note">未连接</span>}</div>
     {caelis && !view.canEditMain && <p className="settings-note">请先更新 Caelis，再修改工作主模型。</p>}
     {caelis && !!view.work?.model && <p className="runtime-callout">新任务已指定独立模型。<button className="text-action" onClick={() => setEditing('work')}>更改</button></p>}
    </section>
    {caelis && <TeamSettings team={view.team} models={view.team.models} client={client} onRefresh={() => refresh(true)}/>}
   </>}
   {tab === 'connections' && <>
    <div className="runtime-section-title"><div><h2>已连接</h2></div><button onClick={() => caelis ? setConnecting(true) : setSwitching('codex')}>{caelis ? '添加连接' : '管理账号'}</button></div>
    {caelis ? view.connections.length ? view.connections.map(group => <details className="runtime-connection-group" key={group.id} open={view.connections.length < 4 || undefined}><summary><span><strong>{group.name}</strong><small>{group.kind === 'agent' ? '外部 Agent' : `${group.models.length} 个模型`}</small></span><span aria-hidden="true">⌄</span></summary>{group.models.map(model => <div className="runtime-connection-model" key={model.id}><div><strong>{model.name}</strong><p>{model.unavailable ? '需要重新认证' : model.uses.join(' · ') || '尚未使用'}</p></div>{group.kind === 'provider' && <span title={model.uses.length ? `正在用于 ${model.uses.join('、')}，请先更改对应配置` : undefined}><button disabled={!!model.uses.length} aria-label={`移除 ${model.name}`} onClick={() => { setRemoving({ group, model }); setRemoveError(''); }}>移除</button></span>}</div>)}{group.kind === 'agent' && <div className="setup-end"><button disabled={group.models.some(m => m.uses.length > 0)} onClick={() => { setRemoving({group,model:group.models[0]});setRemoveError(''); }}>断开 Agent</button></div>}</details>) : <div className="runtime-empty">还没有连接模型服务或外部 Agent。</div> : <p className="settings-note">{view.setup.accountType === 'chatgpt' ? '已使用 ChatGPT 账号连接 Codex。' : view.setup.accountType === 'apiKey' ? '已使用 OpenAI API Key 连接 Codex。' : '查看或更改 Codex 的登录方式。'}</p>}
   </>}
   {tab === 'advanced' && caelis && view.work && <div className="runtime-setting-row"><strong>独立工作模型</strong><ModelSummary value={view.work} models={view.models} label="独立工作模型" onClick={() => setEditing('work')}/></div>}
   <div className="runtime-page-footer"><span role="status">{loading ? '正在刷新…' : notice}</span><button className="text-action" disabled={loading} onClick={() => void refresh()}>刷新配置</button></div>
   {error && <p role="alert" className="inline-error">{error}</p>}
  </>}
  {editing && selection && view && <ModelPicker title={editing === 'conversation' ? 'Bot 对话模型' : editing === 'runtime' ? 'Caelis 主模型' : '工作模型'} description={editing === 'work' ? '用于之后创建的任务。' : undefined} value={selection} models={view.models} onReload={() => refresh(true)} inherited={editing !== 'runtime'} onSave={async value => { await client.saveModel(editing, value, view.revision); setNotice('模型设置已保存。'); await refresh(); }} onClose={() => setEditing(null)} onConnect={caelis ? () => { setEditing(null); setConnecting(true); } : undefined}/>}
  {connecting && <ConnectionWizard client={client} onClose={() => setConnecting(false)} onConnected={async () => { setConnecting(false); await refresh(); }}/ >}
  {removing && <SettingsDialog title={removing.group.kind === 'agent' ? `断开 ${removing.group.name}？` : `移除 ${removing.model.name}？`} busy={busy} onClose={() => setRemoving(null)}><p className="settings-note">{removing.group.kind === 'agent' ? `移除此 Agent 的全部 ${removing.group.models.length} 个模型连接。已安装的程序仍会保留。` : '其他应用将无法再通过此模型连接发送新请求。'}</p>{removeError && <p role="alert" className="inline-error">{removeError}</p>}<div className="setup-end"><button disabled={busy} onClick={() => { setRemoving(null); if (removeError) void refresh(); }}>{removeError ? '关闭并刷新' : '取消'}</button><button disabled={busy || !!removeError} onClick={() => void remove()}>{busy ? '正在移除…' : '移除'}</button></div></SettingsDialog>}
  {switching !== null && <SettingsDialog busy={preparationBusy} title={switching ? `管理 ${switching === 'caelis' ? 'Caelis' : 'Codex'}` : '更换运行时'} description={switching ? undefined : '各运行时保留独立的对话和设置，切换需要重新启动。'} onClose={() => { setSwitching(null); void refresh(); }}>{switching ? preparation(switching, setPreparationBusy) : <div className="runtime-choices">{['caelis', 'codex'].map(id => <button key={id} onClick={() => setSwitching(id)}><div><strong>{id === 'caelis' ? 'Caelis' : 'Codex'}</strong><span>{id === view?.profile.runtime ? '当前使用' : id === 'caelis' ? '连接模型服务，配置 Agent team' : '使用 Codex 账号和模型'}</span></div><span aria-hidden="true">→</span></button>)}</div>}</SettingsDialog>}
 </section>;
}
