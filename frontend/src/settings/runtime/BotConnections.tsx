import {useRef,useState} from 'react';
import {backend} from '../../desktop';
import type {BotConnectionInput,Machine,WorkerSharing} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SettingsDialog} from './SettingsDialog';
import {botAddress,botProfileAt} from './bot-address';
import {pairingProblem,connectionProblem,incomingProblem} from './pairing-errors';

export function BotConnectionDialog({machines,initial,call=backend,onClose,onSaved,onChanged}:{machines:Machine[];initial?:Machine;call?:typeof backend;onClose:()=>void;onSaved:()=>void;onChanged:(fresh?:Machine)=>Promise<void>}){
 const {t}=useI18n();
 const [draft,setDraft]=useState<BotConnectionInput>(()=>({id:initial?.id||'bot-'+crypto.randomUUID(),name:initial?.name||'',address:initial?.address||''}));
 const [port,setPort]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('');const pending=useRef(false);
 const selected=machines.find(v=>v.id===draft.id)||initial;
 const run=async(action:()=>Promise<unknown>)=>{if(pending.current)return;pending.current=true;setBusy(true);setError('');try{await action()}catch(cause){const recovery=(cause as {botRecovery?:{profileId:string}})?.botRecovery;if(recovery?.profileId)setDraft(old=>({...old,id:recovery.profileId}));setError(cause instanceof Error?cause.message:'unclassified');await onChanged()}finally{pending.current=false;setBusy(false)}};
 const connect=()=>run(async()=>{
  const address=botAddress(draft.address,port),known=botProfileAt(machines,draft.address,port);
  await call<Machine>('ConnectBot',{...draft,id:known?.id||draft.id,address,reconnect:!!known?.archived||!!selected&&selected.pairingStatus!=='approved'});onSaved();
 });
 const check=()=>run(async()=>{const address=botAddress(draft.address,port);const fresh=selected&&address!==botAddress(selected.address)?await call<Machine>('ConnectBot',{...draft,address,reconnect:true}):await call<Machine>('InspectMachine',draft.id,'');await onChanged(fresh)});
 const recover=()=>run(async()=>{const fresh=await call<Machine>('RecoverBotWork',draft.id);await onChanged(fresh)});
 const remove=()=>run(async()=>{await call('RemoveMachine',draft.id);onSaved()});
 const status=selected?.pairingStatus,unsent=selected?.issue==='pairing_request_not_dispatched',uncertain=status==='unknown'&&!unsent,waiting=status==='pending';
 const problem=connectionProblem(error);
 return <SettingsDialog title={initial?t('settings.remoteManageTitle'):t('settings.remoteAdd')} className="remote-bot-dialog" busy={busy} onClose={onClose}>
  <form onSubmit={e=>{e.preventDefault();if(!uncertain&&!waiting)void connect()}}>
   <div className="remote-endpoint-fields"><label>{t('settings.remoteAddress')}<input autoFocus required autoComplete="off" spellCheck={false} placeholder={t('settings.remoteAddressPlaceholder')} value={draft.address} onChange={e=>setDraft({...draft,address:e.target.value})} disabled={busy}/></label><label>{t('settings.remotePort')}<input type="text" inputMode="numeric" pattern="[0-9]*" maxLength={5} autoComplete="off" placeholder={t('settings.remoteDefaultPort')} value={port} onChange={e=>setPort(e.target.value)} disabled={busy}/></label></div>
   {initial&&<label>{t('settings.machineName')}<input maxLength={80} value={draft.name} onChange={e=>setDraft({...draft,name:e.target.value})} disabled={busy||waiting||uncertain}/></label>}
   {unsent&&<p className="remote-dialog-status" role="status">{t('settings.remoteNotSent')}</p>}
   {waiting&&<p className="remote-dialog-status" role="status">{t('settings.remoteAwaitingApproval')}</p>}
   {uncertain&&<p className="remote-dialog-status" role="status">{t('settings.remoteRequestUnknown')}</p>}
   {status&&['rejected','expired','revoked'].includes(status)&&<p className="remote-dialog-status">{t(status==='rejected'?'settings.remoteRejected':status==='expired'?'settings.remoteExpired':'settings.remoteRevoked')}</p>}
   {status==='approved'&&selected?.issue==='worker_not_ready'&&<div className="remote-inline-state"><span>{t('settings.remoteWorkUnavailable')}</span><button type="button" className="text-action" disabled={busy} onClick={()=>void recover()}>{t('settings.remoteRecoverWork')}</button></div>}
   {error&&<p role="alert" className="remote-dialog-error">{t(problem.key)}</p>}
   <div className="remote-dialog-footer">{selected&&<button type="button" className="text-action remote-remove" disabled={busy} onClick={()=>void remove()}>{t('settings.machineRemove')}</button>}<button type="button" disabled={busy} onClick={onClose}>{t(selected?'common.close':'common.cancel')}</button>{uncertain||waiting?<button key="check-original" type="button" className="primary" disabled={busy} onClick={event=>{event.preventDefault();void check()}}>{t(busy?'settings.remoteChecking':'settings.machineRecheck')}</button>:<button key="send-explicit" type="submit" className="primary" disabled={busy||!draft.address.trim()}>{t(busy?'settings.machineConnecting':initial&&status==='approved'?'settings.botSaveConnection':'settings.remoteSendRequest')}</button>}</div>
  </form>
 </SettingsDialog>;
}
export function MachineRequestsDialog({value,readError=false,call=backend,onClose,onChanged}:{value:WorkerSharing|null;readError?:boolean;call?:typeof backend;onClose:()=>void;onChanged:()=>Promise<void>}){
 const {t}=useI18n();const [busy,setBusy]=useState(''),[error,setError]=useState('');const pending=useRef(false);
 const decide=async(id:string,allow:boolean)=>{if(pending.current)return;pending.current=true;setBusy(id);setError('');try{await call('DecideWorkerPairing',{id,allow});await onChanged()}catch(cause){setError(cause instanceof Error?cause.message:'unclassified');await onChanged()}finally{pending.current=false;setBusy('')}};
 const problem=pairingProblem(error),requests=value?.requests?.filter(v=>v.expiresAt*1000>Date.now())||[];
 const settings=()=>{onClose();requestAnimationFrame(()=>window.dispatchEvent(new CustomEvent('settings-open',{detail:{section:'connections'}})))};
 return <SettingsDialog title={t('settings.remoteIncoming')} className="remote-requests-dialog" busy={!!busy} onClose={onClose}>
  {readError||value?.issue?<div className="remote-inline-state" role="status"><span>{t(readError?'settings.remoteInboxUnreadable':incomingProblem(value?.issue||''))}</span><button disabled={!!busy} onClick={()=>void onChanged()}>{t('settings.remoteRetry')}</button></div>:!value?<p className="remote-dialog-status" role="status">{t('settings.remoteLoading')}</p>:!requests.length?<p className="remote-dialog-status">{t('settings.remoteNoRequests')}</p>:<ul className="remote-request-list">{requests.map(request=><li key={request.id}><div className="remote-request-identity"><strong>{request.name||t('settings.botConnectedSource')}</strong><span>{request.address||t('settings.remoteSourceUnknown')}</span></div><p className="remote-request-direction">{t('settings.remoteRequestDirection')}</p><div className="remote-request-fingerprint"><span>{t('settings.remoteSourceFingerprint')}</span><code>{request.fingerprint.match(/.{1,8}/g)?.join(' ')}</code></div><div className="remote-request-actions"><button disabled={!!busy} onClick={()=>void decide(request.id,false)}>{t('settings.remoteDenyRequest')}</button><button className="primary" disabled={!!busy} onClick={()=>void decide(request.id,true)}>{t('settings.remoteAllowRequest')}</button></div></li>)}</ul>}
  {error&&<div className="remote-dialog-error" role="alert"><p>{t(problem.key)}</p>{problem.settings&&<button className="text-action" onClick={settings}>{t('settings.remoteOpenWorkerSettings')}</button>}</div>}
 </SettingsDialog>;
}
