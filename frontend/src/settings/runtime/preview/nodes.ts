import type {NodeCatalog, NotebookSyncSettings, NotebookSyncState, WorkerNodeSetup, WorkerNodeConfig} from '../../../backend/contract';
import type {RuntimeSettingsClient} from '../types';
import type {backend} from '../../../desktop';

// Visual fixtures only: no SSH, filesystem, process, auth or model calls.
export function createNodePreview(client:RuntimeSettingsClient) {
 const runtimes=(['caelis','codex'] as const).map(backend=>({backend,version:'1.0',health:'healthy' as const,authentication:'authenticated' as const,roles:[{role:'worker' as const,eligible:true,reason:''},{role:'bot' as const,eligible:true,reason:''}]}));
 const catalog:NodeCatalog={revision:'preview-1',selectedNodeId:'local',activeBotNodeId:'local',pairedRuntime:null,workerTarget:null,broker:null,pendingOperations:[],nodes:[{id:'local',label:'This Mac',os:'darwin',join:'local',runtimes},{id:'vps',label:'Development VPS · Singapore',os:'linux',join:'ssh',runtimes}]};
 let workers:WorkerNodeSetup={revision:1,nodes:[],issue:''};
 let preferences:NotebookSyncSettings={enabled:true,sourceNodeId:'local',intervalMinutes:5,targets:[{nodeId:'vps',backend:'caelis'}]};
 const state:NotebookSyncState={sourceNodeId:'local',targets:[{nodeId:'vps',phase:'ready',lastSuccess:'2026-10-02T01:00:00Z',error:''}]};
 const call:typeof backend=async<T,>(method:string,...args:unknown[]):Promise<T>=>{
  if(method==='NodeCatalog')return structuredClone(catalog) as T;
  if(method==='WorkerNodes')return structuredClone(workers) as T;
  if(method==='SaveWorkerNode'){workers={...workers,revision:workers.revision+1,nodes:[...workers.nodes,{config:args[0] as WorkerNodeConfig,state:'candidate',connected:false,issue:'',facts:{os:'linux',arch:'arm64',version:'preview'}}]};return structuredClone(workers) as T;}
  if(method==='ConnectWorkerTarget'||method==='DisconnectWorkerTarget'){workers={...workers,revision:workers.revision+1,nodes:workers.nodes.map(node=>({...node,connected:method==='ConnectWorkerTarget',state:method==='ConnectWorkerTarget'?'ready':'candidate'}))};return structuredClone(workers) as T;}
  if(method==='NotebookSyncSettings')return structuredClone(preferences) as T;
  if(method==='SaveNotebookSyncSettings'){preferences=structuredClone(args[0]) as NotebookSyncSettings;return structuredClone(preferences) as T;}
  if(method==='NotebookSyncState')return structuredClone(state) as T;
  if(method==='NodeRuntimeConfiguration'){const view=await client.read();return {guard:{nodeId:args[0],backend:args[1],revision:'preview-1'},configurationAvailable:true,installerAvailable:false,installation:null,reviewedVersions:[],conversation:view.conversation,worker:view.work,configuration:{revision:view.revision,main:view.main,models:view.models,connections:view.connections,team:view.team,oauthAvailable:true}} as T;}
  throw new Error('Preview does not perform this native action');
 };
 return {catalog,call};
}
