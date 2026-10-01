import {useEffect,useRef,useState} from 'react';
import {backend} from '../../desktop';
import type {NodeAddResult,NodeCatalog,NodeInfo,NodeJoinInstructions,NodeEditGuard,NodeInstallationState,NodeRoamingRequest,NodeRoamingPlan} from '../../backend/contract';
import {SettingRow} from '../../SettingsUI';
import {SettingsDialog} from './SettingsDialog';
import {useI18n} from '../../i18n';
import type {MessageKey} from '../../i18n/catalogs';
import type {NodeSettingsClient} from './nodeClient';
import type {NodeRoamingClient} from './roamingClient';

export function NodeEnrollment({catalog,call=backend,onChanged}:{catalog:NodeCatalog|null;call?:typeof backend;onChanged:()=>void}) {
 const {t}=useI18n();
 const [open,setOpen]=useState(false),[label,setLabel]=useState(''),[join,setJoin]=useState<'ssh'|'outgoing'>('ssh'),[ssh,setSSH]=useState('');
 const [instructions,setInstructions]=useState<NodeJoinInstructions|null>(null),[error,setError]=useState<MessageKey|''>(''),[busy,setBusy]=useState(false),[unknown,setUnknown]=useState(false);
 const pending=useRef(false),revision=useRef('');
 const submit=async()=>{
  if(pending.current||!catalog||unknown)return;pending.current=true;setBusy(true);setError('');
  try{const next=await call<NodeAddResult>('AddNode',{label:label.trim(),join,sshDestination:join==='ssh'?ssh.trim():'',expectedRevision:revision.current});setInstructions(next.joinInstructions);setOpen(false);setLabel('');setSSH('');onChanged();}
  catch{setError('settings.nodeAddUnknown');setUnknown(true);}
  finally{pending.current=false;setBusy(false);}
 };
 const check=async()=>{
  if(pending.current||!instructions)return;pending.current=true;setBusy(true);setError('');
  try{setInstructions(await call<NodeJoinInstructions>('NodeJoinInstructions',instructions.nodeId));onChanged();}
  catch{setError('settings.nodeCatalogFailed');}
  finally{pending.current=false;setBusy(false);}
 };
 return <details className="settings-disclosure"><summary>{t('settings.nodeAdd')}</summary>
  <p className="settings-note">{t('settings.nodeOutgoingHelp')}</p>
  <button disabled={!catalog} onClick={()=>{revision.current=catalog?.revision??'';setOpen(true);}}>{t('settings.nodeAdd')}</button>
  {instructions&&<div role="status"><p>{t(instructions.state==='connected'?'settings.nodeHealthy':instructions.state==='waiting'?'settings.nodeJoinWaiting':'settings.nodeUnavailable')}</p><p className="runtime-install-instructions">{instructions.instructions}</p><button disabled={busy} onClick={()=>void check()}>{t('runtime.recheck')}</button></div>}
  {!open&&error&&<p className="inline-error" role="alert">{t(error)}</p>}
  {open&&<SettingsDialog title={t('settings.nodeAdd')} busy={busy} onClose={()=>setOpen(false)}><form onSubmit={event=>{event.preventDefault();void submit();}}>
   <label>{t('settings.workerNodeLabel')}<input required value={label} maxLength={128} disabled={busy||unknown} onChange={event=>setLabel(event.target.value)}/></label>
   <label>{t('settings.nodeJoinMethod')}<select value={join} disabled={busy||unknown} onChange={event=>setJoin(event.target.value as 'ssh'|'outgoing')}><option value="ssh">{t('settings.nodeJoinSSH')}</option><option value="outgoing">{t('settings.nodeJoinOutgoing')}</option></select></label>
   {join==='ssh'&&<label>{t('settings.workerNodeSSH')}<input required value={ssh} maxLength={256} autoComplete="off" spellCheck={false} disabled={busy||unknown} onChange={event=>setSSH(event.target.value)}/></label>}
   <p className="settings-note">{t(join==='ssh'?'settings.workerNodeSSHHelp':'settings.nodeOutgoingHelp')}</p>
   {error&&<p role="alert" className="inline-error">{t(error)}</p>}
   <div className="setup-end"><button type="button" disabled={busy} onClick={()=>setOpen(false)}>{t('common.cancel')}</button><button className="primary" disabled={busy||unknown||!label.trim()||join==='ssh'&&!ssh.trim()}>{t('settings.nodeAdd')}</button></div>
  </form></SettingsDialog>}
 </details>;
}

