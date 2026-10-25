import {useEffect, useRef, useState} from 'react';
import {backend, desktop} from '../../desktop';
import type {BotConnectionInput, Machine, WorkerSharing, WorkerSharingInput} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SettingsDialog} from './SettingsDialog';

type BotRecovery={operationId:string;profileId:string;canAuthorize:boolean};
const emptyConnection = (): BotConnectionInput => ({id:'bot-'+Array.from(crypto.getRandomValues(new Uint8Array(16)),v=>v.toString(16).padStart(2,'0')).join(''),name:'',address:'',code:''});

export function BotConnectionDialog({machines,initial,call=backend,onClose,onSaved,onChanged}:{machines:Machine[];initial?:Machine;call?:typeof backend;onClose:()=>void;onSaved:()=>void;onChanged:()=>Promise<void>}) {
 const {t}=useI18n();
 const [draft,setDraft]=useState<BotConnectionInput|null>(()=>initial?{id:initial.id,name:initial.name,address:initial.address,code:''}:emptyConnection());
 const [busy,setBusy]=useState(false),[dialogError,setDialogError]=useState('');
 const [recovery,setRecovery]=useState<BotRecovery|null>(null),[newAuthorization,setNewAuthorization]=useState(false);
 const pending=useRef(false);
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
 const changeAddress=(address:string,code?:string)=>{
  try{const info=JSON.parse(address);if(typeof info.address==='string'&&typeof info.code==='string'){address=info.address;code=info.code}}catch{/* Address entry. */}
  if(!draft)return;
  const canonical=(value:string)=>{try{return new URL(value).origin}catch{return value.trim().replace(/\/$/,'')}};
  const existing=!selected?machines.find(machine=>machine.connectionType==='bot'&&canonical(machine.address)===canonical(address)):undefined;
  setDraft({...draft,...(existing?{id:existing.id,name:draft.name||existing.name}:{}),address,...(code!==undefined?{code}:{})});
 };

 const needsAuthorization=/revoked|authentication_required|original_peer_proof_required/.test(selected?.issue||'');
 const needsWorkRecovery=!!selected?.issue?.includes('worker_not_ready');
 const restoreVisible=!needsAuthorization&&!recovery&&!newAuthorization&&!!draft?.id&&!!selected&&(selected.archived||selected.state!=='ready')&&draft.address===selected.address&&!draft.code.trim();
 const connect=async(reconnect=false)=>{
  if(!draft||pending.current)return;pending.current=true;setBusy(true);setDialogError('');
  try{
   if(newAuthorization)await call<Machine>('AuthorizeBotConnection',{connection:{...draft,reconnect:true},originalOperationId:recovery?.operationId||''});
   else await call<Machine>('ConnectBot',{...draft,reconnect});
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
 return draft&&<SettingsDialog title={t(selected?'settings.botEdit':'settings.botAdd')} description={t('settings.botConnectNote')} busy={busy} onClose={onClose}>
   <form onSubmit={event=>{event.preventDefault();void connect();}}>
    <label>{t('settings.botConnectionInfo')}<input autoFocus required type="text" inputMode="url" autoComplete="off" placeholder="http://127.0.0.1:29317" disabled={busy} value={draft.address} onChange={event=>changeAddress(event.target.value)} onPaste={event=>{
     try{const info=JSON.parse(event.clipboardData.getData('text'));if(typeof info.address==='string'&&typeof info.code==='string'){event.preventDefault();changeAddress(info.address,info.code);}}catch{/* Ordinary address paste remains unchanged. */}
    }}/></label>
    <label>{t('settings.botCode')}<input required={newAuthorization||needsAuthorization||!selected} type="password" autoComplete="off" disabled={busy} value={draft.code} onChange={event=>setDraft({...draft,code:event.target.value})}/></label>
    <label>{t('settings.machineName')}<input maxLength={80} placeholder={t('settings.remoteAutomaticName')} autoComplete="off" disabled={busy} value={draft.name} onChange={event=>setDraft({...draft,name:event.target.value})}/></label>
    {selected&&<><details><summary>{t('settings.botEndpointSettings')}</summary><p className="settings-note">{selected?.checkedAt?t('settings.botCheckedAt',{time:new Date(selected.checkedAt).toLocaleTimeString()}):t('settings.botNotChecked')}</p><p className="settings-note">{t('settings.botRePairNote')}</p><p className="settings-note">{t('settings.botRemoveNote')}</p></details>{selected?.issue&&<p className="settings-note">{problem(selected.issue,false,selected.name)}</p>}{restoreVisible&&<button type="button" disabled={busy} onClick={()=>void (needsWorkRecovery?recover():connect(true))}>{t('settings.botCheckRestore')}</button>}</>}
    {recovery&&!newAuthorization&&<div className="settings-note" role="status"><p>{t(recovery.canAuthorize?'settings.botNewAuthorizationNote':'settings.botOriginalPairingUnavailable')}</p>{recovery.canAuthorize&&<button type="button" disabled={busy} onClick={beginAuthorization}>{t('settings.botNewAuthorization')}</button>}</div>}
    {newAuthorization&&<p className="settings-note" role="status">{t('settings.botNewAuthorizationNote')}</p>}
    {dialogError&&<p role="alert" className="inline-error">{dialogError}</p>}
    <div className="setup-end">{selected&&<button type="button" className="text-action" disabled={busy} onClick={()=>void remove()}>{t('settings.machineRemove')}</button>}<button type="button" disabled={busy} onClick={onClose}>{t('common.cancel')}</button><button type="submit" className="primary" disabled={busy}>{t(busy?'settings.machineConnecting':newAuthorization||needsAuthorization?'settings.botAuthorize':selected?'settings.botSaveConnection':'settings.machineConnect')}</button></div>
   </form>
  </SettingsDialog>;
}

export function BotSharing({active=true,call=backend}:{active?:boolean;call?:typeof backend}) {
 const {t}=useI18n();
 const [sharing,setSharing]=useState<WorkerSharing|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(''),[copied,setCopied]=useState(false);
 const [endpoint,setEndpoint]=useState<WorkerSharingInput|null>(null),[now,setNow]=useState(()=>Date.now());
 const pending=useRef(false),epoch=useRef(0);
 const reload=async()=>{const generation=epoch.current;const status=await call<WorkerSharing>('WorkerSharing');if(generation!==epoch.current)return;setSharing(old=>({...status,...(old?.code&&old.expiresAt&&old.expiresAt*1000>Date.now()&&status.enabled&&old.peer===status.peer?{code:old.code,expiresAt:old.expiresAt}:{})}));};
 useEffect(()=>{if(!active)return;void reload().catch(()=>setError('read'));const timer=window.setInterval(()=>{setNow(Date.now());if(!pending.current)void reload().catch(()=>{});},4000);return()=>{epoch.current++;window.clearInterval(timer);};},[active,call]);
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

 const change=async(action:()=>Promise<WorkerSharing>)=>{if(pending.current)return;pending.current=true;setBusy(true);setError('');setCopied(false);try{setSharing(await action());}catch(cause){setError(cause instanceof Error?cause.message:String(cause));}finally{pending.current=false;setBusy(false)}};
 const pairingLive=!!sharing?.code&&!!sharing.expiresAt&&sharing.expiresAt*1000>now;
 const copy=async()=>{if(!sharing?.code||!pairingLive)return;try{await desktop('CopyText',JSON.stringify({address:sharing.address,code:sharing.code}));setCopied(true);}catch{setError('copy')}};
 const saveEndpoint=async()=>{if(!endpoint)return;await change(async()=>{const result=await call<WorkerSharing>('SaveWorkerSharing',endpoint);setEndpoint(null);return result;});};
 return <section className="remote-sharing">
  <div className="remote-sharing-toggle"><label htmlFor="remote-worker-sharing">{t('settings.botSharing')}</label><input id="remote-worker-sharing" className="settings-switch" role="switch" type="checkbox" checked={!!sharing?.enabled} disabled={busy||sharing===null} onChange={event=>void change(()=>call<WorkerSharing>('SetWorkerSharing',event.target.checked))}/></div>
  <p className="settings-note">{t('settings.botSharingNote')}</p>
  {sharing?.enabled&&<>
   <div className="remote-sharing-actions"><button disabled={busy} onClick={()=>void change(()=>call<WorkerSharing>('CreateWorkerPairing'))}>{t('settings.botGeneratePairing')}</button></div>
   {sharing.code&&<div className="remote-pairing-info">
    {pairingLive?<><dl><div><dt>{t('settings.botAddress')}</dt><dd>{sharing.address}</dd></div><div><dt>{t('settings.botCode')}</dt><dd><code>{sharing.code}</code></dd></div></dl>{(()=>{try{return ['localhost','127.0.0.1','[::1]'].includes(new URL(sharing.address).hostname)}catch{return false}})()&&<p className="settings-note">{t('settings.botLoopbackAddressNote')}</p>}<div className="remote-copy-row"><small>{t('settings.botPairingExpires',{time:new Date(sharing.expiresAt!*1000).toLocaleTimeString()})}</small><button disabled={busy} onClick={()=>void copy()}>{t(copied?'settings.botCopied':'settings.botCopyPairing')}</button></div></>:<p role="status">{t('settings.botPairingExpired')}</p>}
   </div>}
   {!!sharing.sources?.length&&<><h3>{t('settings.botConnectedSources')}</h3><ul className="remote-source-list">{sharing.sources.map(source=><li key={source.id}><span><strong>{source.name||t('settings.botConnectedSource')}</strong><small>{source.id.slice(-8)}</small></span><button disabled={busy} onClick={()=>void change(async()=>{await call('RevokeWorkerPeer',source.id);return await call<WorkerSharing>('WorkerSharing');})}>{t('settings.botRevoke')}</button></li>)}</ul><p className="settings-note">{t('settings.botRevokeDirection')}</p></>}
  </>}
  <details className="remote-endpoint-details"><summary>{t('settings.botEndpointSettings')}</summary><dl className="machine-connection-values"><div><dt>{t('settings.botListener')}</dt><dd>{sharing?.listen||'—'}</dd></div><div><dt>{t('settings.botReachableEndpoint')}</dt><dd>{sharing?.address||'—'}</dd></div></dl><button disabled={busy||!sharing} onClick={()=>{setError('');setEndpoint({listen:sharing!.listen,address:sharing!.address});}}>{t('settings.botEditAddress')}</button><p className="settings-note">{t('settings.botEndpointNote')}</p></details>
  {error&&<div role="alert" className="inline-error"><p>{error==='read'?t('settings.botReadFailed'):error==='copy'?t('settings.botCopyFailed'):problem(error,true)}</p>{error.includes('worker_not_ready')&&<button disabled={busy} onClick={()=>void change(()=>call<WorkerSharing>('RecoverWorkerSharing'))}>{t('settings.botCheckRestore')}</button>}</div>}
  {endpoint&&<SettingsDialog title={t('settings.botEndpointSettings')} busy={busy} onClose={()=>setEndpoint(null)}><form onSubmit={event=>{event.preventDefault();void saveEndpoint()}}>
   <label>{t('settings.botListener')}<input required disabled={busy||sharing?.managed} value={endpoint.listen} onChange={event=>setEndpoint({...endpoint,listen:event.target.value})}/></label>
   <label>{t('settings.botReachableEndpoint')}<input type="url" disabled={busy} value={endpoint.address} onChange={event=>setEndpoint({...endpoint,address:event.target.value})} placeholder="http://100.64.0.1:9317"/></label>
   <p className="settings-note">{t('settings.botReachableAddress')}</p>
   <div className="setup-end"><button type="button" disabled={busy} onClick={()=>setEndpoint(null)}>{t('common.cancel')}</button><button type="submit" className="primary" disabled={busy}>{t('settings.botSaveConnection')}</button></div>
  </form></SettingsDialog>}
 </section>;
}
