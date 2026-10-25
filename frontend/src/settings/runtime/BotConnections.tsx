import {useRef,useState} from 'react';
import {backend} from '../../desktop';
import type {BotConnectionInput,Machine,WorkerSharing} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SettingsChevron} from '../../SettingsIcons';
import {SettingsDialog} from './SettingsDialog';
import {botAddress,botProfileAt} from './bot-address';
import {pairingProblem} from './pairing-errors';

export function BotConnectionDialog({machines,initial,call=backend,onClose,onSaved,onChanged}:{machines:Machine[];initial?:Machine;call?:typeof backend;onClose:()=>void;onSaved:()=>void;onChanged:()=>Promise<void>}){
 const {t}=useI18n();
 const [draft,setDraft]=useState<BotConnectionInput>(()=>({id:initial?.id||'bot-'+crypto.randomUUID(),name:initial?.name||'',address:initial?.address||''}));
 const [port,setPort]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('');const pending=useRef(false);
 const selected=machines.find(v=>v.id===draft.id)||initial;
 const run=async(action:()=>Promise<unknown>)=>{if(pending.current)return;pending.current=true;setBusy(true);setError('');try{await action()}catch(cause){const recovery=(cause as {botRecovery?:{profileId:string}})?.botRecovery;if(recovery?.profileId)setDraft(old=>({...old,id:recovery.profileId}));setError(cause instanceof Error?cause.message:String(cause));await onChanged()}finally{pending.current=false;setBusy(false)}};
 const connect=()=>run(async()=>{
  const address=botAddress(draft.address,port),known=botProfileAt(machines,draft.address,port);
  await call<Machine>('ConnectBot',{...draft,id:known?.id||draft.id,address,reconnect:!!known?.archived||!!selected&&selected.pairingStatus!=='approved'});onSaved();
 });
 const check=()=>run(async()=>{await call('InspectMachine',draft.id,'');await onChanged()});
 const recover=()=>run(async()=>{await call('RecoverBotWork',draft.id);await onChanged()});
 const remove=()=>run(async()=>{await call('RemoveMachine',draft.id);onSaved()});
 const status=selected?.pairingStatus;
 const issue=error==='bot_connection_target_unconfirmed'?t('settings.remoteTargetUnconfirmed'):error==='pairing_requests_full'?t('settings.remoteRequestsFull'):error==='bot_peer_unreachable'?t('settings.botUnreachable',{name:draft.name||t('settings.botTargetMachine')}):error==='invalid_bot_address'?t('settings.botAddressInvalid'):error==='original_peer_proof_required'?t('settings.botProofRequired'):t('settings.botActionFailed');
 return <SettingsDialog title={initial?.name||t('settings.remoteAdd')} className="remote-bot-dialog" busy={busy} onClose={onClose}>
  <form onSubmit={e=>{e.preventDefault();void connect()}}>
   <label>{t('settings.remoteAddress')}<input autoFocus required autoComplete="off" placeholder="100.64.0.5" value={draft.address} onChange={e=>setDraft({...draft,address:e.target.value})} disabled={busy}/></label>
   <label>{t('settings.botOptionalPort')}<input type="number" min="1" max="65535" inputMode="numeric" autoComplete="off" placeholder="9317" value={port} onChange={e=>setPort(e.target.value)} disabled={busy}/></label>
   <details className="remote-name"><summary>{t('settings.machineName')}</summary><label>{t('settings.machineName')}<input maxLength={80} value={draft.name} onChange={e=>setDraft({...draft,name:e.target.value})} disabled={busy}/></label></details>
   {(status==='pending'||status==='unknown')?<p className="settings-note" role="status">{t(status==='pending'?'settings.remoteAwaitingApproval':'settings.remoteRequestUnknown')}</p>:status&&status!=='approved'?<p className="settings-note">{t('settings.remoteRequestClosed')}</p>:!initial&&<p className="settings-note">{t('settings.remoteApprovalNote')}</p>}
   {selected?.pairingStatus==='approved'&&selected.issue==='worker_not_ready'&&<p className="settings-note">{t('settings.remoteWorkUnavailable')} <button type="button" disabled={busy} onClick={()=>void recover()}>{t('settings.remoteRecoverWork')}</button></p>}
   {error&&<p role="alert" className="inline-error">{issue}</p>}
   <div className="setup-end">{selected&&<button type="button" className="text-action" disabled={busy} onClick={()=>void remove()}>{t('settings.machineRemove')}</button>}{selected&&<button type="button" disabled={busy} onClick={()=>void check()}>{t('settings.machineRecheck')}</button>}<button type="button" disabled={busy} onClick={onClose}>{t('common.cancel')}</button><button type="submit" className="primary" disabled={busy||!draft.address.trim()}>{t(busy?'settings.machineConnecting':initial&&status==='approved'?'settings.botSaveConnection':'settings.remoteSendRequest')}</button></div>
  </form>
 </SettingsDialog>;
}
export function MachineRequestsDialog({value,call=backend,onClose,onChanged}:{value:WorkerSharing|null;call?:typeof backend;onClose:()=>void;onChanged:()=>Promise<void>}){
 const {t}=useI18n();const [busy,setBusy]=useState(''),[error,setError]=useState('');const pending=useRef(false);
 const decide=async(id:string,allow:boolean)=>{if(pending.current)return;pending.current=true;setBusy(id);setError('');try{await call('DecideWorkerPairing',{id,allow});await onChanged()}catch(cause){setError(cause instanceof Error?cause.message:String(cause))}finally{pending.current=false;setBusy('')}};
 const problem=pairingProblem(error);
 return <SettingsDialog title={t('settings.remoteIncoming')} className="remote-requests-dialog" onClose={onClose}>
  {value?.issue&&<p role="alert" className="inline-error">{t('settings.remoteServiceUnavailable')}</p>}
  {!value?.requests?.length?<p className="settings-note">{t('settings.remoteNoRequests')}</p>:<><p className="settings-note">{t('settings.remoteRequestDirection')}</p><ul className="remote-request-list">{value.requests.map(request=><li key={request.id}><strong>{request.name||t('settings.botConnectedSource')}</strong><dl><div><dt>{t('settings.remoteSourceAddress')}</dt><dd>{request.address||'—'}</dd></div><div><dt>{t('settings.remoteSourceFingerprint')}</dt><dd><code>{request.fingerprint.match(/.{1,8}/g)?.join(' ')}</code></dd></div></dl><div className="remote-request-actions"><button disabled={!!busy} onClick={()=>void decide(request.id,false)}>{t('settings.remoteDenyRequest')}</button><button className="primary" disabled={!!busy} onClick={()=>void decide(request.id,true)}>{t('settings.remoteAllowRequest')}</button></div></li>)}</ul></>}
  {value?.address&&<details className="remote-connection-detail"><summary><SettingsChevron/>{t('settings.remoteConnectionDetails')}</summary><p>{value.address}</p></details>}
  <div className="remote-request-footer"><button type="button" onClick={()=>void onChanged()}>{t('settings.machineRecheck')}</button></div>
  {error&&<p role="alert" className="inline-error">{t(problem.key)}{problem.settings&&' '+t('settings.pairSettingsPath')}</p>}
 </SettingsDialog>;
}