export function NodePrograms({node,backendID,owner,call=backend,onChanged,refreshKey=0}:{refreshKey?:number|string;node:NodeInfo;backendID:'codex'|'caelis';owner:NodeSettingsClient;call?:typeof backend;onChanged:()=>void}) {
 const {t}=useI18n();
 const [installer,setInstaller]=useState(false),[versions,setVersions]=useState<string[]>([]),[managed,setManaged]=useState<NodeInstallationState|null>(null);
 const [version,setVersion]=useState(''),[confirm,setConfirm]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState<MessageKey|''>('');
 const pending=useRef(false);
 const status=node.runtimes.find(value=>value.backend===backendID)!;
 const installationGuard=useRef<NodeEditGuard|null>(null),draftSnapshot=useRef<{guard:NodeEditGuard;installation:NodeInstallationState}|null>(null);
 useEffect(()=>{let active=true;installationGuard.current=null;setInstaller(false);void owner.configuration({nodeId:node.id,backend:backendID,revision:''}).then(read=>{if(active&&read.guard.nodeId===node.id&&read.guard.backend===backendID){installationGuard.current=read.guard;setManaged(read.installation);setInstaller(read.installerAvailable&&!!read.installation);setVersions(read.reviewedVersions??[]);}}).catch(()=>{});return()=>{active=false;};},[owner,node.id,backendID,status.version,refreshKey]);
 useEffect(()=>{const guard=(event:Event)=>{if(version&&version!==(managed?.version??''))event.preventDefault();};window.addEventListener('settings-navigate',guard);return()=>window.removeEventListener('settings-navigate',guard);},[version,managed?.version]);
 const detect=async()=>{
  if(pending.current)return;pending.current=true;setBusy(true);setError('');
  try{await call<NodeInfo>('DetectNode',node.id);onChanged();}catch{setError('settings.nodeCatalogFailed');}finally{pending.current=false;setBusy(false);}
 };
 const apply=async()=>{
  if(pending.current||owner.pending(node.id,backendID)||!installer||!versions.includes(version))return;pending.current=true;setBusy(true);setError('');
  try{
   // Installation obtains its native guard from the target read. A catalog
   // revision is not a substitute for that target's configuration revision.
   const snapshot=draftSnapshot.current;
   if(!snapshot)throw new Error('Node inspection is required before changing the program.');
   await owner.change(snapshot.guard,{change:null,installation:{action:snapshot.installation.installed?'update':'install',version:version.trim(),expectedVersion:snapshot.installation.installed?snapshot.installation.version:''}});
   setConfirm(false);setVersion('');draftSnapshot.current=null;onChanged();
  }catch{setError('settings.nodeProgramFailed');}
  finally{pending.current=false;setBusy(false);}
 };
 return <details className="settings-disclosure"><summary>{t('settings.productTargetPrograms')}</summary>
  <SettingRow label={t('settings.nodeDetectedProgram')}><span>{status.version||t(status.health==='missing'?'runtime.notInstalled':'runtime.unknownVersion')}</span><button disabled={busy} onClick={()=>void detect()}>{t('runtime.recheck')}</button></SettingRow>
  <SettingRow label={t('settings.nodeManagedProgram')}><span>{managed?managed.installed?managed.version:t('runtime.notInstalled'):t('settings.nodeStateUnknown')}</span></SettingRow>
  {managed&&!managed.installed&&!!status.version&&<p className="settings-note">{t('settings.nodeManagedCopyHelp')}</p>}
  <p className="settings-note">{t(installer?'settings.nodeReviewedVersionHelp':'settings.nodeRemotePreparation')}</p>
  {version&&<button disabled={busy} className="text-action" onClick={()=>{setVersion('');draftSnapshot.current=null;}}>{t('common.cancel')}</button>}
  <SettingRow label={t('settings.productReviewedVersion')} htmlFor="node-program-version"><select id="node-program-version" value={version} onChange={event=>{setVersion(event.target.value);draftSnapshot.current=installationGuard.current&&managed?{guard:{...installationGuard.current},installation:{...managed}}:null;}} disabled={!installer||busy||!!owner.pending(node.id,backendID)}><option value="" disabled>{t('settings.productSelectVersion')}</option>{versions.map(value=><option key={value} value={value}>{value}</option>)}</select><button disabled={!installer||busy||!!owner.pending(node.id,backendID)||!versions.includes(version)||managed?.installed&&version===managed.version} onClick={()=>setConfirm(true)}>{t(managed?.installed?'runtime.update':'settings.nodeInstallManagedCopy',{name:backendID==='codex'?'Codex':'Caelis'})}</button></SettingRow>
  {confirm&&<SettingsDialog title={t('runtime.confirm')} busy={busy} onClose={()=>setConfirm(false)}><p>{t('settings.productConfirmInstall',{name:backendID==='codex'?'Codex':'Caelis',version,target:node.label})}</p>{error&&<p role="alert" className="inline-error">{t(error)}</p>}<div className="setup-end"><button disabled={busy} onClick={()=>setConfirm(false)}>{t('common.cancel')}</button><button disabled={busy||!!owner.pending(node.id,backendID)} onClick={()=>void apply()}>{t('runtime.confirm')}</button></div></SettingsDialog>}
  {!confirm&&error&&<p role="alert" className="inline-error">{t(error)}</p>}
 </details>;
}

