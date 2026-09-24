import { useEffect, useState } from 'react';
import { desktop } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';
import type { AttachmentStorage } from './backend/contract';
import { useI18n } from './i18n';

function size(bytes:number) { return bytes<1024*1024 ? `${Math.ceil(bytes/1024)} KB` : `${(bytes/1024/1024).toFixed(1)} MB`; }
export function Maintenance({storage}:{storage:boolean}) {
 const {t}=useI18n();
 const [info,setInfo]=useState<AttachmentStorage|null>(null),[busy,setBusy]=useState(false),[message,setMessage]=useState(''),[confirm,setConfirm]=useState(false);
 const refresh=async()=>{try{setInfo(await desktop<AttachmentStorage>('AttachmentStorage'));}catch{setMessage(t('settings.storageLoadFailed'));}};
 useEffect(()=>{
  if(storage)void refresh();
  const opened=()=>{setConfirm(false);setMessage('');void refresh();};
  window.addEventListener('storage-open',opened);
  return()=>{window.removeEventListener('storage-open',opened);};
 },[storage]);
 const clean=async()=>{
  setBusy(true);setMessage('');
  try{setInfo(await desktop<AttachmentStorage>('CleanAttachmentStorage'));setMessage(t('settings.storageCleanSuccess'));}
  catch(e){setMessage(e instanceof Error?e.message:t('settings.storageCleanFailed'));await refresh();}
  finally{setBusy(false);setConfirm(false);}
 };
 const exportReport=async()=>{setBusy(true);setMessage('');try{setMessage(await desktop<string>('ExportDiagnostics'));}catch{setMessage(t('settings.diagnosticsExportFailed'));}finally{setBusy(false);}};
 return <section className="maintenance-surface">
  <h1>{storage?t('settings.storage'):t('settings.diagnostics')}</h1>
  {storage ? <>
   <SettingGroup title={t('settings.storageCopiesGroup')}>
    <SettingRow label={t('settings.storageUsed')} description={info?t('settings.storageFileCount',{count:info.files}):message?t('settings.storageReadFailed'):t('common.loading')}>{!info&&message?<button onClick={()=>{setMessage('');void refresh();}}>{t('settings.retry')}</button>:<span className="setting-value">{info?size(info.bytes):'—'}</span>}</SettingRow>
    <SettingRow label={t('settings.storageEligibleGroup')} description={info?`${size(info.eligibleBytes)} · ${t('settings.storageFileCount',{count:info.eligibleFiles})}`:undefined}><button disabled={busy||!info?.canClean} onClick={()=>setConfirm(true)}>{t('settings.storageCleanButton')}</button></SettingRow>
   </SettingGroup>
   {info?.notice&&<p className="settings-note" role="status">{info.notice}</p>}
   {confirm&&<section className="storage-confirm"><p>{t('settings.storageConfirmPrompt')}</p><div><button disabled={busy} onClick={()=>setConfirm(false)}>{t('common.cancel')}</button><button className="primary" disabled={busy} onClick={()=>void clean()}>{t('settings.storageMoveToTrash')}</button></div></section>}
  </> : <>
   <SettingGroup><SettingRow label={t('settings.diagnosticsReport')} description={t('settings.diagnosticsReportDescription')}><button disabled={busy} onClick={()=>void exportReport()}>{busy?t('settings.diagnosticsExporting'):t('settings.diagnosticsExportButton')}</button></SettingRow></SettingGroup>
  </>}
  {message&&<p className="settings-note" role="status">{message}</p>}
 </section>;
}
