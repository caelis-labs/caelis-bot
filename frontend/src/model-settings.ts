import type { ExecutionSettings, WorkExecutionSettings } from './backend/contract';

export type ModelPreferences = { conversation: ExecutionSettings; work: WorkExecutionSettings };
export const sameModelSettings = (a: WorkExecutionSettings, b: WorkExecutionSettings) =>
 a.model === b.model && a.effort === b.effort && a.serviceTier === b.serviceTier;
export const sameExecutionSettings = (a: ExecutionSettings, b: ExecutionSettings) =>
 sameModelSettings(a, b) && a.approvalMode === b.approvalMode;

// Keep the independent save receipts: a rejected second write stays dirty and
// retry must not repeat the first write or override untouched inherited defaults.
export async function saveModelChanges(next: ModelPreferences, saved: ModelPreferences,
 write: (method: string, value: ExecutionSettings | WorkExecutionSettings) => Promise<unknown>,
 accepted: (scope: keyof ModelPreferences) => void) {
 const completed: string[] = [];
 for (const scope of ['conversation', 'work'] as const) {
  const same = scope === 'conversation' ? sameExecutionSettings(next.conversation, saved.conversation) : sameModelSettings(next.work, saved.work);
  if (same) continue;
  const label = scope === 'conversation' ? '对话设置' : '工作任务设置';
  try {
   await write(scope === 'conversation' ? 'SaveExecutionSettings' : 'SaveWorkExecutionSettings', next[scope]);
   accepted(scope); completed.push(label);
  } catch (e) {
   throw new Error(`${completed.length ? completed.join('、') + '已保存；' : ''}${label}未保存：${e instanceof Error ? e.message : '请重试'}`);
  }
 }
}
