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
 return {key:['pairing_request_closed','pairing_request_changed','pairing_request_unknown'].includes(value)?'settings.remoteRequestClosed':value==='copy'?'settings.botCopyFailed':value==='read'?'settings.botReadFailed':value==='worker_listener_unavailable'?'settings.botListenerUnavailable':'settings.pairUnavailable',settings:false} as const;
}
