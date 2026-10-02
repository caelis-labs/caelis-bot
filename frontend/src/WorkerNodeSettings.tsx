import {useEffect,useRef,useState} from 'react';
import {backend} from './desktop';
import {SettingRow} from './SettingsUI';
import {useI18n} from './i18n';
import type {MessageKey} from './i18n/catalogs';
import type {NodeCatalog,WorkerNodeSetup,WorkerNodeConfig} from './backend/contract';
import {callWorkerTarget,workerTargetKey,type WorkerTargetAction} from './workerNodeTargets';

// Machines are enrolled once. Worker setup selects that exact machine/backend;
// paths, SSH details and authentication remain in native node management.
export function WorkerNodeSettings({catalog:provided,call=backend}:{catalog?:NodeCatalog;call?:typeof backend}) {
 const {t}=useI18n();
 const [catalog,setCatalog]=useState<NodeCatalog|null>(provided??null),[setup,setSetup]=useState<WorkerNodeSetup|null>(null);
 const [nodeID,setNodeID]=useState(''),[runtime,setRuntime]=useState('codex');
 const [busy,setBusy]=useState(false),[error,setError]=useState<MessageKey|''>('');
 const pending=useRef(false),alive=useRef(true);
 useEffect(()=>{if(provided)setCatalog(provided);},[provided]);
 useEffect(()=>{
  alive.current=true;
  void Promise.all([call<WorkerNodeSetup>('WorkerNodes'),provided?Promise.resolve(provided):call<NodeCatalog>('NodeCatalog')]).then(([workers,nodes])=>{if(!Array.isArray(workers.nodes))throw new Error("Worker setup unavailable");if(alive.current){setSetup(workers);setCatalog(nodes);}}).catch(()=>{if(alive.current)setError('settings.workerNodeLoadFailed');});
  return()=>{alive.current=false;};
 },[call]);
 const machines=catalog?.nodes.filter(node=>node.join==='ssh')??[];
 const selected=machines.find(node=>node.id===nodeID)??machines[0];
 const selectedRuntime=selected?.runtimes.find(value=>value.backend===runtime)??selected?.runtimes[0];
 const eligible=!!selectedRuntime?.roles.some(role=>role.role==='worker'&&role.eligible);
 const action=async(method:WorkerTargetAction,config:WorkerNodeConfig)=>{
  if(pending.current||!setup)return;
  pending.current=true;setBusy(true);setError('');
  try{const next=await callWorkerTarget(call,method,config,setup.revision);if(alive.current)setSetup(next);}
  catch{if(alive.current)setError('settings.workerNodeActionFailed');await refresh();}
  finally{pending.current=false;if(alive.current)setBusy(false);}
 };
 const refresh=async()=>{try{const next=await call<WorkerNodeSetup>('WorkerNodes');if(alive.current)setSetup(next);}catch{/* Keep the original failure visible. */}};
 const connect=async()=>{
  if(pending.current||!setup||!selected||!selectedRuntime)return;
  pending.current=true;setBusy(true);setError('');
  const config:WorkerNodeConfig={transport:'registered-agent',id:selected.id,label:selected.label,backend:selectedRuntime.backend,ssh:'',helper:'',store:'',workspaceRoot:''};
  try{
   let next=setup;
   if(!next.nodes.some(node=>workerTargetKey(node.config)===workerTargetKey(config)))next=await call<WorkerNodeSetup>('SaveWorkerNode',config,next.revision);
   if(alive.current)setSetup(next);
   next=await callWorkerTarget(call,'ConnectWorkerTarget',config,next.revision);
   if(alive.current)setSetup(next);
  }catch{if(alive.current)setError('settings.workerNodeActionFailed');await refresh();}
  finally{pending.current=false;if(alive.current)setBusy(false);}
 };
 const editable=!!setup&&!setup.issue&&!busy;
 const stateKey=(state:string):MessageKey=>state==='ready'?'settings.workerNodeReady':state==='unavailable'?'settings.workerNodeUnavailable':'settings.workerNodeCandidate';
 return <section className="worker-node-settings"><h2>{t('settings.workerNodes')}</h2>
  <p className="settings-note">{t('settings.workerRegisteredHelp')}</p>
  {setup?.nodes.map(node=><div className="worker-node-entry" key={workerTargetKey(node.config)}>
   <SettingRow label={node.config.label} description={`${t(stateKey(node.state))} · ${node.config.backend==='codex'?'Codex':'Caelis'}`}>
    <button disabled={!editable} onClick={()=>void action(node.connected?'DisconnectWorkerTarget':'ConnectWorkerTarget',node.config)}>{t(node.connected?'settings.workerNodeDisconnect':'settings.workerNodeConnect')}</button>
   </SettingRow>
   {node.issue&&<p className="setting-feedback" role="status">{t('settings.workerNodeUnavailable')}</p>}
  </div>)}
  {machines.length>0?<div className="worker-enrolled-form">
   <SettingRow label={t('settings.workerNodeLocation')} htmlFor="worker-node-location"><select id="worker-node-location" value={selected?.id??''} disabled={!editable} onChange={event=>{setNodeID(event.target.value);setRuntime('codex');}}>{machines.map(node=><option key={node.id} value={node.id}>{node.label}</option>)}</select></SettingRow>
   <SettingRow label={t('settings.workerNodeBackend')} htmlFor="worker-node-backend"><select id="worker-node-backend" value={selectedRuntime?.backend??''} disabled={!editable} onChange={event=>setRuntime(event.target.value)}>{selected?.runtimes.map(value=><option key={value.backend} value={value.backend}>{value.backend==='codex'?'Codex':'Caelis'}</option>)}</select></SettingRow>
   <div className="settings-footer"><span className="settings-note">{!eligible&&t('settings.workerNodeAuthSetup')}</span><button disabled={!editable||!eligible||setup?.nodes.some(node=>node.connected&&node.config.id===selected?.id&&node.config.backend===selectedRuntime?.backend)} onClick={()=>void connect()}>{t('settings.workerNodeConnect')}</button></div>
  </div>:<p className="settings-note">{t('settings.workerRegisteredEmpty')}</p>}
  {setup?.issue&&<p role="alert" className="inline-error">{t('settings.workerNodeConfigUnreadable')}</p>}
  {(error||busy)&&<p className={error?'inline-error':'settings-note'} role={error?'alert':'status'}>{t(error||'settings.workerNodeWorking')}</p>}
 </section>;
}
