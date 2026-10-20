import {useEffect,useRef,useState} from 'react';
import {desktop} from './desktop';
import type {MessageKey} from './i18n/catalogs';
import {useI18n} from './i18n';
import {SettingGroup,SettingRow} from './SettingsUI';
import {Maintenance} from './Maintenance';
import {updateProgress,updateReason,type UpdateIntent,type UpdateView} from './managed-update-client';
export function ManagedUpdates({version,active,focusVersion}:{version:string;active:boolean;focusVersion:number}){
 const {t}=useI18n(),[view,setView]=useState<UpdateView>(),[error,setError]=useState(''),[busy,setBusy]=useState(false),refresh=useRef(()=>{}),flight=useRef(false);
 useEffect(()=>{
  if(!active)return;let live=true,timer:ReturnType<typeof setTimeout>,reading=false,failures=0;
  const read=async()=>{if(reading)return;reading=true;clearTimeout(timer);
   try{const next=await desktop<UpdateView>('ManagedUpdateRead');if(live){setView(next);failures=0;setError(current=>current==='connection'||!next.pending&&['update_unconfirmed','receipt_unconfirmed'].includes(current)?'':current)}}catch{if(live){failures++;setError('connection')}}finally{reading=false;if(live)timer=setTimeout(read,Math.min(1000*2**failures,15000));}
  };refresh.current=()=>void read();void read();return()=>{live=false;clearTimeout(timer);refresh.current=()=>{}};
 },[active,focusVersion]);
 const act=async(intent:UpdateIntent)=>{if(flight.current)return;flight.current=true;setBusy(true);setError('');try{setView(await desktop<UpdateView>('ManagedUpdateCommand',intent));}catch(e){setError(updateReason(e instanceof Error?e.message:undefined))}finally{flight.current=false;setBusy(false);refresh.current()}};
 const status=view?.status,managed=status?.mode==='managed',state=status?.state||'loading',locked=busy||!!view?.pending;
 const phases=['idle','checking','no_compatible_update','available','downloading','verifying','ready','waiting_idle','restarting','installed','rolling_back','rolled_back','failed','unknown','check_failed'];
 const phase=state==='installed'&&!view?.confirmed?'restarting':phases.includes(state)?state:'loading';
 const progress=status?updateProgress(status):undefined;
 const reason=error||(status?.reason?updateReason(status.reason):'');
 const working=['checking','downloading','verifying','waiting_idle','restarting','rolling_back'].includes(state);
 return <section className="update-settings">
  <h1>{t('settings.updates')}</h1>
  <div className="about-identity"><img src="/icons/caelis-avatar.png" alt="" width="64" height="64"/><div><h2>CaelisBot</h2><p>{status?.current.version||version}{status?.testArtifact?' · '+t('settings.managedQA'):''}</p></div></div>
  <SettingGroup>
   {managed&&<SettingRow label={t('settings.managedAutomatic')} htmlFor="managed-automatic" description={t('settings.managedAutomaticHint')}><input id="managed-automatic" className="settings-switch" role="switch" type="checkbox" checked={status.automatic} disabled={locked||working||state==='unknown'} onChange={e=>void act({action:'automatic',automatic:e.target.checked})}/></SettingRow>}
   <SettingRow label={t('settings.softwareUpdate')} description={<span role="status">{t(('settings.managedPhase_'+phase) as MessageKey)}{status?.latest&&['available','ready','downloading','verifying','waiting_idle'].includes(state)?' '+status.latest:''}</span>}><button disabled={!status||locked||working||state==='unknown'} onClick={()=>void act({action:'check'})}>{t('settings.checkUpdatesButton')}</button></SettingRow>
   {reason&&<SettingRow label={t('settings.managedDetail')} description={t(('settings.managedReason_'+reason) as MessageKey)}><button onClick={()=>{setError('');refresh.current()}}>{t('settings.managedRefresh')}</button></SettingRow>}
   {state==='downloading'&&status&&<SettingRow label={t('settings.managedDownload')} description={`${(status.receivedBytes/1048576).toFixed(1)} MB${status.totalBytes>0?' / '+(status.totalBytes/1048576).toFixed(1)+' MB':''}`}><progress aria-label={t('settings.managedDownload')} max={100} value={progress}/></SettingRow>}
   {view?.pending&&<p className="settings-note" role="status">{t('settings.managedPending')}</p>}
   {managed&&status?.offerId&&['available','failed'].includes(state)&&<SettingRow label={t('settings.managedDownload')} description={t('settings.managedDownloadHint')}><button disabled={locked} onClick={()=>void act({action:'download',offerId:status.offerId})}>{t('settings.managedDownload')}</button></SettingRow>}
   {managed&&state==='ready'&&<SettingRow label={t('settings.managedInstall')} description={t('settings.managedInstallHint')}><button disabled={locked} onClick={()=>void act({action:'install',offerId:status?.offerId,when:'idle'})}>{t('settings.managedSchedule')}</button><button disabled={locked} onClick={()=>void act({action:'install',offerId:status?.offerId,when:'now'})}>{t('settings.managedInstall')}</button></SettingRow>}
   {managed&&state==='waiting_idle'&&status?.transactionId&&<SettingRow label={t('settings.managedScheduled')}><button disabled={locked} onClick={()=>void act({action:'cancel',transactionId:status.transactionId,revision:status.revision})}>{t('common.cancel')}</button></SettingRow>}
  </SettingGroup><Maintenance storage={false} embedded/>
 </section>;
}
