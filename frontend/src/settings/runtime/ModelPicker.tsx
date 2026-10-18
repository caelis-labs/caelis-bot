import {Recovery} from '../../SurfaceBoundary';
import { SettingsSelect } from '../../SettingsSelect';
import { useId, useRef, useState } from 'react';
import type { ModelOption } from '../../backend/contract';
import type { ModelSelection } from './types';
import { changeModelParameters, chooseModel, defaultModel, fastTier, getEffortName, inheritedSelection, messageOf, selectionSummary, initialModelSelection, tierName, validSelection } from './state';
import { ConfigurationError } from './client';
import { SettingsDialog } from './SettingsDialog';
import { useI18n } from '../../i18n';
import { ArrowClockwiseIcon, LightningIcon, SettingsChevron } from '../../SettingsIcons';

export function ModelSummary({ value, models, disabled, onClick, label, unbound = false, showDetail = true, runtimeDefault }: {
 value: ModelSelection; models: ModelOption[]; disabled?: boolean; onClick: (anchor: HTMLElement) => void; label: string; unbound?: boolean; showDetail?: boolean; runtimeDefault?: ModelSelection | null;
}) {
 const { t } = useI18n();
 const summary = unbound && !value.model ? { name: t('runtime.unbound'), detail: '' } : selectionSummary(value, models, t, runtimeDefault);
 return <button className="runtime-model-summary" aria-label={t('runtime.configureModel', { label })} disabled={disabled} onClick={event => onClick(event.currentTarget)}><span><strong>{summary.name}</strong>{showDetail && summary.detail && <small>{summary.detail}</small>}</span><SettingsChevron/></button>;
}

export function ModelPicker({ title, description, value, models, inherited = false, requireEffort = false, onSave, onClose, onConnect, onReset, onReload, anchor, runtimeDefault, selectInitialModel=false, sourceLabels={}, missingModelLabel, unavailableLabel, catalogPending=false }: {
 missingModelLabel?:string; unavailableLabel?:string; catalogPending?:boolean; sourceLabels?: Record<string,string>; selectInitialModel?: boolean; emptyMessage?: string; showSources?: boolean; title: string; description?: string; value: ModelSelection; models: ModelOption[]; inherited?: boolean; requireEffort?: boolean; anchor?: HTMLElement | null; runtimeDefault?: ModelSelection | null;
 onSave: (value: ModelSelection) => Promise<void>; onClose: () => void; onConnect?: () => void; onReset?: () => Promise<void>; onReload?: () => Promise<void>;
}) {
 const { t } = useI18n();
 const [draft, setDraft] = useState(() => selectInitialModel ? initialModelSelection(value, models) : value), [error, setError] = useState(''), [busy, setBusy] = useState(false), [blocked, setBlocked] = useState(false);
 const working = useRef(false);
 const model = draft.model ? models.find(m => m.model === draft.model) : defaultModel(models, runtimeDefault);
 const effort = draft.effort || (!draft.model ? runtimeDefault?.effort : '') || model?.defaultEffort || '';
 const serviceTier = draft.serviceTier || (!draft.model ? runtimeDefault?.serviceTier : '') || '';
 const dirty = draft.model !== value.model || draft.effort !== value.effort || draft.serviceTier !== value.serviceTier;
 const change = (patch: Partial<ModelSelection>) => { if (model) { setDraft(changeModelParameters(draft, model, patch, runtimeDefault)); setError(''); } };
 const save = async (reset = false) => {
  if (blocked || working.current || !reset && (!validSelection(draft, models, inherited) || requireEffort && !draft.effort)) return;
  working.current = true; setBusy(true); setError('');
  try { if (reset) await onReset?.(); else await onSave(draft); onClose(); }
  catch (e) { setError(messageOf(e, t('runtime.actionFailed'))); if (e instanceof ConfigurationError) setBlocked(e.unknown || e.receipt.outcome === 'conflicted'); }
  finally { working.current = false; setBusy(false); }
 };
 return <>
  <SettingsDialog className="runtime-model-picker runtime-model-popover runtime-model-compact" anchor={anchor} title={title} description={description} busy={busy} returnFocus={anchor} onClose={onClose}>
   <div className="runtime-compact-model-row">
    <span>{t('runtime.model')}</span>
    <SettingsSelect searchable aria-label={t('runtime.model')} disabled={busy||blocked} value={draft.model} onChange={event=>{const selected=models.find(item=>item.model===event.target.value);setDraft(selected?chooseModel(selected):inheritedSelection);setError('');}}>
     {(!draft.model||inherited)&&<option value="">{t('runtime.default')}</option>}
     {draft.model&&!model&&<option value={draft.model}>{unavailableLabel||missingModelLabel||draft.model}</option>}
     {models.map(item=><option key={item.model} value={item.model} data-badge={sourceLabels[item.model]}>{item.name||item.model}</option>)}
    </SettingsSelect>
    {fastTier(model)&&<button className="runtime-compact-fast" title={t('runtime.fastMode')} aria-label={t('runtime.fastMode')} aria-pressed={serviceTier===fastTier(model)!.id} disabled={busy||blocked} onClick={()=>change({serviceTier:serviceTier===fastTier(model)!.id?'':fastTier(model)!.id})}><LightningIcon aria-hidden="true" size={18}/></button>}
   </div>
   <ModelParameters hideFast model={model} value={{...draft,effort,serviceTier}} disabled={busy||blocked} onChange={change}/>
   {onConnect&&<button className="text-action runtime-model-connect" onClick={onConnect}>{t('runtime.connectOtherModels')}</button>}

   {draft.model && !model && catalogPending ? <p role="status" className="settings-note">{t('common.loading')}</p> : draft.model && (!model || !validSelection(draft,models,inherited)) && <p role="alert" className="inline-error">{t('runtime.modelUnavailable')}</p>}
   {error&&<Recovery onRetry={onReload?()=>{void onReload().then(()=>{setError('');setBlocked(false)}).catch(()=>{});}:undefined}/>}

   <div className="setup-end runtime-model-actions">{model && <button disabled={busy || blocked} className="text-action runtime-reset-parameters" onClick={() => { setDraft(draft.model ? chooseModel(model) : inheritedSelection); setError(''); }}><ArrowClockwiseIcon aria-hidden="true" size={13}/>{t('runtime.resetParameters')}</button>}{!model && <span/>}<button disabled={busy} onClick={onClose}>{t('common.cancel')}</button><button className="primary" disabled={busy || blocked || !dirty || !validSelection(draft,models,inherited) || requireEffort && !draft.effort} onClick={() => void save()}>{busy ? t('runtime.saving') : t('common.save')}</button></div>
   {onReset && <button disabled={busy || blocked} className="text-action runtime-reset-binding" onClick={() => void save(true)}>{t('runtime.resetRoleDefaultBinding')}</button>}
  </SettingsDialog>

 </>;
}

