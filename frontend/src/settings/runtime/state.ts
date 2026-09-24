import type { ModelOption, SetupChoice } from '../../backend/contract';
import type { ConnectionFlow, ConnectionGroup, ModelSelection, RuntimeView } from './types';

export const inheritedSelection: ModelSelection = { model: '', effort: '', serviceTier: '' };
export const effortName: Record<string, string> = { none: '无', minimal: '最低', low: '低', medium: '中', high: '高', xhigh: '极高', max: '最高', ultra: '超高' };
export const tierName = (id: string, name: string) => id === 'fast' || name.toLowerCase() === 'fast' ? 'Fast' : name || id;
export const messageOf = (error: unknown) => error instanceof Error ? error.message : '操作未完成，请稍后重试';

export function selectionSummary(selection: ModelSelection, models: ModelOption[]) {
 const model = models.find(m => m.model === selection.model);
 if (!selection.model) return { name: '沿用运行时', detail: '模型、推理强度与速度' };
 return {
  name: model?.name || selection.model,
  detail: [effortName[selection.effort] || selection.effort || '默认推理', selection.serviceTier ? tierName(selection.serviceTier, model?.serviceTiers.find(t => t.id === selection.serviceTier)?.name || selection.serviceTier) : '默认速度'].join(' · '),
 };
}
export function chooseModel(model: ModelOption): ModelSelection {
 return { model: model.model, effort: model.defaultEffort, serviceTier: '' };
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
