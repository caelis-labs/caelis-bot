import type {MessageKey} from '../../i18n/catalogs';
import {useEffect,useRef,useState} from 'react';
import type {backend} from '../../desktop';
import type {RuntimeSettings,SetupInstallProgress,SetupRequest} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SettingsDialog} from './SettingsDialog';

// Both App and Web observe the same host operation. Polling never starts or
// retries installation; refreshing/mounting a second tab only adopts its state.
export function useCodexInstallation(active:boolean,profile:RuntimeSettings,call:typeof backend,onSettled:()=>Promise<unknown>){
 const {t}=useI18n();
 const [progress,setProgress]=useState<SetupInstallProgress|null>(null),[starting,setStarting]=useState(false),[error,setError]=useState(''),[dismissed,setDismissed]=useState(''),[cancelling,setCancelling]=useState(false);
 const flight=useRef(false),settled=useRef(''),settling=useRef(false),callback=useRef(onSettled),generation=useRef(0);
 callback.current=onSettled;
 useEffect(()=>{
  if(!active)return;
  let live=true,reading=false;
  const read=async()=>{
   if(reading)return;reading=true;
   const ticket=generation.current;
   try{
    const p=await call<SetupInstallProgress>('SetupInstallProgress');
    if(!live||ticket!==generation.current||p.runtime!=='codex')return;
    setProgress(p);
    if(p.state==='failed'||p.state==='unknown')setCancelling(false);
    if(p.state==='cancelled')setError('');
    if(p.state!=='running'&&p.id&&p.id!==settled.current&&!settling.current){
     settling.current=true;
     try{await callback.current();if(live)settled.current=p.id;}finally{settling.current=false;}
    }
   }catch{/* Keep known progress; a transient read is never an effect retry. */}
   finally{reading=false;}
  };
  void read();const timer=window.setInterval(()=>void read(),750);
  return()=>{live=false;clearInterval(timer)};
 },[active,call]);
 const start=async(action:string)=>{
  if(flight.current)return;
  generation.current++;
  flight.current=true;setStarting(true);setError('');setDismissed('');setCancelling(false);
  try{
   // Reconcile an existing host operation before admitting a new write.
   const current=await call<SetupInstallProgress>('SetupInstallProgress');
   if(current.runtime==='codex'&&['running','unknown'].includes(current.state||'')){setProgress(current);return;}
   const request:SetupRequest={settings:profile,action,provider:'',baseUrl:'',model:'',apiKey:''};
   await call('ApplySetup',request);
   setProgress(await call<SetupInstallProgress>('SetupInstallProgress'));
  }catch{setError(t('settings.codexInstallRetryHint'));}
  finally{flight.current=false;setStarting(false);}
 };
 const running=starting||progress?.state==='running';
 const failed=progress&&['failed','unknown'].includes(progress.state||'');
 // A user's cancellation is an ordinary return to the installation page.
 // Keep observing the original host operation while the dialog is dismissed;
 // running/unknown still prevents starting a second installation.
 const show=active&&!cancelling&&(running||!!error||!!failed&&dismissed!==progress?.id);
 const phase=progress?.phase;
 const phaseKey=phase==='downloading'?'settings.codexDownloadStage':phase==='installing'?'settings.runtimeFinishing':phase==='verifying'?'settings.codexCheckStage':'settings.codexPrepareStage';
 const action=progress?.action==='update'?'update':'install';
 const failureKey:Record<string,MessageKey>={download_network:'settings.codexInstallNetwork',download_timeout:'settings.codexInstallTimeout',download_http:'settings.codexInstallHTTP',download_tls:'settings.codexInstallTLS',installation_permission:'settings.codexInstallPermission',storage_unavailable:'settings.codexInstallStorage',missing_dependency:'settings.codexInstallDependency',archive_invalid:'settings.codexInstallArchive',installer_exit:'settings.codexInstallExit',runtime_recovery_required:'settings.codexInstallBlocked'};
 const close=()=>{if(!running){setDismissed(progress?.id||'');setError('')}};
 const cancel=()=>{
  generation.current++;
  setCancelling(true);setError('');
  // A lost cancel response is reconciled by the existing progress reader,
  // never by replaying cancel or starting another install.
  void call('CancelSetupOperation').catch(()=>{});
 };
 const dialog=show?<SettingsDialog className="runtime-install-dialog" title={t(running?(action==='update'?'settings.codexUpdating':'settings.codexInstalling'):'settings.runtimeInstallFailed',{name:'Codex'})} busy={!!running} onClose={close}>
  {running?<><div className="runtime-install-progress" role="progressbar" aria-label={t(phaseKey)} aria-valuetext={t(phaseKey)} aria-busy="true"><span/></div><p className="settings-note" role="status">{t(phaseKey)}</p><div className="setup-end"><button disabled={cancelling} onClick={cancel}>{t('common.cancel')}</button></div></>:<>
   <p className="inline-error" role="alert">{progress?.state==='unknown'?t('settings.codexInstallUnknown'):error||t(failureKey[progress?.code||'']||'settings.codexInstallRetryHint')}</p>
   <div className="setup-end"><button onClick={close}>{t('common.close')}</button>{progress?.state==='unknown'?<button className="primary" onClick={()=>void callback.current().catch(()=>{})}>{t('runtime.recheck')}</button>:<button className="primary" onClick={()=>void start(action)}>{t('settings.runtimeTryAgain')}</button>}</div>
  </>}
 </SettingsDialog>:null;
 return {running:!!running,start,dialog};
}
