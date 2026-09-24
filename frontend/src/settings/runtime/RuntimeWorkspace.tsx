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
  <h1>运行时与模型</h1><p className="settings-intro">选择 Bot 如何与你对话，以及如何完成工作。</p>
  {!view ? <div className="runtime-empty"><p role="status">{loading ? '正在读取运行时…' : '暂时无法读取运行时'}</p>{error && <p role="alert" className="inline-error">{error}</p>}<button disabled={loading} onClick={() => void refresh()}>重新读取</button></div> : <>
   <div className="runtime-active"><span className="runtime-monogram" aria-hidden="true">{caelis ? 'C' : '⌘'}</span><div><strong>{name} Runtime</strong><p>{view.setup.state === 'ready' ? '已连接 · 本机运行' : view.setup.message || '需要完成设置'}{view.pending && ` · 待切换到 ${view.pending}`}</p></div><button onClick={() => setSwitching('')}>更换…</button></div>
   {view.setup.state !== 'ready' && <p className="settings-note"><button className="text-action" onClick={() => setSwitching(view.profile.runtime)}>完成运行时设置 →</button></p>}
   <div className="runtime-workspace-tabs" aria-label="运行时设置内容">{[['models', caelis ? '模型与 Team' : '模型'], ['connections', '连接'], ['advanced', '高级']].map(([id, label]) => <button key={id} aria-pressed={tab === id} onClick={() => { setTab(id); setNotice(''); }}>{label}</button>)}</div>
   {tab === 'models' && <>
    <section className="runtime-model-section"><h2>Bot 对话</h2><div className="runtime-setting-row"><div><strong>对话模型</strong><p>用于日常对话，保留 Bot 的独立配置。</p></div>{view.conversation ? <ModelSummary value={view.conversation} models={view.models} label=" Bot 对话模型" disabled={loading} onClick={() => setEditing('conversation')}/> : <span className="settings-note">连接后可配置</span>}</div></section>
    {caelis ? <>
     <section className="runtime-model-section"><div className="runtime-section-title"><h2>Caelis 工作</h2><span className="runtime-shared-label">与本机 Caelis 共用</span></div>
      <div className="runtime-setting-row"><div><strong>主模型</strong><p>和打开 Caelis 终端发送任务使用相同配置。</p></div>{view.main ? <ModelSummary value={view.main} models={view.models} label=" Caelis 主模型" disabled={!view.canEditMain || loading} onClick={() => setEditing('runtime')}/> : <span className="settings-note">尚未读取到主模型</span>}</div>
      {!view.canEditMain && <p className="settings-note">当前连接仅能读取默认模型；推理强度、速度和主模型更改请使用 Caelis /model。</p>}
      {!!view.work?.model && <p className="runtime-callout">Bot 目前为新任务单独指定了模型。<button className="text-action" onClick={() => setEditing('work')}>更改或沿用运行时</button></p>}
     </section>
     <TeamSettings team={view.team} models={view.team.models} client={client} onRefresh={() => refresh(true)}/>
    </> : <section className="runtime-model-section"><h2>Codex 工作</h2><div className="runtime-setting-row"><div><strong>工作模型</strong><p>用于新建任务，已有任务保留原设置。</p></div>{view.work && <ModelSummary value={view.work} models={view.models} label="工作模型" disabled={loading} onClick={() => setEditing('work')}/>}</div></section>}
   </>}
   {tab === 'connections' && <>
    <div className="runtime-section-title"><div><h2>已连接</h2><p className="settings-note">认证和连接信息由本机 {name} 管理。</p></div><button onClick={() => caelis ? setConnecting(true) : setSwitching('codex')}>{caelis ? '添加连接' : '管理账号'}</button></div>
    {caelis ? view.connections.length ? view.connections.map(group => <details className="runtime-connection-group" key={group.id} open={view.connections.length < 4 || undefined}><summary><span><strong>{group.name}</strong><small>{group.kind === 'agent' ? '外部 Agent' : `${group.models.length} 个模型`}</small></span><span aria-hidden="true">⌄</span></summary><p className="settings-note">{group.detail}</p>{group.models.map(model => <div className="runtime-connection-model" key={model.id}><div><strong>{model.name}</strong><p>{model.unavailable ? '需要重新认证' : model.uses.join(' · ') || '尚未使用'}</p></div>{group.kind === 'provider' && <span title={model.uses.length ? `正在用于 ${model.uses.join('、')}，请先更改对应配置` : undefined}><button disabled={!!model.uses.length} aria-label={`移除 ${model.name}`} onClick={() => { setRemoving({ group, model }); setRemoveError(''); }}>移除</button></span>}</div>)}{group.kind === 'agent' && <div className="setup-end"><button disabled={group.models.some(m => m.uses.length > 0)} onClick={() => { setRemoving({group,model:group.models[0]});setRemoveError(''); }}>断开 Agent</button></div>}</details>) : <div className="runtime-empty">还没有连接模型服务或外部 Agent。</div> : <p className="settings-note">{view.setup.accountType === 'chatgpt' ? '已使用 ChatGPT 账号连接 Codex。' : view.setup.accountType === 'apiKey' ? '已使用 OpenAI API Key 连接 Codex。' : '查看或更改 Codex 的登录方式。'}</p>}
   </>}
   {tab === 'advanced' && <><div className="runtime-section-title"><div><h2>本机运行时</h2><p className="settings-note">程序位置、连接检测与更新。</p></div><button onClick={() => setSwitching(view.profile.runtime)}>管理…</button></div><dl className="runtime-installation"><dt>版本</dt><dd>{view.setup.installation.version || '尚未检测'}</dd><dt>程序</dt><dd>{view.setup.installation.path || '自动发现'}</dd></dl>{caelis && view.work && <div className="runtime-setting-row"><div><strong>独立工作模型</strong><p>仅影响 Bot 新建的任务。</p></div><ModelSummary value={view.work} models={view.models} label="独立工作模型" onClick={() => setEditing('work')}/></div>}</>}
   <div className="runtime-page-footer"><span role="status">{loading ? '正在刷新…' : notice}</span><button className="text-action" disabled={loading} onClick={() => void refresh()}>刷新配置</button></div>
   {error && <p role="alert" className="inline-error">{error}</p>}
  </>}
  {editing && selection && view && <ModelPicker title={editing === 'conversation' ? 'Bot 对话模型' : editing === 'runtime' ? 'Caelis 主模型' : '工作模型'} description={editing === 'conversation' ? '仅修改 Bot 对话，不改变工作主模型或 Team。' : editing === 'runtime' ? '修改本机 Caelis 的共享配置，按运行时规则生效。' : '用于新建任务，已有工作不受影响。'} value={selection} models={view.models} onReload={() => refresh(true)} inherited={editing !== 'runtime'} onSave={async value => { await client.saveModel(editing, value, view.revision); setNotice('模型设置已保存。'); await refresh(); }} onClose={() => setEditing(null)} onConnect={caelis ? () => { setEditing(null); setConnecting(true); } : undefined}/>}
  {connecting && <ConnectionWizard client={client} onClose={() => setConnecting(false)} onConnected={async () => { setConnecting(false); await refresh(); }}/ >}
  {removing && <SettingsDialog title={removing.group.kind === 'agent' ? `断开 ${removing.group.name}？` : `移除 ${removing.model.name}？`} description="这会更改本机 Caelis 的共享连接配置。" busy={busy} onClose={() => setRemoving(null)}><p className="settings-note">{removing.group.kind === 'agent' ? `移除此 Agent 的全部 ${removing.group.models.length} 个模型连接。已安装的程序仍会保留。` : '其他应用将无法再通过此模型连接发送新请求。'}</p>{removeError && <p role="alert" className="inline-error">{removeError}</p>}<div className="setup-end"><button disabled={busy} onClick={() => { setRemoving(null); if (removeError) void refresh(); }}>{removeError ? '关闭并刷新' : '取消'}</button><button disabled={busy || !!removeError} onClick={() => void remove()}>{busy ? '正在移除…' : '移除'}</button></div></SettingsDialog>}
  {switching !== null && <SettingsDialog busy={preparationBusy} title={switching ? `管理 ${switching === 'caelis' ? 'Caelis' : 'Codex'}` : '更换运行时'} description={switching ? undefined : '各运行时保留独立的对话和设置，切换需要重新启动。'} onClose={() => { setSwitching(null); void refresh(); }}>{switching ? preparation(switching, setPreparationBusy) : <div className="runtime-choices">{['caelis', 'codex'].map(id => <button key={id} onClick={() => setSwitching(id)}><div><strong>{id === 'caelis' ? 'Caelis' : 'Codex'}</strong><span>{id === view?.profile.runtime ? '当前使用' : id === 'caelis' ? '连接模型服务，配置 Agent team' : '使用 Codex 账号和模型'}</span></div><span aria-hidden="true">→</span></button>)}</div>}</SettingsDialog>}
 </section>;
}
