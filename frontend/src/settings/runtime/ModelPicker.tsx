import { useId, useRef, useState } from 'react';
import type { ModelOption } from '../../backend/contract';
import type { ModelSelection } from './types';
import { chooseModel, effortName, inheritedSelection, messageOf, selectionSummary, tierName, validSelection } from './state';
import { ConfigurationError } from './client';
import { SettingsDialog } from './SettingsDialog';

export function ModelSummary({ value, models, disabled, onClick, label, unbound = false }: { value: ModelSelection; models: ModelOption[]; disabled?: boolean; onClick: () => void; label: string; unbound?: boolean }) {
 const summary = unbound && !value.model ? {name:'未绑定',detail:''} : selectionSummary(value, models);
 return <button className="runtime-model-summary" aria-label={`配置${label}`} disabled={disabled} onClick={onClick}><span><strong>{summary.name}</strong>{summary.detail && <small>{summary.detail}</small>}</span><span aria-hidden="true">›</span></button>;
}

export function ModelPicker({ title, description, value, models, inherited = false, requireEffort = false, onSave, onClose, onConnect, onReset, onReload }: {
 title: string; description?: string; value: ModelSelection; models: ModelOption[]; inherited?: boolean; requireEffort?: boolean;
 onSave: (value: ModelSelection) => Promise<void>; onClose: () => void; onConnect?: () => void; onReset?: () => Promise<void>; onReload?: () => Promise<void>;
}) {
 const [draft, setDraft] = useState(value), [query, setQuery] = useState(''), [error, setError] = useState(''), [busy, setBusy] = useState(false), [blocked, setBlocked] = useState(false);
 const working = useRef(false), group = useId();
 const model = models.find(m => m.model === draft.model);
 const filtered = models.filter(m => `${m.name} ${m.model} ${m.description}`.toLocaleLowerCase().includes(query.toLocaleLowerCase()));
 const save = async (reset = false) => {
  if (blocked || working.current || !reset && (!validSelection(draft, models, inherited) || requireEffort && !draft.effort)) return;
  working.current = true; setBusy(true); setError('');
  try { if (reset) await onReset?.(); else await onSave(draft); onClose(); } catch (e) { setError(messageOf(e)); if(e instanceof ConfigurationError) setBlocked(e.unknown || e.receipt.outcome === 'conflicted'); } finally { working.current = false; setBusy(false); }
 };
 return <SettingsDialog title={title} description={description} busy={busy} onClose={onClose}>
  <input className="runtime-model-search" aria-label="搜索模型" placeholder="搜索已连接的模型" value={query} onChange={e => setQuery(e.target.value)} disabled={busy}/>
  <fieldset disabled={busy} className="runtime-model-options"><legend className="visually-hidden">可用模型</legend>
   {inherited && !query && <label className="runtime-model-option"><input type="radio" name={group} checked={!draft.model} onChange={() => setDraft(inheritedSelection)}/><span><strong>默认</strong></span></label>}
   {filtered.map(m => <label className="runtime-model-option" key={m.model}><input type="radio" name={group} checked={draft.model === m.model} onChange={() => { setDraft(chooseModel(m)); setError(''); }}/><span><strong>{m.name || m.model}</strong><small>{m.description || m.model}</small></span></label>)}
   {!filtered.length && <p className="settings-note">没有找到可用模型</p>}
  </fieldset>
  {onConnect && <button className="text-action" disabled={busy} onClick={onConnect}>连接其他模型</button>}
  {draft.model && !model && <p role="alert" className="inline-error">当前模型已不可用，请重新选择。</p>}
  {model && <div className="runtime-model-options-row">
   <label>推理强度<select aria-label="推理强度" disabled={busy} value={draft.effort} onChange={e => setDraft({ ...draft, effort: e.target.value })}>
    {!requireEffort && <option value="">由模型决定</option>}{draft.effort && !model.efforts.includes(draft.effort) && <option value={draft.effort}>{draft.effort} · 当前不可用</option>}{model.efforts.filter(Boolean).map(e => <option key={e} value={e}>{effortName[e] || e}</option>)}
   </select></label>
   <label>响应速度<select aria-label="响应速度" disabled={busy} value={draft.serviceTier} onChange={e => setDraft({ ...draft, serviceTier: e.target.value })}>
    <option value="">由运行时决定</option>{draft.serviceTier && !model.serviceTiers.some(t => t.id === draft.serviceTier) && <option value={draft.serviceTier}>{draft.serviceTier} · 当前不可用</option>}{model.serviceTiers.filter(t => t.id).map(t => <option key={t.id} value={t.id}>{tierName(t.id, t.name)}</option>)}
   </select></label>
  </div>}
  {onReset && <button disabled={busy || blocked} className="text-action" onClick={() => void save(true)}>恢复角色默认绑定</button>}
  {error && <p role="alert" className="inline-error">{error}{onReload && <button disabled={busy} className="text-action" onClick={() => { void onReload().then(() => { setError(''); setBlocked(false); }).catch(e=>setError(messageOf(e)));  }}>读取最新配置，保留当前选择</button>}</p>}
  <div className="setup-end"><button disabled={busy} onClick={onClose}>取消</button><button className="primary" disabled={busy || blocked || !validSelection(draft, models, inherited) || requireEffort && !draft.effort} onClick={() => void save()}>{busy ? '正在保存…' : '保存'}</button></div>
 </SettingsDialog>;
}
