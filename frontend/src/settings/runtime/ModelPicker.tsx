import { useId, useRef, useState } from 'react';
import type { ModelOption } from '../../backend/contract';
import type { ModelSelection } from './types';
import { chooseModel, getEffortName, inheritedSelection, messageOf, selectionSummary, tierName, validSelection } from './state';
import { ConfigurationError } from './client';
import { SettingsDialog } from './SettingsDialog';
import { useI18n } from '../../i18n';

export function ModelSummary({ value, models, disabled, onClick, label, unbound = false }: { value: ModelSelection; models: ModelOption[]; disabled?: boolean; onClick: () => void; label: string; unbound?: boolean }) {
 const { t } = useI18n();
 const summary = unbound && !value.model ? { name: t('runtime.unbound'), detail: '' } : selectionSummary(value, models, t);
 return <button className="runtime-model-summary" aria-label={t('runtime.configureModel', { label })} disabled={disabled} onClick={onClick}><span><strong>{summary.name}</strong>{summary.detail && <small>{summary.detail}</small>}</span><span aria-hidden="true">›</span></button>;
}

export function ModelPicker({ title, description, value, models, inherited = false, requireEffort = false, onSave, onClose, onConnect, onReset, onReload }: {
 title: string; description?: string; value: ModelSelection; models: ModelOption[]; inherited?: boolean; requireEffort?: boolean;
 onSave: (value: ModelSelection) => Promise<void>; onClose: () => void; onConnect?: () => void; onReset?: () => Promise<void>; onReload?: () => Promise<void>;
}) {
 const { t } = useI18n();
 const [draft, setDraft] = useState(value), [query, setQuery] = useState(''), [error, setError] = useState(''), [busy, setBusy] = useState(false), [blocked, setBlocked] = useState(false);
 const working = useRef(false), group = useId();
 const model = models.find(m => m.model === draft.model);
 const filtered = models.filter(m => `${m.name} ${m.model} ${m.description}`.toLocaleLowerCase().includes(query.toLocaleLowerCase()));
 const save = async (reset = false) => {
  if (blocked || working.current || !reset && (!validSelection(draft, models, inherited) || requireEffort && !draft.effort)) return;
  working.current = true; setBusy(true); setError('');
  try { if (reset) await onReset?.(); else await onSave(draft); onClose(); } catch (e) { setError(messageOf(e, t('runtime.actionFailed'))); if(e instanceof ConfigurationError) setBlocked(e.unknown || e.receipt.outcome === 'conflicted'); } finally { working.current = false; setBusy(false); }
 };
 return <SettingsDialog title={title} description={description} busy={busy} onClose={onClose}>
  <input className="runtime-model-search" aria-label={t('runtime.searchModels')} placeholder={t('runtime.searchModelsPlaceholder')} value={query} onChange={e => setQuery(e.target.value)} disabled={busy}/>
  <fieldset disabled={busy} className="runtime-model-options"><legend className="visually-hidden">{t('runtime.availableModels')}</legend>
   {inherited && !query && <label className="runtime-model-option"><input type="radio" name={group} checked={!draft.model} onChange={() => setDraft(inheritedSelection)}/><span><strong>{t('runtime.default')}</strong></span></label>}
   {filtered.map(m => <label className="runtime-model-option" key={m.model}><input type="radio" name={group} checked={draft.model === m.model} onChange={() => { setDraft(chooseModel(m)); setError(''); }}/><span><strong>{m.name || m.model}</strong><small>{m.description || m.model}</small></span></label>)}
   {!filtered.length && <p className="settings-note">{t('runtime.noModelsFound')}</p>}
  </fieldset>
  {onConnect && <button className="text-action" disabled={busy} onClick={onConnect}>{t('runtime.connectOtherModels')}</button>}
  {draft.model && !model && <p role="alert" className="inline-error">{t('runtime.modelUnavailable')}</p>}
  {model && <div className="runtime-model-options-row">
   <label>{t('runtime.reasoningEffort')}<select aria-label={t('runtime.reasoningEffort')} disabled={busy} value={draft.effort} onChange={e => setDraft({ ...draft, effort: e.target.value })}>
    {!requireEffort && <option value="">{t('runtime.effortDecidedByModel')}</option>}{draft.effort && !model.efforts.includes(draft.effort) && <option value={draft.effort}>{draft.effort} · {t('runtime.currentlyUnavailable')}</option>}{model.efforts.filter(Boolean).map(e => <option key={e} value={e}>{getEffortName(e, t)}</option>)}
   </select></label>
   <label>{t('runtime.responseSpeed')}<select aria-label={t('runtime.responseSpeed')} disabled={busy} value={draft.serviceTier} onChange={e => setDraft({ ...draft, serviceTier: e.target.value })}>
    <option value="">{t('runtime.speedDecidedByRuntime')}</option>{draft.serviceTier && !model.serviceTiers.some(t => t.id === draft.serviceTier) && <option value={draft.serviceTier}>{draft.serviceTier} · {t('runtime.currentlyUnavailable')}</option>}{model.serviceTiers.filter(t => t.id).map(t => <option key={t.id} value={t.id}>{tierName(t.id, t.name)}</option>)}
   </select></label>
  </div>}
  {onReset && <button disabled={busy || blocked} className="text-action" onClick={() => void save(true)}>{t('runtime.resetRoleDefaultBinding')}</button>}
  {error && <p role="alert" className="inline-error">{error}{onReload && <button disabled={busy} className="text-action" onClick={() => { void onReload().then(() => { setError(''); setBlocked(false); }).catch(e=>setError(messageOf(e, t('runtime.actionFailed'))));  }}>{t('runtime.reloadKeepSelection')}</button>}</p>}
  <div className="setup-end"><button disabled={busy} onClick={onClose}>{t('common.cancel')}</button><button className="primary" disabled={busy || blocked || !validSelection(draft, models, inherited) || requireEffort && !draft.effort} onClick={() => void save()}>{busy ? t('runtime.saving') : t('common.save')}</button></div>
 </SettingsDialog>;
}
