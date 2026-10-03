import { useEffect, useRef, useState } from 'react';
import type { ModelOption } from '../../backend/contract';
import type { ModelSelection } from './types';
import { changeModelParameters, chooseModel, defaultModel, fastTier, getEffortName, inheritedSelection, messageOf, selectionSummary, tierName, validSelection } from './state';
import { ConfigurationError } from './client';
import { SettingsDialog } from './SettingsDialog';
import { useI18n } from '../../i18n';
import { ArrowClockwiseIcon, CheckIcon, LightningIcon, MagnifyingGlassIcon, SettingsChevron } from '../../SettingsIcons';

export function ModelSummary({ value, models, disabled, onClick, label, unbound = false, showDetail = true, runtimeDefault }: {
 value: ModelSelection; models: ModelOption[]; disabled?: boolean; onClick: (anchor: HTMLElement) => void; label: string; unbound?: boolean; showDetail?: boolean; runtimeDefault?: ModelSelection | null;
}) {
 const { t } = useI18n();
 const summary = unbound && !value.model ? { name: t('runtime.unbound'), detail: '' } : selectionSummary(value, models, t, runtimeDefault);
 return <button className="runtime-model-summary" aria-label={t('runtime.configureModel', { label })} disabled={disabled} onClick={event => onClick(event.currentTarget)}><span><strong>{summary.name}</strong>{showDetail && summary.detail && <small>{summary.detail}</small>}</span><SettingsChevron/></button>;
}

function ModelListDrawer({ models, value, inherited, runtimeDefault, anchor, onChoose, onClose, onConnect }: {
 models: ModelOption[]; value: ModelSelection; inherited: boolean; runtimeDefault?: ModelSelection | null; anchor: HTMLElement | null; onChoose: (model: ModelOption | null) => void; onClose: () => void; onConnect?: () => void;
}) {
 const { t } = useI18n();
 const [query, setQuery] = useState('');
 const search = useRef<HTMLInputElement>(null), list = useRef<HTMLDivElement>(null);
 useEffect(() => { search.current?.focus(); }, []);
 const currentDefault = defaultModel(models, runtimeDefault), defaultName = currentDefault ? `${t('runtime.default')} · ${currentDefault.name || currentDefault.model}` : t('runtime.default');
 const filtered = models.filter(m => `${m.name} ${m.model}`.toLocaleLowerCase().includes(query.toLocaleLowerCase()));
 const showDefault = inherited && defaultName.toLocaleLowerCase().includes(query.toLocaleLowerCase());
 return <SettingsDialog title={t('runtime.selectModel')} className="runtime-model-drawer" anchor={anchor} alignTo={anchor?.closest<HTMLElement>('.runtime-model-popover')} onClose={onClose}>
  <div className="runtime-model-list-body" onKeyDown={event => {
   if (!['ArrowDown','ArrowUp','Home','End'].includes(event.key) || event.target === search.current && event.key !== 'ArrowDown') return;
   const items = [...list.current!.querySelectorAll<HTMLButtonElement>('button')]; if (!items.length) return;
   event.preventDefault(); const index = items.indexOf(document.activeElement as HTMLButtonElement);
   const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : event.key === 'ArrowDown' ? (index + 1) % items.length : index <= 0 ? items.length - 1 : index - 1;
   items[next].focus();
  }}>
   <div className="runtime-model-search-field"><MagnifyingGlassIcon aria-hidden="true" size={15}/><input ref={search} spellCheck={false} autoComplete="off" autoCorrect="off" aria-label={t('runtime.searchModels')} placeholder={t('runtime.searchModels')} value={query} onChange={e => setQuery(e.target.value)}/></div>
   <div ref={list} className="runtime-model-list" role="listbox" aria-label={t('runtime.availableModels')}>
    {showDefault && <button role="option" aria-selected={!value.model} onClick={() => onChoose(null)}><span>{defaultName}</span>{!value.model && <CheckIcon aria-hidden="true" size={16}/>}</button>}
    {filtered.map(model => <button key={model.model} role="option" aria-selected={value.model === model.model} onClick={() => onChoose(model)}><span>{model.name || model.model}</span>{fastTier(model) && <LightningIcon aria-hidden="true" size={14}/>}{value.model === model.model && <CheckIcon aria-hidden="true" size={16}/>}</button>)}
    {!filtered.length && !showDefault && <p className="settings-note">{t('runtime.noModelsFound')}</p>}
   </div>
   {onConnect && <button className="text-action runtime-model-connect" onClick={onConnect}>{t('runtime.connectOtherModels')}</button>}
  </div>
 </SettingsDialog>;
}

