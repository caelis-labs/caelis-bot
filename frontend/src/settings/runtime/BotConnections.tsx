import {useEffect, useRef, useState} from 'react';
import {backend, desktop} from '../../desktop';
import type {BotConnectionInput, Machine, WorkerSharing} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {DesktopTowerIcon, SettingsChevron} from '../../SettingsIcons';
import {SettingsDialog} from './SettingsDialog';

const emptyConnection = (): BotConnectionInput => ({id:'', name:'', address:'', code:''});

export function BotConnections({active=true, call=backend}: {active?:boolean; call?:typeof backend}) {
 const {t}=useI18n();
 const [machines,setMachines]=useState<Machine[]>([]),[sharing,setSharing]=useState<WorkerSharing|null>(null);
 const [draft,setDraft]=useState<BotConnectionInput|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(''),[dialogError,setDialogError]=useState(''),[copied,setCopied]=useState(false);
 const pending=useRef(false),epoch=useRef(0),[now,setNow]=useState(()=>Date.now());
 const reload=async()=>{
  const generation=epoch.current;
  const [connections,status]=await Promise.all([call<Machine[]>('Machines'),call<WorkerSharing>('WorkerSharing')]);
  if(generation!==epoch.current)return;
  setMachines(connections.filter(machine=>machine.connectionType==='bot'));
  setSharing(old=>({...status,...(old?.code&&old.expiresAt&&old.expiresAt*1000>Date.now()&&status.enabled&&old.peer===status.peer?{code:old.code,expiresAt:old.expiresAt}:{})}));
 };
 useEffect(()=>{if(!active)return;void reload().catch(()=>setError(t('settings.botReadFailed')));const timer=window.setInterval(()=>{setNow(Date.now());if(!pending.current)void reload().catch(()=>{});},4000);return()=>{epoch.current++;window.clearInterval(timer);};},[active,call]);
 const problem=(cause:unknown)=>{
  const value=cause instanceof Error?cause.message:String(cause);
  if(value.includes('machine_has_tasks'))return t('settings.botHasTasks');
  if(value.includes('identity_changed'))return t('settings.botIdentityChanged');
  if(value.includes('incompatible'))return t('settings.botIncompatible');
  if(value.includes('not_ready')||value.includes('not ready')||value.includes('worker_not'))return t('settings.botWorkerNotReady');
  return t('settings.botActionFailed');
 };
 const change=async(action:()=>Promise<WorkerSharing>)=>{
  if(pending.current)return;pending.current=true;setBusy(true);setError('');setCopied(false);
  try{setSharing(await action());}catch(cause){setError(problem(cause));}finally{pending.current=false;setBusy(false);}
 };
 const pairingLive=!!sharing?.code&&!!sharing.expiresAt&&sharing.expiresAt*1000>now;
 const copy=async()=>{
  if(!sharing?.code||!pairingLive)return;
  try{await desktop('CopyText',JSON.stringify({address:sharing.address,code:sharing.code}));setCopied(true);setError('');}catch{setError(t('settings.botCopyFailed'));}
 };
 const connect=async()=>{
  if(!draft||pending.current)return;pending.current=true;setBusy(true);setDialogError('');
  try{await call<Machine>('ConnectBot',draft);setDraft(null);await reload();}catch(cause){setDialogError(problem(cause));}finally{pending.current=false;setBusy(false);}
 };
 const remove=async()=>{
  if(!draft||pending.current)return;pending.current=true;setBusy(true);setDialogError('');
  try{await call('RemoveMachine',draft.id);setDraft(null);await reload();}catch(cause){setDialogError(problem(cause));}finally{pending.current=false;setBusy(false);}
 };
 const recover=async()=>{
  if(!draft||pending.current)return;pending.current=true;setBusy(true);setDialogError('');
  try{await call<Machine>('RecoverBotWork',draft.id);await reload();}catch(cause){setDialogError(problem(cause));}finally{pending.current=false;setBusy(false);}
 };
 const revoke=async(source:string)=>{
  if(pending.current)return;pending.current=true;setBusy(true);setError('');
  try{await call('RevokeWorkerPeer',source);await reload();}catch(cause){setError(problem(cause));}finally{pending.current=false;setBusy(false);}
 };
 const begin=(machine?:Machine)=>{setDialogError('');setDraft(machine?{id:machine.id,name:machine.name,address:machine.address,code:''}:emptyConnection());};
 return <section className="machine-section" aria-label={t('settings.botConnectionsTitle')}>
  <div className="runtime-section-title"><div><h2>{t('settings.botConnectionsTitle')}</h2><p className="settings-note">{t('settings.botConnectionsNote')}</p></div><button disabled={busy} onClick={()=>begin()}>{t('settings.botAdd')}</button></div>
  {machines.length>0&&<ul className="machine-list">{machines.map(machine=><li key={machine.id}><button className="machine-row" disabled={busy} onClick={()=>begin(machine)}><span className="machine-icon" aria-hidden="true"><DesktopTowerIcon size={20}/></span><span><strong>{machine.name}</strong><small>{machine.address}</small></span><span className={`machine-status ${machine.state==='ready'?'ready':''}`}>{t(machine.state==='ready'?'settings.machineReady':'settings.machineOffline')}</span><SettingsChevron/></button></li>)}</ul>}
  <div className="runtime-setting-row"><div><strong>{t('settings.botSharing')}</strong><p className="settings-note">{t('settings.botSharingNote')}</p></div><input type="checkbox" aria-label={t('settings.botSharing')} checked={!!sharing?.enabled} disabled={busy||sharing===null} onChange={event=>void change(()=>call<WorkerSharing>('SetWorkerSharing',event.target.checked))}/></div>
  {sharing?.enabled&&<>
   <div className="machine-actions"><button disabled={busy} onClick={()=>void change(()=>call<WorkerSharing>('CreateWorkerPairing'))}>{t('settings.botGeneratePairing')}</button><button disabled={busy} onClick={()=>void change(()=>call<WorkerSharing>('RecoverWorkerSharing'))}>{t('settings.botRecoverNewWork')}</button></div>
   <p className="settings-note">{t('settings.botRecoverNote')}</p>
   {sharing.code&&<div className="machine-guide">
    {pairingLive?<><strong>{t('settings.botPairingInfo')}</strong><dl className="machine-connection-values"><div><dt>{t('settings.botAddress')}</dt><dd>{sharing.address}</dd></div><div><dt>{t('settings.botCode')}</dt><dd><code>{sharing.code}</code></dd></div></dl><p>{t('settings.botPairingExpires',{time:new Date(sharing.expiresAt!*1000).toLocaleTimeString()})}</p><p>{t('settings.botReachableAddress')}</p><button disabled={busy} onClick={()=>void copy()}>{t(copied?'settings.botCopied':'settings.botCopyPairing')}</button></>:<p role="status">{t('settings.botPairingExpired')}</p>}
   </div>}
   {!!sharing.sources?.length&&<div className="machine-extra"><strong>{t('settings.botConnectedSources')}</strong><ul className="machine-list">{sharing.sources.map(source=><li key={source.id}><div className="machine-status-actions"><span><strong>{source.name||t('settings.botConnectedSource')}</strong><small>{source.id.slice(-8)}{source.pairedAt>0&&` · ${new Date(source.pairedAt).toLocaleString()}`}</small></span><button disabled={busy} onClick={()=>void revoke(source.id)}>{t('settings.botRevoke')}</button></div></li>)}</ul></div>}
  </>}
  {error&&<p role="alert" className="inline-error">{error}</p>}
  {draft&&<SettingsDialog title={t(draft.id?'settings.botEdit':'settings.botAdd')} description={t('settings.botConnectNote')} busy={busy} onClose={()=>setDraft(null)}>
   <form onSubmit={event=>{event.preventDefault();void connect();}}>
    <label>{t('settings.botAddress')}<input autoFocus required type="url" autoComplete="off" placeholder="http://127.0.0.1:29317" disabled={busy} value={draft.address} onChange={event=>setDraft({...draft,address:event.target.value})} onPaste={event=>{
     try{const info=JSON.parse(event.clipboardData.getData('text'));if(typeof info.address==='string'&&typeof info.code==='string'){event.preventDefault();setDraft({...draft,address:info.address,code:info.code});}}catch{/* Ordinary address paste remains unchanged. */}
    }}/></label>
    <label>{t('settings.botCode')}<input required type="password" autoComplete="off" disabled={busy} value={draft.code} onChange={event=>setDraft({...draft,code:event.target.value})}/></label>
    <label>{t('settings.machineName')}<input maxLength={80} autoComplete="off" disabled={busy} value={draft.name} onChange={event=>setDraft({...draft,name:event.target.value})}/></label>
    {draft.id&&<p className="settings-note">{t('settings.botRePairNote')}</p>}
    {draft.id&&<><button type="button" disabled={busy} onClick={()=>void recover()}>{t('settings.botRecoverNewWork')}</button><p className="settings-note">{t('settings.botRecoverNote')}</p></>}
    {dialogError&&<p role="alert" className="inline-error">{dialogError}</p>}
    <div className="setup-end">{draft.id&&<button type="button" className="text-action" disabled={busy} onClick={()=>void remove()}>{t('settings.machineRemove')}</button>}<button type="button" disabled={busy} onClick={()=>setDraft(null)}>{t('common.cancel')}</button><button type="submit" className="primary" disabled={busy}>{t(busy?'settings.machineConnecting':'settings.machineConnect')}</button></div>
   </form>
  </SettingsDialog>}
 </section>;
}
