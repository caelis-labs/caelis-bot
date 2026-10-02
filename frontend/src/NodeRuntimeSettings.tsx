import {useEffect,useMemo,useRef,useState} from 'react';
import {backend} from './desktop';
import type {NodeCatalog,NodeInfo} from './backend/contract';
import {NodeEnrollment,NodePrograms} from './settings/runtime/NodeSetup';
import {BatchSettings} from './settings/runtime/BatchSettings';
import {NotebookSyncSettings} from './settings/runtime/NotebookSyncSettings';
import {RuntimeWorkspace} from './settings/runtime/RuntimeWorkspace';
import {createNodeRuntimeClient,createNodeSettingsClient,nodeScopeKey,type NodeSettingsClient} from './settings/runtime/nodeClient';
import {RuntimePreparation} from './RuntimePreparation';
import {WorkerNodeSettings} from './WorkerNodeSettings';
import {RemoteRuntimeSettings} from './RemoteRuntimeSettings';
import {createPairedRuntimeInvoker} from './settings/runtime/pairedClient';
import {useI18n} from './i18n';
import type {MessageKey} from './i18n/catalogs';
import './settings/runtime/runtime.css';

const displayName=(catalog:NodeCatalog,id:string)=>catalog.nodes.find(node=>node.id===id)?.label||id;

// This selection is renderer presentation only. It cannot activate a Runtime,
// change the Bot owner, or change the exact configured Worker target.
export function NodeRuntimeSettings({active=true,refreshKey=0,client:provided,call=backend}:{active?:boolean;refreshKey?:number;client?:NodeSettingsClient;call?:typeof backend}) {
 const {t}=useI18n();
 const translations=useRef(t);translations.current=t;
 const owner=useMemo(()=>provided??createNodeSettingsClient(call,key=>translations.current(key)),[provided,call]);
 const [catalog,setCatalog]=useState<NodeCatalog|null>(null),[selected,setSelected]=useState(''),[runtime,setRuntime]=useState('');
 const [error,setError]=useState<MessageKey|''>(''),[loading,setLoading]=useState(false),[refresh,setRefresh]=useState(0),[notice,setNotice]=useState<MessageKey|''>('');
 const generation=useRef(0),pending=useRef(false);
 const [,setOperations]=useState(0);
 useEffect(()=>owner.subscribe(()=>setOperations(value=>value+1)),[owner]);
 const load=async()=>{
  const epoch=++generation.current;setLoading(true);setError('');
  try{const next=await owner.catalog();if(epoch===generation.current){setCatalog(next);setSelected(value=>value||next.selectedNodeId||next.nodes.find(node=>node.join==='local')?.id||next.nodes[0]?.id||'');}}
  catch{if(epoch===generation.current)setError('settings.nodeCatalogFailed');}
  finally{if(epoch===generation.current)setLoading(false);}
 };
 useEffect(()=>{if(active)void load();return()=>{generation.current++;};},[owner,active,refreshKey,refresh]);
 const node=catalog?.nodes.find(value=>value.id===selected);
 const paired=catalog?.pairedRuntime;
 const pairedSelected=!!paired&&paired.nodeId===selected;
 const pairedCall=useMemo(()=>createPairedRuntimeInvoker(call,paired?.binding??'',()=>translations.current('settings.productManagementUnknown')),[call,paired?.binding]);
 const backendID=node?.runtimes.some(value=>value.backend===runtime)?runtime:node?.runtimes[0]?.backend||'';
 const status=node?.runtimes.find(value=>value.backend===backendID);
 const scope=nodeScopeKey(selected,backendID);
 const guard=useMemo(()=>({nodeId:selected,backend:backendID as 'codex'|'caelis',revision:catalog?.revision??''}),[selected,backendID,catalog?.revision]);
 const defaultLocal=node?.join==='local'&&catalog?.activeBotNodeId===selected;
 const scoped=useMemo(()=>createNodeRuntimeClient(owner,guard,{defaultLocal}),[owner,guard,defaultLocal]);
 const select=(nodeId:string,backendID:string)=>{
  if(!window.dispatchEvent(new Event('settings-navigate',{cancelable:true}))){setNotice('settings.nodeFinishEditing');return;}
  generation.current++;setLoading(false);setSelected(nodeId);setRuntime(backendID);setNotice('');
 };
 const recover=async()=>{
  if(pending.current)return;pending.current=true;setLoading(true);setError('');
  const epoch=generation.current;
  try{await owner.reconcile(selected,backendID);if(epoch===generation.current)setRefresh(value=>value+1);}
  catch{if(epoch===generation.current)setError('settings.nodeOperationUnknown');}
  finally{pending.current=false;if(epoch===generation.current)setLoading(false);}
 };
 const healthy=status?.health==='healthy';
 const connectionState=error?'unknown':healthy&&status?.authentication==='authenticated'?'connected':status?.health==='unknown'||status?.authentication==='unknown'?'unknown':'disconnected';
 const local=node?.join==='local';
 return <section className="runtime-workspace node-runtime-settings">
  <div className="node-settings-heading"><h1>{t('runtime.runtimeAndModelsTitle')}</h1><div className="node-settings-selectors">
   {catalog&&catalog.nodes.length>1&&<label>{t('settings.nodeSelected')}<select aria-label={t('settings.nodeSelected')} disabled={!catalog||loading} value={selected} onChange={event=>select(event.target.value,'')}>{catalog?.nodes.map(value=><option key={value.id} value={value.id}>{value.label}</option>)}</select></label>}
   {node&&node.runtimes.length>0&&<label>{t('settings.workerNodeBackend')}<select aria-label={t('settings.workerNodeBackend')} value={backendID} onChange={event=>select(selected,event.target.value)}>{node.runtimes.map(value=><option key={value.backend} value={value.backend}>{value.backend==='codex'?'Codex':'Caelis'}</option>)}</select></label>}
  </div></div>
  {notice&&<p role="status" className="settings-note">{t(notice)}</p>}
  {catalog&&catalog.nodes.length>1&&<p className="settings-note">{t('settings.nodeViewOnly')}</p>}
  {catalog&&catalog.nodes.length>1&&<p className="settings-note node-owner-context">{t('settings.nodeBotOwner',{name:displayName(catalog,catalog.activeBotNodeId)||t('runtime.notConnected')})}{catalog.workerTarget&&<><br/>{t('settings.nodeWorkerTarget',{name:displayName(catalog,catalog.workerTarget.nodeId),backend:catalog.workerTarget.backend==='codex'?'Codex':'Caelis'})}</>}</p>}
  {node&&status&&(!healthy||!defaultLocal&&status.roles.some(role=>!role.eligible))&&<NodeStatus node={node} backendID={backendID}/>}
  {owner.pending(selected,backendID)&&<div role="status" className="runtime-callout"><p>{t('settings.nodeOperationUnknown')}</p><button disabled={loading} onClick={()=>void recover()}>{t('settings.productCheckOriginalReceipt')}</button></div>}
  {pairedSelected?<><p className="settings-note">{t('settings.nodePairedProductScope')}</p><RemoteRuntimeSettings key={`${selected}:${paired.binding}`} call={pairedCall} heading={false} active={active} refreshKey={refresh}/></>:catalog&&node?<div className="node-configuration"><RuntimeWorkspace batch={(view,connection,onClose)=><BatchSettings catalog={catalog} owner={owner} sourceId={selected} view={view} connection={connection} onClose={onClose} onSelect={id=>{setTimeout(()=>select(id,backendID),0);}}/>} nodeManaged connectionState={connectionState} connectionReadOnly={!!error||!!owner.pending(selected,backendID)||status?.health==='missing'} readOnly={!!error||!healthy||!!owner.pending(selected,backendID)} key={scope} heading={false} remote={!defaultLocal||!!owner.pending(selected,backendID)} client={scoped} active={active} refreshKey={refresh} preparation={(id,onBusy,defaultLocalView)=>local&&defaultLocal&&defaultLocalView?<RuntimePreparation initialRuntime={id} onBusy={onBusy} call={call}/>:<p className="settings-note">{t(local?'settings.nodeScopedPreparation':'settings.nodeRemotePreparation')}</p>}/></div>:<p role="status" className="settings-note">{loading?t('runtime.loadingRuntime'):t('settings.nodeUnavailable')}</p>}
  {!pairedSelected&&node&&!healthy&&<p role="status" className="settings-note">{t('settings.nodeUnavailable')}</p>}
  {!pairedSelected&&node&&status&&<NodePrograms refreshKey={`${catalog?.revision}:${refresh}:${refreshKey}`} key={scope} node={node} backendID={backendID as 'codex'|'caelis'} owner={owner} call={call} onChanged={()=>setRefresh(value=>value+1)}/>}
  <NodeEnrollment catalog={catalog} call={call} onChanged={()=>setRefresh(value=>value+1)}/>
  {catalog&&<WorkerNodeSettings catalog={catalog} call={call}/>}
  {catalog&&<NotebookSyncSettings catalog={catalog} call={call} active={active}/>}
  {error&&<p role="alert" className="inline-error">{t(error)}</p>}
  <button className="text-action" disabled={loading} onClick={()=>setRefresh(value=>value+1)}>{t('runtime.refreshConfig')}</button>
 </section>;
}

