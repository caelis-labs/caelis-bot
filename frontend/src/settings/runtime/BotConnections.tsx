import {useRef,useState} from 'react';
import {backend} from '../../desktop';
import type {BotConnectionInput,Machine,WorkerSharing,SSHTarget} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SSHAuthentication,useSSHConnect,sshAddress,sshTarget,sshProblem} from './SSHConnection';
import {SetupButton} from '../../SetupLoading';
import {SettingsDialog} from './SettingsDialog';
import {SSHFields} from './SSHFields';
import {PortInput} from './PortInput';
import {botAddress,botAddressPort} from './bot-address';
import {pairingProblem,connectionProblem,incomingProblem} from './pairing-errors';

export function BotConnectionDialog({machines,initial,call=backend,onClose,onSaved,onChanged}:{machines:Machine[];initial?:Machine;call?:typeof backend;onClose:()=>void;onSaved:(fresh?:Machine)=>void;onChanged:(fresh?:Machine)=>Promise<void>}){
 const {t}=useI18n();const legacy=initial?.state==='legacy';const creation=useRef(!initial||legacy);
 const [draft,setDraft]=useState<BotConnectionInput>(()=>({id:(!legacy&&initial?.id)||'',name:legacy?'':initial?.name||'',address:initial?.savedAddress||initial?.address||''}));
 const initialSSH=sshTarget(initial?.savedAddress||initial?.address||'',legacy&&!initial?.savedAddress);
 const [method,setMethod]=useState(initialSSH?'ssh':'address');
 const [sshDraft,setSSHDraft]=useState<SSHTarget>(()=>initialSSH||{host:'',port:9317});
 const [port,setPort]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('');const pending=useRef(false);
 const selected=machines.find(v=>v.id===draft.id)||(!legacy?initial:undefined);
 const run=async(action:()=>Promise<unknown>)=>{if(pending.current)return;pending.current=true;setBusy(true);setError('');try{await action()}catch(cause){const recovery=(cause as {botRecovery?:{profileId:string}})?.botRecovery;if(recovery?.profileId)setDraft(old=>({...old,id:recovery.profileId}));setError(cause instanceof Error?cause.message:'unclassified');await onChanged()}finally{pending.current=false;setBusy(false)}};
 const endpoint=()=>method==='ssh'?sshAddress(sshDraft):botAddress(draft.address,port);
 const connect=()=>{let address:string;try{address=endpoint()}catch(cause){setError(cause instanceof Error?cause.message:'invalid_bot_port');return Promise.resolve()}return run(async()=>{
  const known=machines.find(v=>v.state!=='legacy'&&v.address===address);
  const fresh=await call<Machine>('ConnectBot',{...draft,id:known?.id||draft.id,address,reconnect:!!known?.archived||!!selected&&selected.pairingStatus!=='approved'});setDraft(old=>({...old,id:fresh.id,name:fresh.name,address:fresh.address}));onSaved(!creation.current&&selected?.pairingStatus==='approved'?undefined:fresh);
 })};
 const applySaved=!!selected?.savedAddress&&selected.savedAddress!==selected.address;
 const check=()=>run(async()=>{const fresh=await call<Machine>('InspectMachine',draft.id,'');await onChanged(fresh)});
 const recover=()=>run(async()=>{const fresh=await call<Machine>('RecoverBotWork',draft.id);await onChanged(fresh)});
 const status=selected?.pairingStatus,unsent=selected?.issue==='pairing_request_not_dispatched',uncertain=status==='unknown'&&!unsent,waiting=status==='pending';
 const problem=connectionProblem(error),approvedUnready=status==='approved'&&selected?.state!=='ready';
 const afterSSH=useRef<'connect'|'check'>('connect');
 const ssh=useSSHConnect(call,async()=>{if(afterSSH.current==='check')await check();else await connect()});
 const submit=()=>{if(blocked)return;if(method==='ssh'){afterSSH.current='connect';void ssh.start(sshDraft)}else void connect()};
 const blocked=busy||ssh.working;
 const connected=creation.current&&status==='approved'&&!approvedUnready;

 return <SettingsDialog title={creation.current?t('settings.remoteAdd'):t('settings.remoteConnect')} className="remote-bot-dialog" busy={busy} onClose={onClose}>
  <form onSubmit={e=>{e.preventDefault();if((!uncertain&&!waiting)||applySaved)submit()}}>
   {creation.current&&!status&&<div className="remote-methods" role="group" aria-label={t('settings.sshConnectionMethod')}><button type="button" aria-pressed={method==='address'} disabled={blocked} onClick={()=>setMethod('address')}>{t('settings.sshAddressMethod')}</button><button type="button" aria-pressed={method==='ssh'} disabled={blocked} onClick={()=>setMethod('ssh')}>SSH</button></div>}
   <div className="remote-add-body">
   {((!waiting&&!uncertain)||applySaved)&&!ssh.status?.prompt&&!(creation.current&&status==='approved')&&<>
   {method==='ssh'?<SSHFields value={sshDraft} onChange={setSSHDraft} disabled={blocked} call={call}/>:<div className="remote-endpoint-row">
    <label>{t('settings.remoteAddress')}<input autoFocus required autoComplete="off" spellCheck={false} placeholder={t('settings.remoteAddressPlaceholder')} value={draft.address} onChange={e=>setDraft({...draft,address:e.target.value})} disabled={blocked}/></label>
    <label>{t('settings.sshBotPort')}<PortInput placeholder={botAddressPort(draft.address)} value={port} onChange={setPort} disabled={blocked}/></label>
   </div>}
   </>}
   <SSHAuthentication ssh={ssh}/>
   {ssh.error&&<p role="alert" className="remote-dialog-error">{t(sshProblem(ssh.error))}</p>}

   {!ssh.status?.prompt&&(!creation.current||!status)&&<label>{t('settings.machineIdentifier')}<input placeholder={t('settings.machineIdentifierAuto')} maxLength={80} value={draft.name} onChange={e=>setDraft({...draft,name:e.target.value})} disabled={blocked||waiting||uncertain}/></label>}
   {creation.current&&status==='approved'&&<div className="remote-connection-state" role="status"><strong>{t(approvedUnready?'settings.remoteAuthorizedUnavailable':'settings.remoteConnectedTitle')}</strong><p>{approvedUnready?t(selected?.issue==='worker_not_ready'?'settings.remoteWorkUnavailable':connectionProblem(selected?.issue||'').key):(selected?.name||'')}</p></div>}
   {unsent&&<p className="remote-dialog-status" role="status">{t('settings.remoteNotSent')}</p>}
   {waiting&&<div className="remote-connection-state" role="status"><strong>{t('settings.remotePendingTitle')}</strong><p>{t('settings.remoteAwaitingApproval')}</p></div>}
   {uncertain&&<p className="remote-dialog-status" role="status">{t('settings.remoteRequestUnknown')}</p>}
   {status&&['rejected','expired','revoked'].includes(status)&&<p className="remote-dialog-status">{t(status==='rejected'?'settings.remoteRejected':status==='expired'?'settings.remoteExpired':'settings.remoteRevoked')}</p>}
   {status==='approved'&&selected?.issue==='worker_not_ready'&&<div className="remote-inline-state">{!creation.current&&<span>{t('settings.remoteWorkUnavailable')}</span>}<button type="button" className="text-action" disabled={blocked} onClick={()=>void recover()}>{t('settings.remoteRecoverWork')}</button></div>}
   {error&&<p role="alert" className="remote-dialog-error">{t(problem.key)}</p>}
   </div>
   {!ssh.status?.prompt&&<div className="remote-dialog-footer">{!connected&&<button type="button" disabled={busy} onClick={onClose}>{t(waiting||uncertain||creation.current&&status==='approved'?'common.close':'common.cancel')}</button>}{applySaved&&(uncertain||waiting)&&<SetupButton type="submit" busy={blocked} className="primary" disabled={blocked}>{t('settings.remoteApplyAddress')}</SetupButton>}{uncertain||waiting||(creation.current&&approvedUnready)?<SetupButton key="check-original" type="button" busy={blocked} className={applySaved?undefined:"primary"} disabled={blocked} onClick={event=>{event.preventDefault();if(method==='ssh'){afterSSH.current='check';void ssh.start(sshTarget(selected?.address||'')||sshDraft)}else void check()}}>{t(waiting||uncertain?'settings.remoteCheckApproval':'settings.machineRecheck')}</SetupButton>:creation.current&&status==='approved'?<button type="button" className="primary" onClick={onClose}>{t('settings.remoteDone')}</button>:<SetupButton key="send-explicit" type="submit" busy={blocked} className="primary" disabled={blocked||!(method==='ssh'?sshDraft.host.trim():draft.address.trim())||method==='ssh'&&sshDraft.authentication==='key'&&!sshDraft.identity}>{t('settings.remoteConnect')}</SetupButton>}</div>}
  </form>
 </SettingsDialog>;
}
export function MachineRequestsDialog({requestID,value,readError=false,call=backend,onClose,onChanged}:{requestID?:string;value:WorkerSharing|null;readError?:boolean;call?:typeof backend;onClose:()=>void;onChanged:()=>Promise<void>}){
 const {t}=useI18n();const [busy,setBusy]=useState(''),[error,setError]=useState(''),[decision,setDecision]=useState<{allow:boolean;name:string}>();const pending=useRef(false);
 const decide=async(id:string,allow:boolean)=>{if(pending.current)return;pending.current=true;setBusy(id);setError('');try{await call('DecideWorkerPairing',{id,allow});setDecision({allow,name:value?.requests.find(v=>v.id===id)?.name||t('settings.botConnectedSource')});await onChanged()}catch(cause){setError(cause instanceof Error?cause.message:'unclassified');await onChanged()}finally{pending.current=false;setBusy('')}};
 const problem=pairingProblem(error),requests=value?.requests?.filter(v=>v.expiresAt*1000>Date.now()&&(!requestID||v.id===requestID))||[];
 const settings=()=>{onClose();requestAnimationFrame(()=>window.dispatchEvent(new CustomEvent('settings-open',{detail:{section:'connections'}})))};
 return <SettingsDialog title={t('settings.remoteIncoming')} className="remote-requests-dialog" busy={!!busy} onClose={onClose}>
  {decision?<div className="remote-connection-state" role="status"><strong>{t(decision.allow?'settings.remoteAllowed':'settings.remoteRequestDenied')}</strong><p>{t(decision.allow?'settings.remotePairAllowedHelp':'settings.remotePairDeniedHelp',{name:decision.name})}</p></div>:readError||value?.issue?<div className="remote-inline-state" role="status"><span>{t(readError?'settings.remoteInboxUnreadable':incomingProblem(value?.issue||''))}</span><button disabled={!!busy} onClick={()=>void onChanged()}>{t('settings.remoteRetry')}</button></div>:!value?<p className="remote-dialog-status" role="status">{t('settings.remoteLoading')}</p>:!requests.length?<p className="remote-dialog-status">{t(requestID?'settings.remoteRequestClosed':'settings.remoteNoRequests')}</p>:<ul className="remote-request-list">{requests.map(request=><li key={request.id}><div className="remote-request-identity"><strong>{request.name||t('settings.botConnectedSource')}</strong><span>{request.address||t('settings.remoteSourceUnknown')}</span></div><p className="remote-request-direction">{t('settings.remoteRequestDirection')}</p><div className="remote-request-fingerprint"><span>{t('settings.remoteSourceFingerprint')}</span><code>{request.fingerprint.match(/.{1,8}/g)?.join(' ')}</code></div><div className="remote-request-actions"><button disabled={!!busy} onClick={()=>void decide(request.id,false)}>{t('settings.remoteDenyRequest')}</button><button className="primary" disabled={!!busy} onClick={()=>void decide(request.id,true)}>{t('settings.remoteAllowRequest')}</button></div></li>)}</ul>}
  {decision&&<div className="remote-dialog-footer"><button className="primary" onClick={onClose}>{t('settings.remoteDone')}</button></div>}
  {error&&<div className="remote-dialog-error" role="alert"><p>{t(problem.key)}</p>{problem.settings&&<button className="text-action" onClick={settings}>{t('settings.remoteOpenWorkerSettings')}</button>}</div>}
 </SettingsDialog>;
}
