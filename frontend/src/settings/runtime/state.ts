import type { ModelOption, SetupChoice } from '../../backend/contract';
import type { ConnectionFlow, ConnectionGroup, ModelSelection, RuntimeView } from './types';

export const inheritedSelection: ModelSelection = { model: '', effort: '', serviceTier: '' };
export const effortName: Record<string, string> = { none: 'none', minimal: 'minimal', low: 'low', medium: 'medium', high: 'high', xhigh: 'xhigh', max: 'max', ultra: 'ultra' };
export const tierName = (id: string, name: string) => id === 'fast' || name.toLowerCase() === 'fast' ? 'Fast' : name || id;
// Exceptions are diagnostics, never product copy. Keep receipts on their typed
// owner; callers render a recovery surface without forwarding exception prose.
export const messageOf = (_error: unknown, fallback = '操作未完成，请稍后重试') => fallback;

export function getEffortName(effort: string, t?: (key: any) => string): string {
 if (t && effort) {
  const key = `runtime.effort_${effort}`;
  try {
   const translated = t(key);
   if (translated && translated !== key) return translated;
  } catch {
   // fallback
  }
 }
 return effortName[effort] || effort || '';
}

export function formatModelUse(use: string, t: (key: any) => string): string {
 if (use === 'Bot 对话' || use === 'bot_conversation') return t('runtime.uses_conversation');
 if (use === 'Caelis 主模型' || use === 'caelis_main') return t('runtime.uses_main');
 if (use === '独立工作模型' || use === 'dedicated_work') return t('runtime.uses_work');
 return use;
}

export function defaultModel(models: ModelOption[], configured?: ModelSelection | null) {
 // Catalog recommendations do not identify the user's configured default.
 // Show a model name only when native configuration supplies it.
 return configured?.model ? models.find(m => m.model === configured.model) : undefined;
}
export function fastTier(model?: ModelOption) {
 return model?.serviceTiers.find(t => t.id === 'fast' || t.name.toLowerCase() === 'fast');
}
export function selectionSummary(selection: ModelSelection, models: ModelOption[], t?: (key: any) => string, configured?: ModelSelection | null) {
 const fallback = !selection.model, model = fallback ? defaultModel(models, configured) : models.find(m => m.model === selection.model);
 const inherited = fallback && model;
 const effort = selection.effort || (inherited ? configured?.effort || model.defaultEffort : '');
 const serviceTier = selection.serviceTier || (inherited ? configured?.serviceTier || '' : '');
 return {
  name: model?.name || selection.model || (t ? t('runtime.default') : '默认'),
  detail: [inherited ? (t ? t('runtime.default') : '默认') : '', getEffortName(effort, t), serviceTier ? tierName(serviceTier, model?.serviceTiers.find(t => t.id === serviceTier)?.name || serviceTier) : ''].filter(Boolean).join(' · '),
 };
}
export function changeModelParameters(selection: ModelSelection, model: ModelOption, patch: Partial<ModelSelection>, configured?: ModelSelection | null): ModelSelection {
 return { ...(!selection.model ? { ...chooseModel(model), effort: configured?.effort || model.defaultEffort, serviceTier: configured?.serviceTier || '' } : selection), ...patch };
}
export function chooseModel(model: ModelOption): ModelSelection {
 return { model: model.model, effort: model.defaultEffort, serviceTier: '' };
}
// Opening an unbound role creates a draft only. Never replace a saved model,
// even if it is temporarily unavailable, or persist a catalog guess implicitly.
export function initialModelSelection(value: ModelSelection, models: ModelOption[]): ModelSelection {
 return value.model || !models.length ? value : chooseModel(models[0]);
}
export function validSelection(selection: ModelSelection, models: ModelOption[], allowInherited = false) {
 if (!selection.model) return allowInherited && !selection.effort && !selection.serviceTier;
 const model = models.find(m => m.model === selection.model);
 return !!model && (!selection.effort || model.efforts.includes(selection.effort)) && (!selection.serviceTier || model.serviceTiers.some(t => t.id === selection.serviceTier));
}
export function safeWebURL(raw: string) {
 try { const url = new URL(raw); return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password; } catch { return false; }
}
// Progress can race browser loopback completion, pasted-code submission and
// cancellation. Native projection supplies the ordering; never compare opaque
// revisions lexically or let an older auth screen replace a completed flow.
export function acceptConnectionProgress(current: ConnectionFlow | null, next: ConnectionFlow) {
 if (current && current.id === next.id && (next.sequence < current.sequence || current.stage === 'complete' && next.stage !== 'complete')) return current;
 return next;
}
// Legacy SetupChoice has no structured group identity. This affects display
// only: deletion always uses the complete selector supplied by the Host.
export function groupLegacyModels(models: SetupChoice[], selected: string): ConnectionGroup[] {
 const groups = new Map<string, ConnectionGroup>();
 for (const model of models) {
  const separator = model.value.indexOf('/');
  const id = separator > 0 ? model.value.slice(0, separator) : 'models';
  const group = groups.get(id) || { id, name: id === 'models' ? '已连接的模型' : id, kind: 'provider' as const, detail: '保存在本机 Caelis', models: [] };
  group.models.push({ id: model.value, name: model.label || model.value, unavailable: model.noAuth, uses: [model.value === selected ? 'Bot 对话' : '', model.current ? 'Caelis 主模型' : ''].filter(Boolean) });
  groups.set(id, group);
 }
 return [...groups.values()];
}
export function withWorkUsage(view: RuntimeView): RuntimeView {
 if (!view.work?.model) return view;
 return { ...view, connections: view.connections.map(group => ({ ...group, models: group.models.map(model => model.id === view.work?.model ? { ...model, uses: [...model.uses, '独立工作模型'] } : model) })) };
}

export function modelUseLabel(use:string):string {
 if (['Bot 对话','bot_conversation'].includes(use)) return 'Bot';
 if (['Caelis 主模型','caelis_main'].includes(use)) return 'built-in';
 if (['独立工作模型','dedicated_work'].includes(use)) return 'Worker';
 return use==='guardian-screen'?'guardian':use;
}

// Onboarding always starts with an explicit supported level, without changing saved settings.
export function onboardingEffort(efforts: string[]): string {
 const order = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra'];
 const available = [...new Set(efforts.filter(Boolean))].sort((a,b)=>order.indexOf(a)-order.indexOf(b));
 if (available.includes('medium')) return 'medium';
 return available[Math.floor(available.length / 2)] || '';
}
export function modelDescription(description: string | undefined, ...names: string[]): string | undefined {
 const text = description?.trim();
 if (text && text.split('·').every(part => ['native preset','reasoning','tools'].includes(part.trim().toLowerCase()))) return undefined;
 const normalize = (value: string) => value.toLowerCase().replace(/[^\p{L}\p{N}]/gu, '');
 return text && !names.some(name => normalize(name) === normalize(text)) ? text : undefined;
}

export function validCustomLimits(context: string, output: string): boolean {
 return [context,output].every(value=>value.trim()!==''&&Number.isSafeInteger(Number(value))&&Number(value)>0);
}
