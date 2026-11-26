import {useEffect,useRef,useState} from 'react';
import {desktop} from './desktop';
import type {MessageKey} from './i18n/catalogs';
import {useI18n} from './i18n';
import {UpdateSettingsShell} from './UpdateSettingsShell';
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
 const latestSuffix=status?.latest&&['available','ready','downloading','verifying','waiting_idle'].includes(state)?` ${status.latest}`:'';
 const reasonText=reason?t(('settings.managedReason_'+reason) as MessageKey):'';
 const reloadReady=!!(view?.confirmed&&status?.current.version!==version);
 const statusText=view?.pending?t('settings.managedPending'):
  error?reasonText:
  reloadReady?t('settings.managedReloadHint'):
  state==='idle'?(reasonText||t('settings.checkNewVersion')):
  `${t(('settings.managedPhase_'+phase) as MessageKey)}${latestSuffix}${reasonText?` · ${reasonText}`:''}`;
 const progressInfo=state==='downloading'&&status?{
  label:t('settings.managedDownload'),
  value:progress,
  detail:`${(status.receivedBytes/1048576).toFixed(1)} MB${status.totalBytes>0?' / '+(status.totalBytes/1048576).toFixed(1)+' MB':''}`,
 }:undefined;
 const actions=reloadReady?
  <button className="primary" onClick={()=>void desktop('RestartSurface')}>{t('settings.managedReload')}</button>:
  managed&&state==='waiting_idle'&&status?.transactionId?
  <button disabled={locked} onClick={()=>void act({action:'cancel',transactionId:status.transactionId,revision:status.revision})}>{t('common.cancel')}</button>:
  managed&&state==='ready'?
  <button className="primary" disabled={locked} onClick={()=>void act({action:'install',offerId:status?.offerId,when:'now'})}>{t('settings.managedInstall')}</button>:
  managed&&status?.offerId&&['available','failed'].includes(state)?
  <button className="primary" disabled={locked} onClick={()=>void act({action:'download',offerId:status.offerId})}>{t('settings.managedDownload')}</button>:
  <button disabled={!status||locked||working||state==='unknown'} onClick={()=>void act({action:'check'})}>{t('settings.checkUpdatesButton')}</button>;
 return <UpdateSettingsShell
  badge={status?.testArtifact?t('settings.managedQA'):undefined}
  status={statusText}
  progress={progressInfo}
  actions={actions}
  automatic={{
   id:'managed-automatic',
   label:t('settings.managedAutomatic'),
   description:!status?t('common.loading'):managed?t('settings.managedAutomaticHint'):(reasonText||t('settings.updateNoteManual')),
   checked:managed?status.automatic:false,
   disabled:!managed||locked||working||state==='unknown',
   onChange:value=>void act({action:'automatic',automatic:value}),
  }}
 />;
}