export function ModelParameters({model,value:draft,disabled:busy=false,allowDefault=false,hideFast=false,onChange:change}:{hideFast?:boolean;allowDefault?:boolean;model?:ModelOption;value:ModelSelection;disabled?:boolean;onChange:(patch:Partial<ModelSelection>)=>void}) {
 const {t}=useI18n();
 const levels=['none','minimal','low','medium','high','xhigh','max','ultra'];
 const fast=fastTier(model), efforts=(model?.efforts.filter(Boolean)||[]).sort((a,b)=>levels.indexOf(a)-levels.indexOf(b));
 const effort=allowDefault?draft.effort:draft.effort||model?.defaultEffort||'', serviceTier=draft.serviceTier;
 const effortGroup=useId();
 const strength=effort?getEffortName(effort,t):t('runtime.default');
 const hasExtraSpeed=!!model&&(model.serviceTiers.some(tier=>tier.id&&tier.id!==fast?.id)||!!draft.serviceTier&&!model.serviceTiers.some(tier=>tier.id===draft.serviceTier));
 return <>   {model && (fast || efforts.length > 0 || hasExtraSpeed) && <div className="model-parameters"><div className="runtime-model-parameters">
    {fast && !hideFast && <button className="runtime-fast-toggle" aria-label={t('runtime.fastMode')} aria-pressed={serviceTier === fast.id} disabled={busy} onClick={() => change({ serviceTier: serviceTier === fast.id ? '' : fast.id })}><LightningIcon aria-hidden="true" size={17}/>{t('runtime.fastMode')}</button>}
    {model && !!efforts.length && <div className="runtime-effort-control"><div className="runtime-parameter-heading"><span id={effortGroup}>{t('runtime.reasoningEffort')}</span>{!effort && !allowDefault && <output>{strength}</output>}</div><div className="runtime-effort-segments" role="radiogroup" aria-labelledby={effortGroup}>{(allowDefault?['',...efforts]:efforts).map(level => <label key={level} title={level||t('runtime.serviceDefault')}><input type="radio" aria-label={level?getEffortName(level,t):t('runtime.serviceDefault')} name={effortGroup} value={level} checked={effort === level} disabled={busy} onChange={() => change({effort:level})}/><span>{level?getEffortName(level,t):t('runtime.default')}</span></label>)}</div></div>}
    {model && hasExtraSpeed && <label className="runtime-extra-speed">{t('runtime.responseSpeed')}<SettingsSelect disabled={busy} value={draft.serviceTier} onChange={e => change({ serviceTier: e.target.value })}><option value="">{t('runtime.default')}</option>{draft.serviceTier && !model.serviceTiers.some(tier => tier.id === draft.serviceTier) && <option value={draft.serviceTier}>{draft.serviceTier} · {t('runtime.currentlyUnavailable')}</option>}{model.serviceTiers.filter(tier => tier.id).map(tier => <option key={tier.id} value={tier.id}>{tierName(tier.id,tier.name)}</option>)}</SettingsSelect></label>}
   </div></div>}</>;
}
