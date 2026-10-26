import {useEffect,useRef,useState} from 'react';
import {backend} from '../../desktop';
import type {BotConnectionInput,Machine,WorkerSharing,SSHConfiguration} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SSHConfigDialog,SSHAuthentication,useSSHConnect,sshEndpoint,sshTarget,sshProblem} from './SSHConnection';
import {SetupButton} from '../../SetupLoading';
import {SettingsDialog} from './SettingsDialog';
import {SettingsSelect} from '../../SettingsSelect';
import {botAddress,botAddressPort} from './bot-address';
import {pairingProblem,connectionProblem,incomingProblem} from './pairing-errors';

export function BotConnectionDialog({machines,initial,call=backend,onClose,onSaved,onChanged}:{machines:Machine[];initial?:Machine;call?:typeof backend;onClose:()=>void;onSaved:(fresh?:Machine)=>void;onChanged:(fresh?:Machine)=>Promise<void>}){
 const {t}=useI18n();const creation=useRef(!initial);
 const [draft,setDraft]=useState<BotConnectionInput>(()=>({id:initial?.id||'bot-'+crypto.randomUUID(),name:initial?.name||'',address:initial?.address||''}));
 const [method,setMethod]=useState(initial?.address.startsWith('ssh://')?'ssh':'address'),[host,setHost]=useState(sshTarget(initial?.address||'')?.host||''),[config,setConfig]=useState<SSHConfiguration>();
 const [port,setPort]=useState(sshTarget(initial?.address||'')?String(sshTarget(initial!.address)!.port):''),[busy,setBusy]=useState(false),[error,setError]=useState('');const pending=useRef(false);
 const selected=machines.find(v=>v.id===draft.id)||initial;
 const run=async(action:()=>Promise<unknown>)=>{if(pending.current)return;pending.current=true;setBusy(true);setError('');try{await action()}catch(cause){const recovery=(cause as {botRecovery?:{profileId:string}})?.botRecovery;if(recovery?.profileId)setDraft(old=>({...old,id:recovery.profileId}));setError(cause instanceof Error?cause.message:'unclassified');await onChanged()}finally{pending.current=false;setBusy(false)}};
 const endpoint=()=>method==='ssh'?sshEndpoint(host,port):botAddress(draft.address,port);
 const connect=()=>run(async()=>{
  const address=endpoint(),known=machines.find(v=>v.address===address);
  const fresh=await call<Machine>('ConnectBot',{...draft,id:known?.id||draft.id,address,reconnect:!!known?.archived||!!selected&&selected.pairingStatus!=='approved'});setDraft(old=>({...old,id:fresh.id,name:fresh.name,address:fresh.address}));onSaved(!creation.current&&selected?.pairingStatus==='approved'?undefined:fresh);
 });
 const check=()=>run(async()=>{const address=endpoint();const fresh=selected&&address!==selected.address?await call<Machine>('ConnectBot',{...draft,address,reconnect:true}):await call<Machine>('InspectMachine',draft.id,'');await onChanged(fresh)});
 const recover=()=>run(async()=>{const fresh=await call<Machine>('RecoverBotWork',draft.id);await onChanged(fresh)});
 const status=selected?.pairingStatus,unsent=selected?.issue==='pairing_request_not_dispatched',uncertain=status==='unknown'&&!unsent,waiting=status==='pending';
 const problem=connectionProblem(error),approvedUnready=status==='approved'&&selected?.state!=='ready';
 const afterSSH=useRef<'connect'|'check'>('connect');
 const ssh=useSSHConnect(call,async()=>{if(afterSSH.current==='check')await check();else await connect()});
 const configGeneration=useRef(0);
 const loadConfig=async()=>{const epoch=++configGeneration.current;try{const value=await call<SSHConfiguration>('SSHConfiguration');if(configGeneration.current===epoch)setConfig(value)}catch{if(configGeneration.current===epoch)ssh.setError('ssh_config_unreadable')}};
 useEffect(()=>{const focus=()=>{if(method==='ssh'&&!document.hidden)void loadConfig()};if(method==='ssh')void loadConfig();window.addEventListener('focus',focus);window.addEventListener('settings-focus',focus);return()=>{configGeneration.current++;window.removeEventListener('focus',focus);window.removeEventListener('settings-focus',focus)}},[method,call]);
 const submit=()=>{if(busy||ssh.working)return;if(port&&(!/^[0-9]+$/.test(port)||Number(port)<1||Number(port)>65535)){setError('invalid_bot_address');return;}if(method==='ssh'&&!(initial&&status==='approved')){afterSSH.current='connect';void ssh.start({host:host.trim(),port:Number(port)||9317})}else void connect()};
 const reconnectSSH=async()=>{if(blocked)return;afterSSH.current='check';try{await call('StopSSHConnection',{host,port:Number(port)||9317});await ssh.start({host,port:Number(port)||9317})}catch{ssh.setError('ssh_connection_failed')}};

 const blocked=busy||ssh.working;
 const [configOpen,setConfigOpen]=useState(false);
 const connected=creation.current&&status==='approved'&&!approvedUnready;

 // Replace the active dialog while retaining its draft, rather than stacking backdrops.
 if(configOpen)return <SSHConfigDialog call={call} onClose={()=>{setConfigOpen(false);void loadConfig()}} onSaved={()=>void loadConfig()}/>;

 return <SettingsDialog title={creation.current?t('settings.remoteAdd'):t('settings.remoteManageTitle')} className="remote-bot-dialog" busy={busy} onClose={onClose}>
  <form onSubmit={e=>{e.preventDefault();if(!uncertain&&!waiting)submit()}}>
   {!initial&&<div className="remote-methods" role="group" aria-label={t('settings.sshConnectionMethod')}><button type="button" aria-pressed={method==='address'} disabled={blocked} onClick={()=>setMethod('address')}>{t('settings.sshAddressMethod')}</button><button type="button" aria-pressed={method==='ssh'} disabled={blocked} onClick={()=>setMethod('ssh')}>SSH</button></div>}
   <div className="remote-add-body">
   {!waiting&&!uncertain&&!ssh.status?.prompt&&!(creation.current&&status==='approved')&&<>
   <div className="remote-endpoint-row">
   <label>{t(method==='ssh'?'settings.sshHost':'settings.remoteAddress')}{method==='ssh'?<SettingsSelect aria-label={t('settings.sshHost')} value={host} disabled={blocked||!!initial} placeholder={t('settings.sshHostPlaceholder')} searchable centered={false} searchLabel={t('settings.sshHostSearch')} onCreate={setHost} createLabel={value=>t('settings.sshUseAlias',{name:value})} onChange={e=>setHost(e.target.value)}>{config?.hosts.map(v=><option key={v} value={v}>{v}</option>)}</SettingsSelect>:<input autoFocus required autoComplete="off" spellCheck={false} placeholder={t('settings.remoteAddressPlaceholder')} value={draft.address} onChange={e=>setDraft({...draft,address:e.target.value})} disabled={blocked}/>}</label>
   <label>{t('settings.sshBotPort')}<input type="text" inputMode="numeric" pattern="[0-9]*" maxLength={5} autoComplete="off" placeholder={method==='ssh'?'9317':botAddressPort(draft.address)} value={port} onChange={e=>setPort(e.target.value)} disabled={blocked||method==='ssh'&&!!initial}/></label>
   </div>
   {method==='ssh'&&<div className="remote-endpoint-actions"><button type="button" className="text-action link-action remote-config-link" disabled={blocked} onClick={()=>setConfigOpen(true)}>{t('settings.remoteConfigManage')}</button>{initial&&<button type="button" className="text-action link-action" disabled={blocked} onClick={()=>void reconnectSSH()}>{t('settings.sshReconnectAction')}</button>}</div>}
   </>}
   <SSHAuthentication ssh={ssh}/>
   {ssh.error&&<p role="alert" className="remote-dialog-error">{t(sshProblem(ssh.error))}</p>}

   {(!creation.current||!status)&&<label>{t('settings.machineIdentifier')}<input placeholder={t('settings.machineIdentifierAuto')} maxLength={80} value={draft.name} onChange={e=>setDraft({...draft,name:e.target.value})} disabled={blocked||waiting||uncertain}/></label>}
   {creation.current&&status==='approved'&&<div className="remote-connection-state" role="status"><strong>{t(approvedUnready?'settings.remoteAuthorizedUnavailable':'settings.remoteConnectedTitle')}</strong><p>{approvedUnready?t(selected?.issue==='worker_not_ready'?'settings.remoteWorkUnavailable':connectionProblem(selected?.issue||'').key):(selected?.name||'')}</p></div>}
   {unsent&&<p className="remote-dialog-status" role="status">{t('settings.remoteNotSent')}</p>}
   {waiting&&<div className="remote-connection-state" role="status"><strong>{t('settings.remotePendingTitle')}</strong><p>{t('settings.remoteAwaitingApproval')}</p></div>}
   {uncertain&&<p className="remote-dialog-status" role="status">{t('settings.remoteRequestUnknown')}</p>}
   {status&&['rejected','expired','revoked'].includes(status)&&<p className="remote-dialog-status">{t(status==='rejected'?'settings.remoteRejected':status==='expired'?'settings.remoteExpired':'settings.remoteRevoked')}</p>}
   {status==='approved'&&selected?.issue==='worker_not_ready'&&<div className="remote-inline-state">{!creation.current&&<span>{t('settings.remoteWorkUnavailable')}</span>}<button type="button" className="text-action" disabled={blocked} onClick={()=>void recover()}>{t('settings.remoteRecoverWork')}</button></div>}
   {error&&<p role="alert" className="remote-dialog-error">{t(problem.key)}</p>}
   </div>
   {!ssh.status?.prompt&&<div className="remote-dialog-footer">{!connected&&<button type="button" disabled={busy} onClick={onClose}>{t(waiting||uncertain||creation.current&&status==='approved'?'common.close':'common.cancel')}</button>}{uncertain||waiting||(creation.current&&approvedUnready)?<SetupButton key="check-original" type="button" busy={blocked} className="primary" disabled={blocked} onClick={event=>{event.preventDefault();if(method==='ssh'){afterSSH.current='check';void ssh.start({host,port:Number(port)||9317})}else void check()}}>{t(waiting||uncertain?'settings.remoteCheckApproval':'settings.machineRecheck')}</SetupButton>:creation.current&&status==='approved'?<button type="button" className="primary" onClick={onClose}>{t('settings.remoteDone')}</button>:<SetupButton key="send-explicit" type="submit" busy={blocked} className="primary" disabled={blocked||!(method==='ssh'?host.trim():draft.address.trim())}>{t(initial&&status==='approved'?'settings.botSaveConnection':'settings.remoteConnect')}</SetupButton>}</div>}
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
