import type { SetupChoice } from '../../backend/contract';
import type { ConnectionModelOption } from './types';

// Only an explicit Host assertion qualifies a directory item for the chat list.
export function connectionModelOption(choice: SetupChoice): ConnectionModelOption {
 const eligibility = choice.chatEligibility;
 return {
  value: choice.value,
  name: choice.label || choice.value,
  detail: choice.detail || '',
  chatEligibility: eligibility === 'chat' || eligibility === 'non-chat' ? eligibility : 'unknown',
  metadataComplete: choice.modelMetadataComplete ?? undefined,
  imageInputKnown: choice.modelImageInputKnown ?? undefined,
 };
}

export function defaultChatModels(models: readonly ConnectionModelOption[]): string[] {
 return models.filter(model => model.chatEligibility === 'chat').map(model => model.value);
}

export function eligibleSelection(models: readonly ConnectionModelOption[], selected: readonly string[], confirmed: ReadonlySet<string>): string[] {
 const eligible = new Set(defaultChatModels(models));
 return selected.filter(id => eligible.has(id) && !confirmed.has(id));
}
