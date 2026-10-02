import type {ApprovalMode} from './backend/contract';
import type {MessageKey} from './i18n/catalogs';

const labels:Record<string,[MessageKey,MessageKey]>={
 auto:['settings.policyAuto','settings.policyAutoDescription'],
 ask:['settings.policyAsk','settings.policyAskDescription'],
 'read-only':['settings.policyReadOnly','settings.policyReadOnlyDescription'],
 'full-access':['settings.policyFullAccess','settings.policyFullAccessDescription'],
 'workspace-write':['settings.policyAuto','settings.policyGuardianDescription'],
};
// Translate built-in Bot policy copy only. Keep native IDs, available choices
// and the dangerous flag intact; preserve unknown backend descriptions.
export function executionMode(mode:ApprovalMode,t:(key:MessageKey)=>string):ApprovalMode {
 const keys=labels[mode.id];return keys?{...mode,name:t(keys[0]),description:t(keys[1])}:mode;
}
