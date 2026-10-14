import type {SetupState} from '../../backend/contract';

// Caelis reports a configured model separately from its proven chat capability.
// Older Hosts omit this field; absence must not become a readiness claim.
type QualifiedModel = SetupState['models'][number] & {chatEligibility?: string};

export function hasChatModel(state: SetupState): boolean {
 return state.models.some(model => !model.noAuth && (model as QualifiedModel).chatEligibility === 'chat');
}

export function verifiedSetup(runtime: string, state: SetupState): SetupState {
 if (runtime !== 'caelis' || state.state !== 'ready' || hasChatModel(state)) return state;
 return {...state,state:'models'};
}

export type ConnectionReadiness = 'unknown'|'not_installed'|'unavailable'|'no_models'|'auth_needed'|'chat_unverified'|'no_chat_model'|'ready';
export function connectionReadiness(runtime: string, state?: SetupState | null): ConnectionReadiness {
 if (!state) return 'unknown';
 if (!state.installation.installed) return 'not_installed';
 if (runtime === 'codex') return state.state === 'ready' ? 'ready' : 'unavailable';
 if (state.state === 'service' || state.state === 'incompatible' || state.state === 'unavailable') return 'unavailable';
 if (!state.models.length) return 'no_models';
 if (hasChatModel(state)) return state.state === 'ready' ? 'ready' : 'unavailable';
 const connected=state.models.filter(model=>!model.noAuth);
 if (connected.length) return connected.every(model=>(model as QualifiedModel).chatEligibility==='non-chat')?'no_chat_model':'chat_unverified';
 return 'auth_needed';
}

export function workerCardState(id: string, saved: string, available?: boolean): 'selected-ready'|'saved-waiting'|'ready'|'setup' {
 if (id === saved) return available === true ? 'selected-ready' : 'saved-waiting';
 return available === true ? 'ready' : 'setup';
}

export function botCardState(id: string, active: string, pending: string): 'current'|'pending'|'other' {
 if (id === active && active !== '') return 'current';
 return id === pending && pending !== '' ? 'pending' : 'other';
}
