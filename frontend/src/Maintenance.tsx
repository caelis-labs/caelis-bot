import { useEffect, useState } from 'react';
import { desktop } from './desktop';
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
  <h1>{storage?'附件存储':'导出诊断'}</h1>
  {storage ? <>
   <p>发送时保存的附件副本。原始文件、聊天记录和生成的结果文件会保留。</p>
   {info&&<><strong>{size(info.bytes)} · {info.files} 个文件</strong><p>30 天前的副本：{size(info.eligibleBytes)} · {info.eligibleFiles} 个文件</p>{info.notice&&<p role="status">{info.notice}</p>}</>}
   {confirm?<section className="storage-confirm"><p>清理后，后续工作可能需要你重新附加这些文件。副本会移到系统废纸篓，可从那里恢复。</p><div><button disabled={busy} onClick={()=>void clean()}>移到废纸篓</button><button disabled={busy} onClick={()=>setConfirm(false)}>取消</button></div></section>:<div className="maintenance-actions"><button disabled={busy||!info?.canClean} onClick={()=>setConfirm(true)}>清理 30 天前的副本…</button><button disabled={busy} onClick={()=>void refresh()}>刷新</button></div>}
  </> : <>
   <p>遇到问题时，保存一份诊断报告供排查。</p>
   <p>只包含系统和组件版本、连接状态、工作状态及数量统计。不包含聊天正文、草稿内容、文件路径、审批命令或登录凭据。</p>
   <div className="maintenance-actions"><button disabled={busy} onClick={()=>void exportReport()}>{busy?'正在导出…':'选择保存位置…'}</button></div>
   <p>报告仅保存到本机，不会自动上传。</p>
  </>}
  {message&&<p role="status">{message}</p>}
 </section>;
}