export function NodeCoordinator({catalog,roaming,onChanged,call=backend,refreshKey=0}:{catalog:NodeCatalog;roaming:NodeRoamingClient;call?:typeof backend;onChanged:()=>void;refreshKey?:number}) {
 const {t}=useI18n();
 const committed=catalog.broker?.nodeId??'';
 const [selected,setSelected]=useState(committed),[busy,setBusy]=useState(false),[error,setError]=useState<MessageKey|''>('');
 const [confirmation,setConfirmation]=useState<{enable:boolean;revision:string;coordinator:string;reviewed?:{request:NodeRoamingRequest;plan:NodeRoamingPlan}}|null>(null);
 const [,render]=useState(0),[attempted,setAttempted]=useState(false);
 const pending=useRef(false),revision=useRef(catalog.revision),draft=useRef(false),current=useRef(catalog);current.current=catalog;
 const state=roaming.snapshot(),dirty=selected!==committed;
 const coordinatorLocked=()=>{const current=roaming.snapshot();return !!current.value?.enabled||!!current.pending||['enabling','disabling','unknown'].includes(current.value?.state??'');};
 const locked=coordinatorLocked();
 useEffect(()=>roaming.subscribe(()=>render(value=>value+1)),[roaming]);
 useEffect(()=>{void roaming.read();},[roaming,catalog.revision,refreshKey]);
 useEffect(()=>{if(!draft.current){setSelected(committed);revision.current=catalog.revision;}},[committed,catalog.revision]);
 useEffect(()=>{const guard=(event:Event)=>{if(dirty||busy)event.preventDefault();};window.addEventListener('settings-navigate',guard);return()=>window.removeEventListener('settings-navigate',guard);},[dirty,busy]);
 const save=async()=>{
  if(pending.current||roaming.snapshot().busy||coordinatorLocked()||!dirty)return;pending.current=true;setBusy(true);setError('');
  try{await call<NodeCatalog>('SetNodeCoordinator',{nodeId:selected,expectedRevision:revision.current});draft.current=false;onChanged();}catch{setError('settings.nodeCoordinatorFailed');}finally{pending.current=false;setBusy(false);}
 };
 const open=async()=>{
  if(!window.dispatchEvent(new Event('settings-navigate',{cancelable:true}))){setError('settings.nodeFinishEditing');return;}
  if(pending.current)return;pending.current=true;setBusy(true);setError('');
  try{
   const enable=!state.value?.enabled;const reviewed=enable?await roaming.prepare(catalog.revision):undefined;
   if(current.current.revision!==catalog.revision||current.current.broker?.nodeId!==committed||reviewed&&reviewed.plan.coordinatorNodeId!==committed){setError('settings.nodeRoamingChanged');return;}
   setAttempted(false);setConfirmation({enable,revision:catalog.revision,coordinator:committed,reviewed});
  }catch{setError('settings.nodeRoamingPlanFailed');}
  finally{pending.current=false;setBusy(false);}
 };
 const apply=async()=>{
  if(pending.current||attempted||!confirmation||confirmation.revision!==catalog.revision||confirmation.coordinator!==committed)return;
  pending.current=true;setAttempted(true);setBusy(true);setError('');
  try{if(await roaming.change(confirmation.enable,confirmation.revision,confirmation.reviewed)){setConfirmation(null);onChanged();}else setError('settings.nodeRoamingFailed');}
  finally{pending.current=false;setBusy(false);}
 };
 const phase=state.value?.state;
 const changing=phase==='enabling'||phase==='disabling'||phase==='unknown';
 const enabled=!!state.value?.enabled;
 const blocked=busy||state.busy||!!state.pending||state.failed||!state.value?.available||dirty||changing||!enabled&&!committed;
 const ready=phase==='ready'&&enabled&&catalog.broker?.reachable&&catalog.broker.automaticRoaming&&state.value?.activeBotNodeId===catalog.activeBotNodeId&&catalog.nodes.some(node=>node.id===state.value?.activeBotNodeId&&node.runtimes.some(runtime=>runtime.roles.some(role=>role.role==='bot'&&role.eligible)));
 const status:MessageKey=state.pending||phase==='unknown'?'settings.nodeRoamingUnknown':ready?'settings.nodeRoamingReady':phase==='enabling'?'settings.nodeRoamingPreparing':phase==='waiting'||phase==='ready'?'settings.nodeRoamingWaiting':phase==='disabling'?'settings.nodeRoamingStopping':phase==='disabled'?'settings.nodeRoamingDisabled':'settings.nodeRoamingControllerUnavailable';
 return <details className="settings-disclosure"><summary>{t('settings.nodeAlwaysOn')}</summary>
  <p className="settings-note">{t('settings.nodeCoordinatorHelp')}</p>
  <SettingRow label={t('settings.nodeAlwaysOn')} htmlFor="node-coordinator"><select id="node-coordinator" value={selected} disabled={busy||state.busy||locked} onChange={event=>{if(pending.current||roaming.snapshot().busy||coordinatorLocked())return;revision.current=catalog.revision;draft.current=true;setSelected(event.target.value);}}><option value="">{t('settings.nodeCoordinatorNone')}</option>{catalog.nodes.map(node=><option key={node.id} value={node.id}>{node.label}</option>)}</select><button disabled={busy||state.busy||locked||!dirty} onClick={()=>void save()}>{t('common.save')}</button>{dirty&&<button disabled={busy} onClick={()=>{draft.current=false;setSelected(committed);revision.current=catalog.revision;setError('');}}>{t('common.cancel')}</button>}</SettingRow>
  {locked&&<p className="settings-note">{t('settings.nodeCoordinatorLocked')}</p>}
  <SettingRow label={t('settings.nodeRoamingLabel')}><button disabled={blocked} onClick={()=>void open()}>{t(enabled?'settings.nodeRoamingDisable':'settings.nodeRoamingEnable')}</button></SettingRow>
  <p role="status" className="settings-note">{t(status)}</p>
  {(state.pending||state.failed||changing||phase==='waiting')&&<button disabled={busy||state.busy} onClick={()=>{setError('');void roaming.read();}}>{t(state.pending?'settings.productCheckOriginalReceipt':'runtime.recheck')}</button>}
  {state.failed&&<p role="alert" className="inline-error">{t('settings.nodeRoamingReadFailed')}</p>}
  {error&&!confirmation&&<p role="alert" className="inline-error">{t(error)}</p>}
  {confirmation&&<SettingsDialog title={t(confirmation.enable?'settings.nodeRoamingEnable':'settings.nodeRoamingDisable')} busy={busy||state.busy} onClose={()=>setConfirmation(null)}><p>{t(confirmation.enable?'settings.nodeRoamingEnableConfirm':'settings.nodeRoamingDisableConfirm')}</p>{confirmation.reviewed&&<><p className="settings-note">{t('settings.nodeRoamingPlanHelp')}</p><ul>{confirmation.reviewed.plan.actions.map((action,index)=><li key={`${action.nodeId}:${index}`}>{t(roamingActionLabel(action.action),{name:catalog.nodes.find(node=>node.id===action.nodeId)?.label||action.label})}</li>)}</ul></>}{error&&<p role="alert" className="inline-error">{t(error)}</p>}{confirmation.revision!==catalog.revision&&<p role="alert" className="inline-error">{t('settings.nodeRoamingChanged')}</p>}<div className="setup-end"><button disabled={busy||state.busy} onClick={()=>setConfirmation(null)}>{t('common.cancel')}</button><button disabled={blocked||attempted||confirmation.revision!==catalog.revision||confirmation.coordinator!==committed} onClick={()=>void apply()}>{t('runtime.confirm')}</button></div></SettingsDialog>}
 </details>;
}

function roamingActionLabel(action:string):MessageKey {
 return action==='prepare-coordinator'?'settings.nodeRoamingPrepareCoordinator':action==='prepare-node'?'settings.nodeRoamingPrepareNode':action==='connect-outgoing'?'settings.nodeRoamingConnectOutgoing':action==='stop-source'?'settings.nodeRoamingStopSource':action==='start-bot'?'settings.nodeRoamingStartBot':'settings.nodeRoamingPrepareService';
}
