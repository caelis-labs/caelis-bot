export const pairingReadiness = {
 worker_runtime_missing:'settings.pairRuntimeMissing',
 worker_runtime_unavailable:'settings.pairRuntimeUnavailable',
 worker_runtime_incompatible:'settings.pairRuntimeIncompatible',
 worker_login_required:'settings.pairLoginRequired',
 worker_model_authorization_required:'settings.pairModelAuthorization',
 worker_model_required:'settings.pairModelRequired',
 worker_model_catalog_unavailable:'settings.pairModelCatalogUnavailable',
 worker_model_unavailable:'settings.pairModelUnavailable',
 worker_effort_unsupported:'settings.pairEffortUnsupported',
 worker_tier_unsupported:'settings.pairTierUnsupported',
 worker_configuration_invalid:'settings.pairConfigurationInvalid',
 worker_configuration_changed:'settings.pairConfigurationChanged',
 worker_readiness_timeout:'settings.pairReadinessTimeout',
} as const;
export function pairingProblem(value:string) {
 if(Object.hasOwn(pairingReadiness,value))return {key:pairingReadiness[value as keyof typeof pairingReadiness],settings:!['worker_configuration_changed','worker_readiness_timeout'].includes(value)};
 return {key:['pairing_request_closed','pairing_request_changed','pairing_request_unknown'].includes(value)?'settings.remoteRequestClosed':value==='copy'?'settings.botCopyFailed':value==='read'?'settings.botReadFailed':value==='worker_listener_unavailable'?'settings.botListenerUnavailable':'settings.remoteDecisionUnconfirmed',settings:false} as const;
}

export function connectionProblem(value:string){
 const keys={
  duplicate_bot_name:'settings.machineIdentifierDuplicate',
  invalid_bot_name:'settings.machineIdentifierInvalid',
  bot_connection_target_unconfirmed:'settings.remoteTargetUnconfirmed',
  pairing_requests_full:'settings.remoteRequestsFull',
  bot_peer_unreachable:'settings.remoteAddressUnreachable',
  invalid_bot_address:'settings.botAddressInvalid',
  original_peer_proof_required:'settings.botProofRequired',
  operation_unconfirmed:'settings.remoteRequestUnknown',
  peer_contract_incompatible:'settings.remoteVersionMismatch',
  bot_worker_contract_incompatible:'settings.remoteVersionMismatch',
  peer_authorization_revoked:'settings.remoteRevoked',
  pairing_request_closed:'settings.remoteRequestClosed',
 } as const;
 return {key:Object.hasOwn(keys,value)?keys[value as keyof typeof keys]:'settings.remoteCheckConnection'} as const;
}

export function incomingProblem(issue:string){
 return issue==='worker_peer_state_unsupported'?'settings.remoteStateVersion':issue==='worker_peer_state_permission'?'settings.remoteStatePermission':issue==='worker_peer_state_unavailable'?'settings.remoteStateUnreadable':issue==='worker_listener_unavailable'?'settings.remoteReceptionUnavailable':'settings.remoteInboxUnreadable';
}
