import { useEffect, useState } from 'react';
import { desktop } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';
import type { AttachmentStorage } from './backend/contract';

function size(bytes:number) { return bytes<1024*1024 ? `${Math.ceil(bytes/1024)} KB` : `${(bytes/1024/1024).toFixed(1)} MB`; }
export function Maintenance({storage}:{storage:boolean}) {
 const [info,setInfo]=useState<AttachmentStorage|null>(null),[busy,setBusy]=useState(false),[message,setMessage]=useState(''),[confirm,setConfirm]=useState(false);
 const refresh=async()=>{try{setInfo(await desktop<AttachmentStorage>('AttachmentStorage'));}catch{setMessage('暂时无法读取附件存储，请重试');}};
 useEffect(()=>{
  if(storage)void refresh();
  const opened=()=>{setConfirm(false);setMessage('');void refresh();};
  window.addEventListener('storage-open',opened);
  return()=>{window.removeEventListener('storage-open',opened);};
 },[storage]);
 const clean=async()=>{
  setBusy(true);setMessage('');
  try{setInfo(await desktop<AttachmentStorage>('CleanAttachmentStorage'));setMessage('旧附件副本已移到废纸篓。');}
  catch(e){setMessage(e instanceof Error?e.message:'暂时无法清理，请重试');await refresh();}
  finally{setBusy(false);setConfirm(false);}
 };
 const exportReport=async()=>{setBusy(true);setMessage('');try{setMessage(await desktop<string>('ExportDiagnostics'));}catch{setMessage('暂时无法保存诊断报告，请重试');}finally{setBusy(false);}};
 return <section className="maintenance-surface">
  <h1>{storage?'存储':'诊断'}</h1>
  {storage ? <>
   <SettingGroup title="附件副本">
    <SettingRow label="已用空间" description={info?`${info.files} 个文件`:message?'读取失败':'正在读取…'}>{!info&&message?<button onClick={()=>{setMessage('');void refresh();}}>重试</button>:<span className="setting-value">{info?size(info.bytes):'—'}</span>}</SettingRow>
    <SettingRow label="30 天前的副本" description={info?`${size(info.eligibleBytes)} · ${info.eligibleFiles} 个文件`:undefined}><button disabled={busy||!info?.canClean} onClick={()=>setConfirm(true)}>清理…</button></SettingRow>
   </SettingGroup>
   {info?.notice&&<p className="settings-note" role="status">{info.notice}</p>}
   {confirm&&<section className="storage-confirm"><p>旧副本将移到废纸篓，后续使用时可能需要重新添加。原始文件和聊天记录会保留。</p><div><button disabled={busy} onClick={()=>setConfirm(false)}>取消</button><button className="primary" disabled={busy} onClick={()=>void clean()}>移到废纸篓</button></div></section>}
  </> : <>
   <SettingGroup><SettingRow label="诊断报告" description="仅保存在本机，不含聊天内容或凭据"><button disabled={busy} onClick={()=>void exportReport()}>{busy?'正在导出…':'导出报告'}</button></SettingRow></SettingGroup>
  </>}
  {message&&<p className="settings-note" role="status">{message}</p>}
 </section>;
}
