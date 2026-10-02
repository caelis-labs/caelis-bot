import {useCallback,useEffect,useMemo,useRef,useState} from 'react';
import {backend} from './desktop';
import type {RemoteManagementResult,RemoteRuntimeRequest,RemoteRuntimeState,Status} from './backend/contract';
import {SettingGroup,SettingRow} from './SettingsUI';
import {RemoteExecutionSettings} from './RemoteExecutionSettings';
import {RuntimeWorkspace} from './settings/runtime/RuntimeWorkspace';
import {createRemoteRuntimeSettingsClient} from './settings/runtime/remoteClient';
import {useI18n} from './i18n';
import {SettingsDialog} from './settings/runtime/SettingsDialog';

export function RemoteRuntimeSettings({active=true,refreshKey=0,call=backend,heading=true}:{active?:boolean;refreshKey?:number;call?:typeof backend;heading?:boolean}) {
 const {t}=useI18n();
 const [state,setState]=useState<RemoteRuntimeState|null>(null),[error,setError]=useState(''),[notice,setNotice]=useState(''),[busy,setBusy]=useState(false),[revision,setRevision]=useState(0);
 const working=useRef(false),epoch=useRef(0);
 const refresh=useCallback(async()=>{const generation=++epoch.current;try{const next=await call<RemoteRuntimeState>('RemoteRuntime');if(generation===epoch.current){setState(next);setError('');}}catch{if(generation===epoch.current){setError(t('settings.productManagementUnavailable'));setState(current=>current?{...current,available:false}:current);}}},[call,t]);
 useEffect(()=>{if(!active)return;void refresh();return()=>{epoch.current++;};},[active,refreshKey,revision,refresh]);
 const changed=useCallback(()=>{setRevision(value=>value+1);},[]);
 const message=state?.pending.length?t('settings.productManagementUnknown'):t('settings.productManagementUnavailable');
 const client=useMemo(()=>createRemoteRuntimeSettingsClient(call,state?.binding??'',message,changed),[call,state?.binding,message,changed]);
 const recover=async(id:string,runtime?:RemoteRuntimeRequest|null)=>{
  if(working.current||!state?.available)return;working.current=true;setBusy(true);setError('');setNotice('');
  try {const result=runtime?await call<RemoteManagementResult>('ManageRemoteRuntime',{...runtime,binding:state.binding,action:'resolve'}):await call<RemoteManagementResult>('ReconcileRemoteManagement',state.binding,id);setNotice(t(result.outcome==='accepted'?'settings.productManagementConfirmed':result.outcome==='rejected'?'settings.productManagementRejected':'settings.productManagementUnknown'));}
  catch {setError(t('settings.productManagementUnknown'));}
  finally {working.current=false;setBusy(false);changed();}
 };
 return <section>
  <p className="settings-note">{t('settings.productCredentialHandoff')}</p>
  {state?.pending.length ? <div role="status"><p>{t('settings.productManagementUnknown')}</p>{state.pending.map(pending=><button key={pending.id} disabled={busy||!state.available} onClick={()=>void recover(pending.id,pending.runtime)}>{t('settings.productCheckOriginalReceipt')}</button>)}</div>:null}
  {!state?.available?<p role="status">{t('settings.productManagementUnavailable')}</p>:state.capabilities.configuration?<RuntimeWorkspace heading={heading} key={`configuration:${state.binding}`} remote client={client} active={active} refreshKey={revision} preparation={runtime=><RemoteRuntimeInstallation call={call} runtime={runtime} state={state} onChanged={changed}/>}/>:heading?<h1>{t('settings.productTargetRuntime')}</h1>:null}
  {state?.available&&state.capabilities.execution&&<RemoteExecutionSettings call={call} key={`execution:${state.binding}`} state={state} active={active} refreshKey={revision} onChanged={changed}/>}
  {state?.available&&state.capabilities.installation&&<details className="settings-disclosure"><summary>{t('settings.productTargetPrograms')}</summary>{['codex','caelis'].map(runtime=><RemoteRuntimeInstallation call={call} key={`${state.binding}:${runtime}`} runtime={runtime} state={state} onChanged={changed}/>)}</details>}
  {error&&<p role="alert" className="inline-error">{error}</p>}{notice&&<p role="status" className="settings-note">{notice}</p>}
  <button disabled={busy} className="text-action" onClick={()=>{setError('');void refresh();}}>{t('runtime.refreshConfig')}</button>
 </section>;
}