export function ModelPicker({ title, description, value, models, inherited = false, requireEffort = false, onSave, onClose, onConnect, onReset, onReload, anchor, runtimeDefault }: {
 title: string; description?: string; value: ModelSelection; models: ModelOption[]; inherited?: boolean; requireEffort?: boolean; anchor?: HTMLElement | null; runtimeDefault?: ModelSelection | null;
 onSave: (value: ModelSelection) => Promise<void>; onClose: () => void; onConnect?: () => void; onReset?: () => Promise<void>; onReload?: () => Promise<void>;
}) {
 const { t } = useI18n();
 const [draft, setDraft] = useState(value), [catalog, setCatalog] = useState(false), [error, setError] = useState(''), [busy, setBusy] = useState(false), [blocked, setBlocked] = useState(false);
 const working = useRef(false), modelButton = useRef<HTMLButtonElement>(null);
 const model = draft.model ? models.find(m => m.model === draft.model) : defaultModel(models, runtimeDefault), fast = fastTier(model);
 const efforts = model?.efforts.filter(Boolean) || [];
 const effort = draft.effort || (!draft.model ? runtimeDefault?.effort : '') || model?.defaultEffort || '';
 const serviceTier = draft.serviceTier || (!draft.model ? runtimeDefault?.serviceTier : '') || '';
 const effortIndex = Math.max(0, efforts.indexOf(effort));
 const dirty = draft.model !== value.model || draft.effort !== value.effort || draft.serviceTier !== value.serviceTier;
 const closeCatalog = () => { setCatalog(false); requestAnimationFrame(() => modelButton.current?.focus()); };
 const change = (patch: Partial<ModelSelection>) => { if (model) { setDraft(changeModelParameters(draft, model, patch, runtimeDefault)); setError(''); } };
 const save = async (reset = false) => {
  if (blocked || working.current || !reset && (!validSelection(draft, models, inherited) || requireEffort && !draft.effort)) return;
  working.current = true; setBusy(true); setError('');
  try { if (reset) await onReset?.(); else await onSave(draft); onClose(); }
  catch (e) { setError(messageOf(e, t('runtime.actionFailed'))); if (e instanceof ConfigurationError) setBlocked(e.unknown || e.receipt.outcome === 'conflicted'); }
  finally { working.current = false; setBusy(false); }
 };
 const strength = effort ? getEffortName(effort, t) : t('runtime.default');
 return <>
  <SettingsDialog className="runtime-model-popover" title={title} busy={busy} blocked={catalog} anchor={anchor} onClose={onClose}>
   <div className="runtime-model-parameters">
    <div className="runtime-model-choice-row"><span>{t('runtime.model')}</span><button ref={modelButton} className="runtime-current-model" aria-label={t('runtime.selectModel')} aria-haspopup="dialog" aria-expanded={catalog} title={description} disabled={busy} onClick={() => setCatalog(true)}><span>{model?.name || draft.model || t(inherited?'runtime.default':'runtime.selectModel')}</span>{!draft.model && model && <small>{t('runtime.default')}</small>}<SettingsChevron/></button>{fast && <button className="runtime-fast-toggle" aria-label={t('runtime.fastMode')} aria-pressed={serviceTier === fast.id} disabled={busy} title={t('runtime.fastMode')} onClick={() => change({ serviceTier: serviceTier === fast.id ? '' : fast.id })}><LightningIcon aria-hidden="true" size={17}/></button>}</div>
    {model && !!efforts.length && <div className="runtime-effort-control"><div className="runtime-parameter-heading"><span>{t('runtime.reasoningEffort')}</span><output aria-live="polite">{strength}</output></div>{efforts.length > 1 && <><div className={`runtime-effort-track${effort ? '' : ' is-unresolved'}`} style={{ '--effort-progress': `${effortIndex / (efforts.length - 1) * 100}%` } as React.CSSProperties}><div className="runtime-effort-ticks" aria-hidden="true">{efforts.map((level, index) => <i key={level} className={index <= effortIndex ? 'reached' : ''}/>)}</div><input type="range" aria-label={t('runtime.reasoningEffort')} aria-valuetext={strength} min={0} max={efforts.length - 1} step={1} value={effortIndex} disabled={busy} onChange={e => change({ effort: efforts[e.target.valueAsNumber] })} onPointerUp={e => { if (!effort) change({ effort: efforts[e.currentTarget.valueAsNumber] }); }}/></div><div className="runtime-effort-labels" aria-hidden="true"><span style={{ left: '0%' }}>{getEffortName(efforts[0],t)}</span><span style={{ left: '100%' }}>{getEffortName(efforts[efforts.length - 1],t)}</span></div></>}</div>}
    {model && (model.serviceTiers.some(tier => tier.id && tier.id !== fast?.id) || draft.serviceTier && !model.serviceTiers.some(tier => tier.id === draft.serviceTier)) && <label className="runtime-extra-speed">{t('runtime.responseSpeed')}<select disabled={busy} value={draft.serviceTier} onChange={e => change({ serviceTier: e.target.value })}><option value="">{t('runtime.default')}</option>{draft.serviceTier && !model.serviceTiers.some(tier => tier.id === draft.serviceTier) && <option value={draft.serviceTier}>{draft.serviceTier} · {t('runtime.currentlyUnavailable')}</option>}{model.serviceTiers.filter(tier => tier.id).map(tier => <option key={tier.id} value={tier.id}>{tierName(tier.id,tier.name)}</option>)}</select></label>}
   </div>
   {draft.model && (!model || !validSelection(draft,models,inherited)) && <p role="alert" className="inline-error">{t('runtime.modelUnavailable')}</p>}
   {error && <div role="alert" className="inline-error"><p>{error}</p>{onReload && <button disabled={busy} className="text-action" onClick={() => { void onReload().then(() => { setError(''); setBlocked(false); }).catch(e => setError(messageOf(e,t('runtime.actionFailed')))); }}>{t('runtime.reloadKeepSelection')}</button>}</div>}
   <div className="setup-end runtime-model-actions">{model && <button disabled={busy || blocked} className="text-action runtime-reset-parameters" onClick={() => { setDraft(draft.model ? chooseModel(model) : inheritedSelection); setError(''); }}><ArrowClockwiseIcon aria-hidden="true" size={13}/>{t('runtime.resetParameters')}</button>}<button disabled={busy} onClick={onClose}>{t('common.cancel')}</button><button className="primary" disabled={busy || blocked || !dirty || !validSelection(draft,models,inherited) || requireEffort && !draft.effort} onClick={() => void save()}>{busy ? t('runtime.saving') : t('common.save')}</button></div>
   {onReset && <button disabled={busy || blocked} className="text-action runtime-reset-binding" onClick={() => void save(true)}>{t('runtime.resetRoleDefaultBinding')}</button>}
  </SettingsDialog>
  {catalog && <ModelListDrawer models={models} value={draft} inherited={inherited} runtimeDefault={runtimeDefault} anchor={modelButton.current} onClose={closeCatalog} onChoose={model => { if ((model?.model || '') !== draft.model) setDraft(model ? chooseModel(model) : inheritedSelection); setError(''); closeCatalog(); }} onConnect={onConnect}/>}
 </>;
}
