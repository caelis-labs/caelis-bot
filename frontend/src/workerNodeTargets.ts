import type {WorkTarget,WorkerNodeConfig,WorkerNodeSetup} from './backend/contract';

export type WorkerTargetAction='ProbeWorkerTarget'|'ConnectWorkerTarget'|'DisconnectWorkerTarget';
export const workerBackend=(config:WorkerNodeConfig)=>config.backend||'caelis';
export const workerTarget=(config:WorkerNodeConfig):WorkTarget=>({nodeId:config.id,backend:workerBackend(config),role:'worker'});
export const workerTargetKey=(config:WorkerNodeConfig)=>JSON.stringify([config.id,workerBackend(config),'worker']);
export function callWorkerTarget(call:<T>(method:string,...args:unknown[])=>Promise<T>,method:WorkerTargetAction,config:WorkerNodeConfig,revision:WorkerNodeSetup['revision']) {
 return call<WorkerNodeSetup>(method,workerTarget(config),revision);
}