function NodeStatus({node,backendID}:{node:NodeInfo;backendID:string}) {
 const {t}=useI18n();
 const status=node.runtimes.find(value=>value.backend===backendID)!;
 const health:MessageKey=status.health==='healthy'?'settings.nodeHealthy':status.health==='missing'?'runtime.notInstalled':status.health==='unavailable'?'runtime.notConnected':'settings.nodeStateUnknown';
 const auth:MessageKey=status.authentication==='authenticated'?'settings.nodeAuthenticated':status.authentication==='required'?'settings.workerNodeAuthSetup':'settings.nodeStateUnknown';
 const roles=new Map<string,string[]>();
 for(const role of status.roles){
  const reason=role.eligible?'':role.reason;
  const labels=roles.get(reason)??[];
  labels.push(t(role.role==='bot'?(role.eligible?'settings.nodeBotEligible':'settings.nodeBotUnavailable'):(role.eligible?'settings.nodeWorkerEligible':'settings.nodeWorkerUnavailable')));
  roles.set(reason,labels);
 }
 const reasonKey=(reason:string):MessageKey=>reason==='runtime-owner-unavailable'?'settings.nodeOwnerUnavailableReason':reason==='shared-runtime-not-fenceable'?'settings.nodeSharedRuntimeReason':reason==='windows-process-ownership-unsupported'?'settings.nodeWindowsWorkerOnlyReason':'settings.nodeRoleUnavailableReason';
 return <div className="runtime-active"><span className="runtime-monogram" aria-hidden="true">{backendID==='caelis'?'C':'⌘'}</span><div><strong>{backendID==='caelis'?'Caelis':'Codex'}</strong><p>{t(health)} · {status.version||t('runtime.unknownVersion')} · {t(auth)}</p>{[...roles].map(([reason,labels])=><p key={reason}>{labels.join(' · ')}{reason&&<> · {t(reasonKey(reason))}</>}</p>)}</div></div>;
}
