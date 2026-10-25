import {useEffect, useRef, useState} from 'react';
import {backend, desktop} from '../../desktop';
import type {BotConnectionInput, Machine, WorkerSharing} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SettingsDialog} from './SettingsDialog';
import {botAddress,botProfileAt} from './bot-address';

type BotRecovery={operationId:string;profileId:string;canAuthorize:boolean};
const emptyConnection = (): BotConnectionInput => ({id:'bot-'+Array.from(crypto.getRandomValues(new Uint8Array(16)),v=>v.toString(16).padStart(2,'0')).join(''),name:'',address:'',code:''});

export function BotConnectionDialog({machines,initial,call=backend,onClose,onSaved,onChanged}:{machines:Machine[];initial?:Machine;call?:typeof backend;onClose:()=>void;onSaved:()=>void;onChanged:()=>Promise<void>}) {
 const {t}=useI18n();
 const [draft,setDraft]=useState<BotConnectionInput|null>(()=>initial?{id:initial.id,name:initial.name,address:initial.address,code:''}:emptyConnection());
 const [busy,setBusy]=useState(false),[dialogError,setDialogError]=useState('');
 const [port,setPort]=useState('');
 const [recovery,setRecovery]=useState<BotRecovery|null>(null),[newAuthorization,setNewAuthorization]=useState(false);
 const pending=useRef(false);
 const newProfileID=useRef(draft?.id);
 const selected=machines.find(machine=>machine.id===draft?.id);
 const problem=(cause:unknown,local=false,targetName?:string)=>{
  const value=cause instanceof Error?cause.message:String(cause);
  const name=local?t('settings.botThisMachine'):targetName||t('settings.botTargetMachine');
  if(value.includes('new_pairing_code_required'))return t('settings.botFreshCodeRequired');
  if(value.includes('original_pairing_code_unavailable'))return t('settings.botOriginalPairingUnavailable');
  if(value.includes('original_bot_authorization_changed'))return t('settings.botAuthorizationChanged');
  if(value.includes('unreachable'))return t('settings.botUnreachable',{name});
  if(value.includes('code_expired'))return t('settings.botCodeExpired',{name});
  if(value.includes('code_invalid'))return t('settings.botCodeInvalid',{name});
  if(value.includes('revoked')||value.includes('authentication_required'))return t('settings.botAccessRevoked',{name});
  if(value.includes('proof_required')||value.includes('credential_unavailable'))return t('settings.botProofRequired');
  if(value.includes('sharing_disabled'))return t('settings.botSharingDisabled',{name});
  if(value.includes('listener_in_use'))return t('settings.botListenerInUse');
  if(value.includes('listener_managed'))return t('settings.botListenerManaged');
  if(value.includes('reachable_address_required')||value.includes('invalid_bot_address'))return t('settings.botAddressInvalid');
  if(value.includes('machine_has_tasks'))return t('settings.botHasTasks');
  if(value.includes('identity_changed'))return t('settings.botIdentityChanged');
  if(value.includes('incompatible'))return t('settings.botIncompatible');
  if(value.includes('not_ready')||value.includes('not ready')||value.includes('worker_not'))return t(local?'settings.botWorkerNotReady':'settings.botTargetNotReady',{name});
  return t('settings.botActionFailed');
 };
 const changeAddress=(address:string,code?:string,nextPort=port)=>{
  try{const info=JSON.parse(address);if(typeof info.address==='string'&&typeof info.code==='string'){address=info.address;code=info.code;nextPort=''}}catch{/* Address entry. */}
  if(!draft)return;
  setPort(nextPort);
  const existing=!initial&&!recovery?botProfileAt(machines,address,nextPort):undefined;
  setDraft({...draft,...(!initial&&!recovery?{id:existing?.id||newProfileID.current!,name:draft.name||existing?.name||''}:{}),address,...(code!==undefined?{code}:{})});
 };

 const needsAuthorization=/revoked|authentication_required|original_peer_proof_required/.test(selected?.issue||'');
 const needsWorkRecovery=!!selected?.issue?.includes('worker_not_ready');
 const restoreVisible=!needsAuthorization&&!recovery&&!newAuthorization&&!!draft?.id&&!!selected&&(selected.archived||selected.state!=='ready')&&botAddress(draft.address,port)===selected.address&&!draft.code.trim();
 const connect=async(reconnect=false)=>{
  if(!draft||pending.current)return;pending.current=true;setBusy(true);setDialogError('');
  try{
   const existing=!initial&&!recovery?botProfileAt(machines,draft.address,port):undefined;
   const connection={...draft,id:existing?.id||draft.id,address:botAddress(draft.address,port)};
   if(newAuthorization)await call<Machine>('AuthorizeBotConnection',{connection:{...connection,reconnect:true},originalOperationId:recovery?.operationId||''});
   else await call<Machine>('ConnectBot',{...connection,reconnect});
   onSaved();
  }catch(cause){
   const original=(cause as {botRecovery?:BotRecovery})?.botRecovery;
   if(original){setRecovery(original);setDraft({...draft,id:original.profileId});if(newAuthorization)setNewAuthorization(false);}
   setDialogError(problem(cause,false,draft?.name));
  }finally{pending.current=false;setBusy(false);}
 };
 const beginAuthorization=()=>{
  if(!draft)return;
  setDraft({...draft,code:''});setNewAuthorization(true);setDialogError('');
 };
 const remove=async()=>{
  if(!draft||pending.current)return;pending.current=true;setBusy(true);setDialogError('');
  try{await call('RemoveMachine',draft.id);onSaved();}catch(cause){setDialogError(problem(cause,false,draft?.name));}finally{pending.current=false;setBusy(false);}
 };
 const recover=async()=>{
  if(!draft||pending.current)return;pending.current=true;setBusy(true);setDialogError('');
  try{await call<Machine>('RecoverBotWork',draft.id);await onChanged();}catch(cause){setDialogError(problem(cause,false,draft?.name));}finally{pending.current=false;setBusy(false);}
 };
 return draft&&<SettingsDialog title={t(selected?'settings.botEdit':'settings.remoteBotMethod')} description={t('settings.botConnectNote')} className="remote-bot-dialog" busy={busy} onClose={onClose}>
   <form onSubmit={event=>{event.preventDefault();void connect();}}>
    <div className="remote-address-fields"><label>{t('settings.botConnectionInfo')}<input autoFocus required type="text" inputMode="url" autoComplete="off" placeholder="100.64.0.2" disabled={busy} value={draft.address} onChange={event=>changeAddress(event.target.value)} onPaste={event=>{
     try{const info=JSON.parse(event.clipboardData.getData('text'));if(typeof info.address==='string'&&typeof info.code==='string'){event.preventDefault();changeAddress(info.address,info.code,'');}}catch{/* Ordinary address paste remains unchanged. */}
    }}/></label>
    <label>{t('settings.botOptionalPort')}<input type="number" min="1" max="65535" placeholder="9317" autoComplete="off" disabled={busy} value={port} onChange={event=>changeAddress(draft.address,undefined,event.target.value)}/></label></div>
    <label>{t('settings.botCode')}<input required={newAuthorization||needsAuthorization||!selected} pattern="[0-9]{6}" inputMode="numeric" autoComplete="off" placeholder="000000" maxLength={6} disabled={busy} value={draft.code} onChange={event=>setDraft({...draft,code:event.target.value})}/></label>
    {selected?<label>{t('settings.machineName')}<input maxLength={80} placeholder={t('settings.remoteAutomaticName')} autoComplete="off" disabled={busy} value={draft.name} onChange={event=>setDraft({...draft,name:event.target.value})}/></label>:<details className="remote-name"><summary>{t('settings.machineOptional')}</summary><label>{t('settings.machineName')}<input maxLength={80} placeholder={t('settings.remoteAutomaticName')} autoComplete="off" disabled={busy} value={draft.name} onChange={event=>setDraft({...draft,name:event.target.value})}/></label></details>}
    {selected&&<><details><summary>{t('settings.botEndpointSettings')}</summary><p className="settings-note">{selected?.checkedAt?t('settings.botCheckedAt',{time:new Date(selected.checkedAt).toLocaleTimeString()}):t('settings.botNotChecked')}</p><p className="settings-note">{t('settings.botRePairNote')}</p><p className="settings-note">{t('settings.botRemoveNote')}</p></details>{selected?.issue&&<p className="settings-note">{problem(selected.issue,false,selected.name)}</p>}{restoreVisible&&<button type="button" disabled={busy} onClick={()=>void (needsWorkRecovery?recover():connect(true))}>{t('settings.botCheckRestore')}</button>}</>}
    {recovery&&!newAuthorization&&<div className="settings-note" role="status"><p>{t(recovery.canAuthorize?'settings.botNewAuthorizationNote':'settings.botOriginalPairingUnavailable')}</p>{recovery.canAuthorize&&<button type="button" disabled={busy} onClick={beginAuthorization}>{t('settings.botNewAuthorization')}</button>}</div>}
    {newAuthorization&&<p className="settings-note" role="status">{t('settings.botNewAuthorizationNote')}</p>}
    {dialogError&&<p role="alert" className="inline-error">{dialogError}</p>}
    <div className="setup-end">{selected&&<button type="button" className="text-action" disabled={busy} onClick={()=>void remove()}>{t('settings.machineRemove')}</button>}<button type="button" disabled={busy} onClick={onClose}>{t('common.cancel')}</button><button type="submit" className="primary" disabled={busy}>{t(busy?'settings.machineConnecting':newAuthorization||needsAuthorization?'settings.botAuthorize':selected?'settings.botSaveConnection':'settings.machineConnect')}</button></div>
   </form>
   <PairingPanel call={call}/>
  </SettingsDialog>;
}

