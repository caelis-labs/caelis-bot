import {useEffect, useRef, useState} from 'react';
import {backend} from './desktop';
import {SettingGroup, SettingRow} from './SettingsUI';
import {useI18n} from './i18n';
import type {MessageKey} from './i18n/catalogs';
import type {WorkerNodeConfig, WorkerNodeSetup} from './backend/contract';

const emptyNode:WorkerNodeConfig={backend:'caelis',id:'',label:'',ssh:'',helper:'',store:'',workspaceRoot:''};
type NodeAction='ProbeWorkerNode'|'ConnectWorkerNode'|'DisconnectWorkerNode';

export function WorkerNodeSettings({call=backend}:{call?:typeof backend}) {
 const {t}=useI18n();
 const [setup,setSetup]=useState<WorkerNodeSetup|null>(null);
 const [draft,setDraft]=useState<WorkerNodeConfig>(emptyNode);
 const [busy,setBusy]=useState(false),[error,setError]=useState<MessageKey|''>('');
 const pending=useRef(false),active=useRef(true);
 useEffect(()=>{
  active.current=true;
  void call<WorkerNodeSetup>('WorkerNodes').then(value=>{if(active.current)setSetup(value);}).catch(()=>{if(active.current)setError('settings.workerNodeLoadFailed');});
  return()=>{active.current=false;};
 },[call]);
 const refresh=async()=>{const next=await call<WorkerNodeSetup>('WorkerNodes');if(active.current)setSetup(next);};
 const perform=async(method:NodeAction,id:string)=>{
  if(pending.current||!setup)return;
  pending.current=true;setBusy(true);setError('');
  try{const next=await call<WorkerNodeSetup>(method,id,setup.revision);if(active.current)setSetup(next);}
  catch{if(active.current)setError('settings.workerNodeActionFailed');await refresh().catch(()=>{});}
  finally{pending.current=false;if(active.current)setBusy(false);}
 };
 const save=async()=>{
  if(pending.current||!setup)return;
  pending.current=true;setBusy(true);setError('');
  const config={...draft,label:draft.label.trim(),ssh:draft.ssh.trim(),helper:draft.helper.trim(),store:draft.store.trim(),workspaceRoot:draft.workspaceRoot.trim()};
  try{const next=await call<WorkerNodeSetup>('SaveWorkerNode',config,setup.revision);if(active.current){setSetup(next);setDraft(emptyNode);}}
  catch{if(active.current)setError('settings.workerNodeSaveFailed');await refresh().catch(()=>{});}
  finally{pending.current=false;if(active.current)setBusy(false);}
 };
 const editable=!!setup&&!setup.issue&&!busy;
 const stateKey=(state:string):MessageKey=>state==='ready'?'settings.workerNodeReady':state==='unavailable'?'settings.workerNodeUnavailable':'settings.workerNodeCandidate';
 const issueKey=(issue:string):MessageKey=>issue==='model_setup_required'?'settings.workerNodeModelSetup':issue==='authentication_required'?'settings.workerNodeAuthSetup':issue==='detached'?'settings.workerNodeDetached':'settings.workerNodeNeedsHost';
 return <SettingGroup title={t('settings.workerNodes')}>
  <p className="settings-note">{t('settings.workerNodesHelp')}</p>
  {setup?.nodes.map(node=><div className="worker-node-entry" key={node.config.id}>
   <SettingRow label={node.config.label} description={<><span>{t(stateKey(node.state))} · Caelis</span>{node.facts.os&&<span className="worker-node-facts">{[node.facts.os,node.facts.arch,node.facts.version].filter(Boolean).join(' · ')}</span>}</>}>
    <button disabled={!editable||node.connected} onClick={()=>void perform('ProbeWorkerNode',node.config.id)}>{t('settings.workerNodeCheck')}</button>
    <button disabled={!editable} onClick={()=>void perform(node.connected?'DisconnectWorkerNode':'ConnectWorkerNode',node.config.id)}>{t(node.connected?'settings.workerNodeDisconnect':'settings.workerNodeConnect')}</button>
   </SettingRow>
   {node.issue&&<p className="settings-note" role="status">{t(issueKey(node.issue))}</p>}
  </div>)}
  <details className="worker-node-form">
   <summary>{t('settings.workerNodeAdd')}</summary>
   <p className="settings-note">{t('settings.workerNodePreparation')}</p>
   <SettingRow label={t('settings.workerNodeLabel')} htmlFor="worker-node-label"><input id="worker-node-label" value={draft.label} disabled={!editable} maxLength={128} onChange={e=>setDraft({...draft,label:e.target.value})}/></SettingRow>
   <SettingRow label={t('settings.workerNodeSSH')} htmlFor="worker-node-ssh" description={t('settings.workerNodeSSHHelp')}><input id="worker-node-ssh" value={draft.ssh} disabled={!editable} autoComplete="off" spellCheck={false} maxLength={256} onChange={e=>setDraft({...draft,ssh:e.target.value})}/></SettingRow>
   <SettingRow label={t('settings.workerNodeWorkspace')} htmlFor="worker-node-workspace" description={t('settings.workerNodeWorkspaceHelp')}><input id="worker-node-workspace" value={draft.workspaceRoot} disabled={!editable} autoComplete="off" spellCheck={false} onChange={e=>setDraft({...draft,workspaceRoot:e.target.value})}/></SettingRow>
   <details className="worker-node-advanced"><summary>{t('settings.workerNodeAdvanced')}</summary>
    <SettingRow label={t('settings.workerNodeStore')} htmlFor="worker-node-store" description={t('settings.workerNodeStoreHelp')}><input id="worker-node-store" value={draft.store} disabled={!editable} autoComplete="off" spellCheck={false} onChange={e=>setDraft({...draft,store:e.target.value})}/></SettingRow>
    <SettingRow label={t('settings.workerNodeHelper')} htmlFor="worker-node-helper" description={t('settings.workerNodeHelperHelp')}><input id="worker-node-helper" value={draft.helper} disabled={!editable} autoComplete="off" spellCheck={false} onChange={e=>setDraft({...draft,helper:e.target.value})}/></SettingRow>
   </details>
   <button disabled={!editable||!draft.label.trim()||!draft.ssh.trim()||!draft.workspaceRoot.trim()} onClick={()=>void save()}>{t('settings.workerNodeSave')}</button>
  </details>
  {setup?.issue&&<p className="inline-error" role="alert">{t('settings.workerNodeConfigUnreadable')}</p>}
  {(error||busy)&&<p className={error?'inline-error':'settings-note'} role={error?'alert':'status'}>{t(error||'settings.workerNodeWorking')}</p>}
 </SettingGroup>;
}
