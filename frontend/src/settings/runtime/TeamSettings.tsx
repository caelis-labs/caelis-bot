import { useRef, useState } from 'react';
import type { ModelOption } from '../../backend/contract';
import type { RuntimeSettingsClient, TeamChange, TeamRole, TeamState } from './types';
import { messageOf } from './state';
import { ModelPicker, ModelSummary } from './ModelPicker';
import { ConfigurationError } from './client';
import { SettingsDialog } from './SettingsDialog';

export function TeamSettings({ team, models, client, onRefresh }: { team: TeamState; models: ModelOption[]; client: RuntimeSettingsClient; onRefresh: () => Promise<void> }) {
 const [editing, setEditing] = useState<TeamRole | null>(null), [panel, setPanel] = useState(''), [name, setName] = useState(''), [description, setDescription] = useState('');
 const [error, setError] = useState(''), [busy, setBusy] = useState(false), [blocked,setBlocked] = useState(false), working = useRef(false);
 const open = (kind: string) => { setPanel(kind); setName(''); setDescription(''); setError(''); setBlocked(false); };
 const close = () => { setPanel(''); setEditing(null); setError(''); setBlocked(false); };
 const write = async (change: TeamChange) => {
  await client.changeTeam(change, team.revision);
  // A refresh failure must not turn an already committed write into a retry.
  close(); await onRefresh();
 };
 const submit = async (change: TeamChange) => {
  if (working.current || blocked) return;
  working.current = true; setBusy(true); setError('');
  try { await write(change); } catch (e) { setError(messageOf(e)); if (e instanceof ConfigurationError) setBlocked(e.unknown || e.receipt.outcome === 'conflicted'); } finally { working.current = false; setBusy(false); }
 };
 const row = (role: TeamRole) => <div className="runtime-setting-row" key={role.id}><div><strong>{role.id}</strong><p>{role.description}</p>{!role.modelIds && <p className="settings-note">请更新并重启 Caelis，以读取此角色的可用模型。</p>}{role.problem && <p className="inline-error">{role.problem}</p>}</div><div className="runtime-row-actions"><ModelSummary unbound value={role.selection} models={models} label={` ${role.id}`} onClick={() => setEditing(role)}/>{role.custom && <button className="text-action" onClick={() => { setEditing(role); open('delete-role'); }}>删除</button>}</div></div>;
 return <section className="runtime-team">
  <div className="runtime-section-title"><h2>Agent team</h2>{team.available && <div><button className="text-action" onClick={() => open('sets')}>{team.activeSet || 'Team 方案'}</button><button aria-label="添加 Agent" onClick={() => open('create')}>添加</button></div>}</div>
  {!team.available ? <p className="settings-note">{team.reason}</p> : <>
   {team.roles.filter(r => r.id !== 'self' && !r.system).map(row)}
   <p className="runtime-self"><span>self</span>跟随工作会话的主模型、推理强度与速度</p>
   <details className="runtime-system-agents"><summary>系统 Agent</summary><p className="settings-note">按需调整工具检索、审批与记忆维护使用的模型。</p>{team.roles.filter(r => r.system && r.id !== 'self').map(row)}</details>
  </>}
  {editing && !panel && <ModelPicker requireEffort title={`配置 ${editing.id}`} description="与本机 Caelis 共用，按运行时规则生效。" value={editing.selection} models={models.filter(m => editing.modelIds?.includes(m.model))} onSave={selection => write({ action: 'bind', id: editing.id, selection })} onClose={() => setEditing(null)} onReload={onRefresh} onReset={!editing.inherited ? () => write({ action: 'reset', id: editing.id }) : undefined}/>}
  {panel && <SettingsDialog title={panel === 'create' ? '添加 Agent' : panel === 'save-set' ? '保存 Team 方案' : panel === 'delete-role' ? `删除 ${editing?.id}？` : 'Team 方案'} description={panel === 'sets' ? '方案保存角色的模型绑定，不包含 Bot 对话模型与工作主模型。' : undefined} busy={busy} onClose={close}>
   {panel === 'sets' ? <><div className="runtime-scheme-list">{team.sets.map(set => <div key={set.name}><div><strong>{set.name}</strong>{set.problem && <p className="inline-error">{set.problem}</p>}</div><button disabled={busy || blocked || !set.available} onClick={() => void submit({ action: 'apply-set', name: set.name })}>应用</button><button disabled={busy} onClick={() => { setName(set.name); setPanel('delete-set'); }}>删除</button></div>)}</div><button className="text-action" disabled={busy} onClick={() => open('save-set')}>将当前 Team 存为方案</button></> : panel === 'delete-role' || panel === 'delete-set' ? <><p>{panel === 'delete-role' ? '移除此自定义角色和绑定，不中断已有工作。' : `删除方案「${name}」，保留当前 Team 绑定。`}</p><div className="setup-end"><button disabled={busy} onClick={close}>取消</button><button disabled={busy || blocked} onClick={() => void submit(panel === 'delete-role' ? { action: 'delete-role', id: editing!.id } : { action: 'delete-set', name })}>删除</button></div></> : <form onSubmit={e => { e.preventDefault(); void submit(panel === 'create' ? { action: 'create-role', id: name.trim(), description: description.trim() } : { action: 'save-set', name: name.trim() }); }}>
    <label>{panel === 'create' ? '角色名' : '方案名称'}<input required pattern={panel === 'create' ? '[a-z][a-z0-9-]*' : undefined} value={name} disabled={busy} onChange={e => setName(e.target.value)} placeholder={panel === 'create' ? '例如：research' : '例如：日常开发'}/></label>
    {panel === 'create' && <label>职责<textarea required value={description} disabled={busy} onChange={e => setDescription(e.target.value)} placeholder="例如：查找资料并核对来源"/></label>}
    <div className="setup-end"><button type="button" disabled={busy} onClick={close}>取消</button><button className="primary" disabled={busy || blocked || !name.trim() || panel === 'create' && !description.trim()}>{busy ? '正在保存…' : '保存'}</button></div>
   </form>}
   {error && <div role="alert" className="inline-error"><p>{error}</p><button disabled={busy} onClick={() => { close(); void onRefresh().catch(()=>{}); }}>读取最新配置</button></div>}
  </SettingsDialog>}
 </section>;
}
