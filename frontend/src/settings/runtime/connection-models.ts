import type { SetupChoice } from '../../backend/contract';
import type { APIKeyOptions, ConnectionModelOption } from './types';

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

export function apiKeyOptionsFromCatalog(endpoints: readonly SetupChoice[], models: readonly SetupChoice[]): APIKeyOptions {
 return {
  endpoints: endpoints.map(endpoint => ({ value: endpoint.value, name: endpoint.label || endpoint.value, reuseAuth: endpoint.noAuth })),
  models: models.map(connectionModelOption),
 };
}

export function defaultChatModels(models: readonly ConnectionModelOption[]): string[] {
 return models.filter(model => model.chatEligibility === 'chat').map(model => model.value);
}

export function requiresManualCapabilities(model?: ConnectionModelOption): boolean {
 return !model || model.chatEligibility === 'unknown';
}

export function eligibleSelection(models: readonly ConnectionModelOption[], selected: readonly string[], confirmed: ReadonlySet<string>): string[] {
 const eligible = new Set(defaultChatModels(models));
 return selected.filter(id => eligible.has(id) && !confirmed.has(id));
}
