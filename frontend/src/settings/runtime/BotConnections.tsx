import {useEffect,useRef,useState} from 'react';
import {backend} from '../../desktop';
import type {BotConnectionInput,Machine,WorkerSharing,SSHConfiguration} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SSHConfigDialog,SSHAuthentication,useSSHConnect,sshEndpoint,sshTarget,sshProblem} from './SSHConnection';
import {SettingsDialog} from './SettingsDialog';
import {botAddress} from './bot-address';
import {pairingProblem,connectionProblem,incomingProblem} from './pairing-errors';

export function BotConnectionDialog({machines,initial,call=backend,onClose,onSaved,onChanged}:{machines:Machine[];initial?:Machine;call?:typeof backend;onClose:()=>void;onSaved:()=>void;onChanged:(fresh?:Machine)=>Promise<void>}){
 const {t}=useI18n();
 const [draft,setDraft]=useState<BotConnectionInput>(()=>({id:initial?.id||'bot-'+crypto.randomUUID(),name:initial?.name||'',address:initial?.address||''}));
 const [method,setMethod]=useState(initial?.address.startsWith('ssh://')?'ssh':'address'),[host,setHost]=useState(sshTarget(initial?.address||'')?.host||''),[config,setConfig]=useState<SSHConfiguration>(),[editConfig,setEditConfig]=useState(false),[manualHost,setManualHost]=useState(false);
 const [port,setPort]=useState(sshTarget(initial?.address||'')?String(sshTarget(initial!.address)!.port):''),[busy,setBusy]=useState(false),[error,setError]=useState('');const pending=useRef(false);
 const selected=machines.find(v=>v.id===draft.id)||initial;
 const run=async(action:()=>Promise<unknown>)=>{if(pending.current)return;pending.current=true;setBusy(true);setError('');try{await action()}catch(cause){const recovery=(cause as {botRecovery?:{profileId:string}})?.botRecovery;if(recovery?.profileId)setDraft(old=>({...old,id:recovery.profileId}));setError(cause instanceof Error?cause.message:'unclassified');await onChanged()}finally{pending.current=false;setBusy(false)}};
 const endpoint=()=>method==='ssh'?sshEndpoint(host,port):botAddress(draft.address,port);
 const connect=()=>run(async()=>{
  const address=endpoint(),known=machines.find(v=>v.address===address);
  await call<Machine>('ConnectBot',{...draft,id:known?.id||draft.id,address,reconnect:!!known?.archived||!!selected&&selected.pairingStatus!=='approved'});onSaved();
 });
 const check=()=>run(async()=>{const address=endpoint();const fresh=selected&&address!==selected.address?await call<Machine>('ConnectBot',{...draft,address,reconnect:true}):await call<Machine>('InspectMachine',draft.id,'');await onChanged(fresh)});
 const recover=()=>run(async()=>{const fresh=await call<Machine>('RecoverBotWork',draft.id);await onChanged(fresh)});
 const remove=()=>run(async()=>{await call('RemoveMachine',draft.id);onSaved()});
 const status=selected?.pairingStatus,unsent=selected?.issue==='pairing_request_not_dispatched',uncertain=status==='unknown'&&!unsent,waiting=status==='pending';
 const problem=connectionProblem(error);
 const afterSSH=useRef<'connect'|'check'>('connect');
 const ssh=useSSHConnect(call,async()=>{if(afterSSH.current==='check')await check();else await connect()});
 const loadConfig=async()=>{try{setConfig(await call<SSHConfiguration>('SSHConfiguration'))}catch{ssh.setError('ssh_config_unreadable')}};
 useEffect(()=>{if(method==='ssh')void loadConfig()},[method,call]);
 const submit=()=>{if(busy||ssh.working)return;if(port&&(!/^[0-9]+$/.test(port)||Number(port)<1||Number(port)>65535)){setError('invalid_bot_address');return;}if(method==='ssh'&&!(initial&&status==='approved')){afterSSH.current='connect';void ssh.start({host:host.trim(),port:Number(port)||9317})}else void connect()};
 const reconnectSSH=async()=>{if(blocked)return;afterSSH.current='check';try{await call('StopSSHConnection',{host,port:Number(port)||9317});await ssh.start({host,port:Number(port)||9317})}catch{ssh.setError('ssh_connection_failed')}};
 if(editConfig)return <SSHConfigDialog call={call} onClose={()=>setEditConfig(false)} onSaved={()=>void loadConfig()}/>;
 const blocked=busy||ssh.working;

 return <SettingsDialog title={initial?t('settings.remoteManageTitle'):t('settings.remoteAdd')} className="remote-bot-dialog" busy={busy} onClose={onClose}>
  <form onSubmit={e=>{e.preventDefault();if(!uncertain&&!waiting)submit()}}>
   {!initial&&<div className="remote-methods" role="group" aria-label={t('settings.sshConnectionMethod')}><button type="button" aria-pressed={method==='address'} disabled={blocked} onClick={()=>setMethod('address')}>{t('settings.sshAddressMethod')}</button><button type="button" aria-pressed={method==='ssh'} disabled={blocked} onClick={()=>setMethod('ssh')}>SSH</button></div>}
   <div className="remote-endpoint-fields"><label>{t(method==='ssh'?'settings.sshHost':'settings.remoteAddress')}{method==='ssh'?<>{(!config||config.hosts.length>0)&&!manualHost&&!initial?<select aria-label={t('settings.sshHost')} value={host} disabled={blocked||!config} onChange={e=>{if(e.target.value==='__manual'){setManualHost(true);setHost('')}else setHost(e.target.value)}}><option value="" disabled>{t('settings.sshHostPlaceholder')}</option>{config?.hosts.map(v=><option key={v} value={v}>{v}</option>)}<option value="__manual">{t('settings.sshEnterHost')}</option></select>:<input autoFocus required autoComplete="off" spellCheck={false} placeholder={t('settings.sshHostPlaceholder')} value={host} onChange={e=>setHost(e.target.value)} disabled={blocked||!!initial}/>}</>:<input autoFocus required autoComplete="off" spellCheck={false} placeholder={t('settings.remoteAddressPlaceholder')} value={draft.address} onChange={e=>setDraft({...draft,address:e.target.value})} disabled={blocked}/>}</label><label>{t(method==='ssh'?'settings.sshBotPort':'settings.remotePort')}<input type="text" inputMode="numeric" pattern="[0-9]*" maxLength={5} autoComplete="off" placeholder={t('settings.remoteDefaultPort')} value={port} onChange={e=>setPort(e.target.value)} disabled={blocked||method==='ssh'&&!!initial}/></label></div>
   {method==='ssh'&&<div className="remote-ssh-options">{!config?.hosts.length&&<span>{t('settings.sshNoHosts')}</span>}<button type="button" className="remote-config-button" disabled={blocked} onClick={()=>setEditConfig(true)}>{t('settings.sshConfigTitle')}</button></div>}
   {method==='ssh'&&initial&&<div className="remote-ssh-options"><button type="button" className="text-action" disabled={blocked} onClick={()=>void reconnectSSH()}>{t('settings.sshReconnectAction')}</button></div>}
   <SSHAuthentication ssh={ssh}/>
   {ssh.error&&<p role="alert" className="remote-dialog-error">{t(sshProblem(ssh.error))}</p>}

   {initial&&<label>{t('settings.machineName')}<input maxLength={80} value={draft.name} onChange={e=>setDraft({...draft,name:e.target.value})} disabled={blocked||waiting||uncertain}/></label>}
   {unsent&&<p className="remote-dialog-status" role="status">{t('settings.remoteNotSent')}</p>}
   {waiting&&<p className="remote-dialog-status" role="status">{t('settings.remoteAwaitingApproval')}</p>}
   {uncertain&&<p className="remote-dialog-status" role="status">{t('settings.remoteRequestUnknown')}</p>}
   {status&&['rejected','expired','revoked'].includes(status)&&<p className="remote-dialog-status">{t(status==='rejected'?'settings.remoteRejected':status==='expired'?'settings.remoteExpired':'settings.remoteRevoked')}</p>}
   {status==='approved'&&selected?.issue==='worker_not_ready'&&<div className="remote-inline-state"><span>{t('settings.remoteWorkUnavailable')}</span><button type="button" className="text-action" disabled={blocked} onClick={()=>void recover()}>{t('settings.remoteRecoverWork')}</button></div>}
   {error&&<p role="alert" className="remote-dialog-error">{t(problem.key)}</p>}
   {!ssh.status?.prompt&&<div className="remote-dialog-footer">{selected&&<button type="button" className="text-action remote-remove" disabled={blocked} onClick={()=>void remove()}>{t('settings.machineRemove')}</button>}<button type="button" disabled={busy} onClick={onClose}>{t(selected?'common.close':'common.cancel')}</button>{uncertain||waiting?<button key="check-original" type="button" className="primary" disabled={blocked} onClick={event=>{event.preventDefault();if(method==='ssh'){afterSSH.current='check';void ssh.start({host,port:Number(port)||9317})}else void check()}}>{t(busy?'settings.remoteChecking':'settings.machineRecheck')}</button>:<button key="send-explicit" type="submit" className="primary" disabled={blocked||!(method==='ssh'?host.trim():draft.address.trim())}>{t(busy?'settings.machineConnecting':initial&&status==='approved'?'settings.botSaveConnection':'settings.remoteSendRequest')}</button>}</div>}
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
