import {useEffect,useRef,useState} from 'react';
import {backend} from '../../desktop';
import type {NodeAddResult,NodeCatalog,NodeInfo,NodeJoinInstructions,NodeEditGuard} from '../../backend/contract';
import {SettingRow} from '../../SettingsUI';
import {SettingsDialog} from './SettingsDialog';
import {useI18n} from '../../i18n';
import type {MessageKey} from '../../i18n/catalogs';
import type {NodeSettingsClient} from './nodeClient';

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

export function NodePrograms({node,backendID,owner,call=backend,onChanged}:{node:NodeInfo;backendID:'codex'|'caelis';owner:NodeSettingsClient;call?:typeof backend;onChanged:()=>void}) {
 const {t}=useI18n();
 const [version,setVersion]=useState(''),[confirm,setConfirm]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState<MessageKey|''>('');
 const pending=useRef(false);
 const status=node.runtimes.find(value=>value.backend===backendID)!;
 const installationGuard=useRef<NodeEditGuard|null>(null);
 useEffect(()=>{let active=true;installationGuard.current=null;void owner.configuration({nodeId:node.id,backend:backendID,revision:''}).then(read=>{if(active&&read.guard.nodeId===node.id&&read.guard.backend===backendID)installationGuard.current=read.guard;}).catch(()=>{});return()=>{active=false;};},[owner,node.id,backendID,status.version]);
 useEffect(()=>{const guard=(event:Event)=>{if(version&&version!==status.version)event.preventDefault();};window.addEventListener('settings-navigate',guard);return()=>window.removeEventListener('settings-navigate',guard);},[version,status.version]);
 const detect=async()=>{
  if(pending.current)return;pending.current=true;setBusy(true);setError('');
  try{await call<NodeInfo>('DetectNode',node.id);onChanged();}catch{setError('settings.nodeCatalogFailed');}finally{pending.current=false;setBusy(false);}
 };
 const apply=async()=>{
  if(pending.current||owner.pending(node.id,backendID)||!version.trim())return;pending.current=true;setBusy(true);setError('');
  try{
   // Installation obtains its native guard from the target read. A catalog
   // revision is not a substitute for that target's configuration revision.
   if(!installationGuard.current)throw new Error('Node inspection is required before changing the program.');
   await owner.change(installationGuard.current,{change:null,installation:{action:status.health==='missing'?'install':'update',version:version.trim(),expectedVersion:status.version}});
   setConfirm(false);setVersion('');onChanged();
  }catch{setError('settings.nodeProgramFailed');}
  finally{pending.current=false;setBusy(false);}
 };
 return <details className="settings-disclosure"><summary>{t('settings.productTargetPrograms')}</summary>
  <SettingRow label={t('runtime.installed')}><span>{status.version||t(status.health==='missing'?'runtime.notInstalled':'runtime.unknownVersion')}</span><button disabled={busy} onClick={()=>void detect()}>{t('runtime.recheck')}</button></SettingRow>
  <p className="settings-note">{t('settings.nodeReviewedVersionHelp')}</p>
  {version&&<button disabled={busy} className="text-action" onClick={()=>setVersion('')}>{t('common.cancel')}</button>}
  <SettingRow label={t('settings.productReviewedVersion')} htmlFor="node-program-version"><input id="node-program-version" autoComplete="off" spellCheck={false} value={version} onChange={event=>setVersion(event.target.value)} disabled={busy||!!owner.pending(node.id,backendID)}/><button disabled={busy||!!owner.pending(node.id,backendID)||!version.trim()||version===status.version} onClick={()=>setConfirm(true)}>{t(status.health==='missing'?'runtime.installRuntime':'runtime.update',{name:backendID==='codex'?'Codex':'Caelis'})}</button></SettingRow>
  {confirm&&<SettingsDialog title={t('runtime.confirm')} busy={busy} onClose={()=>setConfirm(false)}><p>{t('settings.productConfirmInstall',{name:backendID==='codex'?'Codex':'Caelis',version,target:node.label})}</p>{error&&<p role="alert" className="inline-error">{t(error)}</p>}<div className="setup-end"><button disabled={busy} onClick={()=>setConfirm(false)}>{t('common.cancel')}</button><button disabled={busy||!!owner.pending(node.id,backendID)} onClick={()=>void apply()}>{t('runtime.confirm')}</button></div></SettingsDialog>}
  {!confirm&&error&&<p role="alert" className="inline-error">{t(error)}</p>}
 </details>;
}

export function NodeCoordinator({catalog,call=backend,onChanged}:{catalog:NodeCatalog;call?:typeof backend;onChanged:()=>void}) {
 const {t}=useI18n();
 const [selected,setSelected]=useState(catalog.broker?.nodeId??''),[busy,setBusy]=useState(false),[error,setError]=useState<MessageKey|''>('');
 const pending=useRef(false),revision=useRef(catalog.revision);
 const save=async()=>{
  if(pending.current)return;pending.current=true;setBusy(true);setError('');
  try{await call<NodeCatalog>('SetNodeCoordinator',{nodeId:selected,expectedRevision:revision.current});onChanged();}catch{setError('settings.nodeCoordinatorFailed');}finally{pending.current=false;setBusy(false);}
 };
 return <details className="settings-disclosure"><summary>{t('settings.nodeAlwaysOn')}</summary>
  <p className="settings-note">{t('settings.nodeCoordinatorHelp')}</p>
  <SettingRow label={t('settings.nodeAlwaysOn')} htmlFor="node-coordinator"><select id="node-coordinator" value={selected} disabled={busy} onChange={event=>{revision.current=catalog.revision;setSelected(event.target.value);}}><option value="">{t('settings.nodeCoordinatorNone')}</option>{catalog.nodes.map(node=><option key={node.id} value={node.id}>{node.label}</option>)}</select><button disabled={busy||selected===(catalog.broker?.nodeId??'')} onClick={()=>void save()}>{t('common.save')}</button></SettingRow>
  <p role="status" className="settings-note">{t(catalog.broker?.reachable&&catalog.broker.automaticRoaming?'settings.nodeRoamingAvailable':'settings.nodeRoamingUnavailable')}</p>
  {catalog.broker?.reason&&<p className="settings-note">{catalog.broker.reason}</p>}{error&&<p role="alert" className="inline-error">{t(error)}</p>}
 </details>;
}