function RemoteRuntimeInstallation({runtime,state,onChanged,call=backend}:{runtime:string;state:RemoteRuntimeState;onChanged:()=>void;call?:typeof backend}) {
 const {t}=useI18n();
 const [status,setStatus]=useState<Status|null>(null),[version,setVersion]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState(''),[confirm,setConfirm]=useState(false),[edited,setEdited]=useState(false);
 const working=useRef(false),epoch=useRef(0);
 const versions=state.releases.filter(release=>release.runtime===runtime).map(release=>release.version);
 const name=runtime==='codex'?'Codex':'Caelis';
 const refresh=useCallback(async()=>{const generation=++epoch.current;try{const next=await call<Status>('RemoteRuntimeStatus',state.binding,runtime);if(generation===epoch.current){setStatus(next);setError('');setVersion(current=>current||next.latestVersion||'');}}catch{if(generation===epoch.current){setError(t('settings.productManagementUnavailable'));}}},[call,state.binding,runtime,t]);
 useEffect(()=>{if(state.capabilities.installation)void refresh();return()=>{epoch.current++;};},[refresh,state.capabilities.installation]);
 const apply=async()=>{
  if(working.current||state.pending.length||!status||!version)return;working.current=true;setBusy(true);setConfirm(false);setError('');
  try{const result=await call<RemoteManagementResult>('ManageRemoteRuntime',{id:crypto.randomUUID(),binding:state.binding,action:status.installed?'update':'install',runtime,version,expectedVersion:status.installed?status.version:''});if(result.outcome!=='accepted')setError(t(result.outcome==='rejected'?'settings.productManagementRejected':'settings.productManagementUnknown'));else if(result.status)setStatus(result.status);}
  catch {setError(t('settings.productManagementUnknown'));}
  finally{working.current=false;setBusy(false);onChanged();}
 };
 useEffect(()=>{const guard=(event:Event)=>{if(edited||confirm||busy)event.preventDefault();};window.addEventListener('settings-navigate',guard);return()=>window.removeEventListener('settings-navigate',guard);},[edited,confirm,busy]);
 if(!state.capabilities.installation)return <p>{t('settings.productManagementUnavailable')}</p>;
 const blocked=busy||!!error||state.pending.length>0;
 return <SettingGroup title={name}>
  <SettingRow label={t('runtime.installed')}><span>{status?.installed?status.version:status?t('runtime.notInstalled'):t('runtime.detecting')}</span><button disabled={busy} className="text-action" onClick={()=>void refresh()}>{t('runtime.recheck')}</button></SettingRow>
  <SettingRow label={t('settings.productReviewedVersion')}><select value={version} disabled={blocked} onChange={event=>{setVersion(event.target.value);setEdited(true);}}><option value="" disabled>{t('settings.productSelectVersion')}</option>{versions.map(value=><option key={value} value={value}>{value}</option>)}</select><button disabled={blocked||!status||!versions.includes(version)||status.installed&&status.version===version} onClick={()=>setConfirm(true)}>{t(status?.installed?'runtime.update':'runtime.installRuntime',{name})}</button></SettingRow>
  {edited&&!confirm&&<button disabled={busy} className="text-action" onClick={()=>setEdited(false)}>{t('common.cancel')}</button>}
  {confirm&&<SettingsDialog title={t('runtime.confirm')} busy={busy} onClose={()=>{setConfirm(false);setEdited(false);}}><p>{t('settings.productConfirmInstall',{name,version,target:state.label})}</p><button disabled={busy} onClick={()=>{setConfirm(false);setEdited(false);}}>{t('common.cancel')}</button><button disabled={blocked} onClick={()=>void apply()}>{t('runtime.confirm')}</button></SettingsDialog>}
  {error&&<p role="alert" className="inline-error">{error}</p>}
 </SettingGroup>;
}