// Code exchange lives inside Add machine, not in a local-sharing page/switch.
export function PairingPanel({call=backend}:{call?:typeof backend}) {
 const {t}=useI18n();
 const [sharing,setSharing]=useState<WorkerSharing|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(''),[copied,setCopied]=useState(false),[address,setAddress]=useState('');
 const [now,setNow]=useState(()=>Date.now());const pending=useRef(false),epoch=useRef(0);
 useEffect(()=>{let live=true,reading=false;const reload=async()=>{if(reading||pending.current)return;reading=true;const generation=epoch.current;try{const v=await call<WorkerSharing>('WorkerSharing');if(live&&generation===epoch.current){setSharing(old=>({...v,...(old?.code&&old.expiresAt===v.expiresAt&&old.peer===v.peer?{code:old.code}:{})}));setError(old=>old==='read'?'':old)}}catch{if(live&&generation===epoch.current)setError('read')}finally{reading=false}};void reload();const timer=window.setInterval(()=>{setNow(Date.now());void reload()},4000);return()=>{live=false;window.clearInterval(timer)}},[call]);
 const change=async(action:()=>Promise<WorkerSharing>)=>{if(pending.current)return;pending.current=true;epoch.current++;setBusy(true);setError('');setCopied(false);try{const next=await action();setSharing(next);setAddress(next.address)}catch(cause){setError(cause instanceof Error?cause.message:String(cause))}finally{pending.current=false;setBusy(false)}};
 const live=!!sharing?.code&&!!sharing.expiresAt&&sharing.expiresAt*1000>now;
 const copy=async()=>{if(!sharing?.code||!live)return;try{await desktop('CopyText',JSON.stringify({address:address||sharing.address,code:sharing.code}));setCopied(true)}catch{setError('copy')}};
 const loopback=(()=>{try{return ['localhost','127.0.0.1','[::1]'].includes(new URL(address||sharing?.address||'').hostname)}catch{return false}})();
 return <section className="remote-pairing-step">
  <p className="settings-note">{t('settings.botPairingStep')}</p>
  <button type="button" disabled={busy} onClick={()=>void change(()=>call<WorkerSharing>('BeginWorkerPairing'))}>{t('settings.botGeneratePairing')}</button>
  {sharing?.code&&<div className="remote-pairing-info">
   {live?<><label>{t('settings.botAddress')}<select aria-label={t('settings.botAddress')} disabled={busy} value={address||sharing.address} onChange={event=>{setAddress(event.target.value);setCopied(false)}}>{[...new Set([sharing.address,...sharing.addresses||[]])].map(value=><option key={value} value={value}>{value}</option>)}</select></label><div className="remote-code-row"><code>{sharing.code}</code><button type="button" disabled={busy} onClick={()=>void copy()}>{t(copied?'settings.botCopied':'settings.botCopyPairing')}</button></div><small>{t('settings.botPairingExpires',{time:new Date(sharing.expiresAt!*1000).toLocaleTimeString()})}</small>{loopback&&<p className="settings-note">{t('settings.botLoopbackAddressNote')}</p>}</>:<p role="status">{t('settings.botPairingExpired')}</p>}
  </div>}
  {error&&<p role="alert" className="inline-error">{t(error==='copy'?'settings.botCopyFailed':error==='read'?'settings.botReadFailed':error.includes('listener')?'settings.botListenerUnavailable':'settings.botWorkerNotReady')}</p>}
  {!!sharing?.sources?.length&&<details className="remote-authorizations"><summary>{t('settings.botConnectedSources')}</summary><ul className="remote-source-list">{sharing.sources.map(source=><li key={source.id}><span><strong>{source.name||t('settings.botConnectedSource')}</strong><small>{source.id.slice(-8)}</small></span><button type="button" disabled={busy} onClick={()=>void change(async()=>{await call('RevokeWorkerPeer',source.id);const next=await call<WorkerSharing>('WorkerSharing');return {...next,...(next.expiresAt===sharing.expiresAt?{code:sharing.code}:{})}})}>{t('settings.botRevoke')}</button></li>)}</ul><p className="settings-note">{t('settings.botRevokeDirection')}</p></details>}
 </section>;
}
