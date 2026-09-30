import {useEffect,useMemo,useRef,useState} from 'react';
import {backend} from './desktop';
import type {NodeCatalog,NodeInfo} from './backend/contract';
import {NodeEnrollment,NodePrograms,NodeCoordinator} from './settings/runtime/NodeSetup';
import {RuntimeWorkspace} from './settings/runtime/RuntimeWorkspace';
import {createNodeRuntimeClient,createNodeSettingsClient,nodeScopeKey,type NodeSettingsClient} from './settings/runtime/nodeClient';
import {RuntimePreparation} from './RuntimePreparation';
import {useI18n} from './i18n';
import type {MessageKey} from './i18n/catalogs';
import './settings/runtime/runtime.css';

const displayName=(catalog:NodeCatalog,id:string)=>catalog.nodes.find(node=>node.id===id)?.label||id;

// This selection is renderer presentation only. It cannot activate a Runtime,
// change the Bot owner, or change the exact configured Worker target.
export function NodeRuntimeSettings({active=true,refreshKey=0,client:provided,call=backend}:{active?:boolean;refreshKey?:number;client?:NodeSettingsClient;call?:typeof backend}) {
 const {t}=useI18n();
 const owner=useMemo(()=>provided??createNodeSettingsClient(call),[provided,call]);
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
 const backendID=node?.runtimes.some(value=>value.backend===runtime)?runtime:node?.runtimes[0]?.backend||'';
 const status=node?.runtimes.find(value=>value.backend===backendID);
 const scope=nodeScopeKey(selected,backendID);
 const guard=useMemo(()=>({nodeId:selected,backend:backendID as 'codex'|'caelis',revision:catalog?.revision??''}),[selected,backendID,catalog?.revision]);
 const scoped=useMemo(()=>createNodeRuntimeClient(owner,guard),[owner,guard]);
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
 const local=node?.join==='local';
 return <section className="runtime-workspace node-runtime-settings">
  <div className="node-settings-heading"><h1>{t('runtime.runtimeAndModelsTitle')}</h1><div className="node-settings-selectors">
   <label>{t('settings.nodeSelected')}<select aria-label={t('settings.nodeSelected')} disabled={!catalog||loading} value={selected} onChange={event=>select(event.target.value,'')}>{catalog?.nodes.map(value=><option key={value.id} value={value.id}>{value.label}</option>)}</select></label>
   {node&&<label>{t('settings.workerNodeBackend')}<select aria-label={t('settings.workerNodeBackend')} value={backendID} onChange={event=>select(selected,event.target.value)}>{node.runtimes.map(value=><option key={value.backend} value={value.backend}>{value.backend==='codex'?'Codex':'Caelis'}</option>)}</select></label>}
  </div></div>
  <p className="settings-note">{t('settings.nodeViewOnly')}</p>
  {catalog&&<p className="settings-note node-owner-context">{t('settings.nodeBotOwner',{name:displayName(catalog,catalog.activeBotNodeId)||t('runtime.notConnected')})}{catalog.workerTarget&&<><br/>{t('settings.nodeWorkerTarget',{name:displayName(catalog,catalog.workerTarget.nodeId),backend:catalog.workerTarget.backend==='codex'?'Codex':'Caelis'})}</>}</p>}
  {node&&status&&<NodeStatus node={node} backendID={backendID}/>}
  {owner.pending(selected,backendID)&&<div role="status" className="runtime-callout"><p>{t('settings.nodeOperationUnknown')}</p><button disabled={loading} onClick={()=>void recover()}>{t('settings.productCheckOriginalReceipt')}</button></div>}
  {catalog&&node?<div className="node-configuration"><RuntimeWorkspace nodeManaged readOnly={!healthy||!!owner.pending(selected,backendID)} key={scope} heading={false} remote client={scoped} active={active} refreshKey={refresh} preparation={(id,onBusy)=>local?<RuntimePreparation initialRuntime={id} onBusy={onBusy} viewOnly call={call}/>:<p className="settings-note">{t('settings.nodeRemotePreparation')}</p>}/></div>:<p role="status" className="settings-note">{loading?t('runtime.loadingRuntime'):t('settings.nodeUnavailable')}</p>}
  {node&&!healthy&&<p role="status" className="settings-note">{t('settings.nodeUnavailable')}</p>}
  {node&&status&&<NodePrograms key={scope} node={node} backendID={backendID as 'codex'|'caelis'} owner={owner} call={call} onChanged={()=>setRefresh(value=>value+1)}/>}
  <NodeEnrollment catalog={catalog} call={call} onChanged={()=>setRefresh(value=>value+1)}/>
  {catalog&&<NodeCoordinator key={catalog.broker?.nodeId??''} catalog={catalog} call={call} onChanged={()=>setRefresh(value=>value+1)}/>}
  {notice&&<p role="status" className="settings-note">{t(notice)}</p>}
  {error&&<p role="alert" className="inline-error">{t(error)}</p>}
  <button className="text-action" disabled={loading} onClick={()=>setRefresh(value=>value+1)}>{t('runtime.refreshConfig')}</button>
 </section>;
}

function NodeStatus({node,backendID}:{node:NodeInfo;backendID:string}) {
 const {t}=useI18n();
 const status=node.runtimes.find(value=>value.backend===backendID)!;
 const health:MessageKey=status.health==='healthy'?'settings.nodeHealthy':status.health==='missing'?'runtime.notInstalled':status.health==='unavailable'?'runtime.notConnected':'settings.nodeStateUnknown';
 const auth:MessageKey=status.authentication==='authenticated'?'settings.nodeAuthenticated':status.authentication==='required'?'settings.workerNodeAuthSetup':'settings.nodeStateUnknown';
 return <div className="runtime-active"><span className="runtime-monogram" aria-hidden="true">{backendID==='caelis'?'C':'⌘'}</span><div><strong>{backendID==='caelis'?'Caelis':'Codex'}</strong><p>{t(health)} · {status.version||t('runtime.unknownVersion')} · {t(auth)}</p><p>{status.roles.map(role=>t(role.role==='bot'?(role.eligible?'settings.nodeBotEligible':'settings.nodeBotUnavailable'):(role.eligible?'settings.nodeWorkerEligible':'settings.nodeWorkerUnavailable'))).join(' · ')}</p>{status.roles.filter(role=>!role.eligible&&role.reason).map(role=><p key={role.role}>{role.reason}</p>)}</div></div>;
}
