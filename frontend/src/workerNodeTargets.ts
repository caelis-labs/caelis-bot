import type {WorkTarget,WorkerNodeConfig,WorkerNodeSetup} from './backend/contract';

export type WorkerTargetAction='ProbeWorkerTarget'|'ConnectWorkerTarget'|'DisconnectWorkerTarget';
export const workerBackend=(config:WorkerNodeConfig)=>config.backend||'caelis';
export const workerTarget=(config:WorkerNodeConfig):WorkTarget=>({nodeId:config.id,backend:workerBackend(config),role:'worker'});
export const workerTargetKey=(config:WorkerNodeConfig)=>JSON.stringify([config.id,workerBackend(config),'worker']);
export const workerBackends=(nodes:WorkerNodeSetup['nodes'],nodeID:string)=>['caelis','codex'].filter(backend=>!nodes.some(node=>node.config.id===nodeID&&workerBackend(node.config)===backend));

export function callWorkerTarget(call:<T>(method:string,...args:unknown[])=>Promise<T>,method:WorkerTargetAction,config:WorkerNodeConfig,revision:WorkerNodeSetup['revision']) {
 return call<WorkerNodeSetup>(method,workerTarget(config),revision);
}

export function workerLocations(nodes:WorkerNodeSetup['nodes']) {
 return [...new Map(nodes.map(node=>[node.config.id,node.config])).values()];
}
export function workerDraftForLocation(nodes:WorkerNodeSetup['nodes'],id:string):WorkerNodeConfig {
 const location=workerLocations(nodes).find(config=>config.id===id);
 const backend=workerBackends(nodes,id)[0]??'caelis';
 return {id:location?.id??'',label:location?.label??'',ssh:location?.ssh??'',backend,helper:'',store:'',socket:'',workspaceRoot:''};
}
