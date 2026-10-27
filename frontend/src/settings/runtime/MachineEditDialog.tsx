import {useEffect,useRef,useState} from 'react';
import {backend} from '../../desktop';
import type {Machine,SSHTarget} from '../../backend/contract';
import {useI18n} from '../../i18n';
import {SetupButton} from '../../SetupLoading';
import {SettingsDialog} from './SettingsDialog';
import {SSHFields} from './SSHFields';
import {sshAddress,sshTarget} from './SSHConnection';
import {PortInput} from './PortInput';
import {botAddress,botAddressPort} from './bot-address';
import {connectionProblem} from './pairing-errors';
import {newMachineOperationID,mutateMachine,MachineMutationError,pendingMachineMutation} from './machine-mutation';

export function MachineEditDialog({machine,call=backend,onClose,onSaved}:{machine:Machine;call?:typeof backend;onClose:()=>void;onSaved:(v:Machine)=>void}){
 const {t}=useI18n(),[name,setName]=useState(machine.name),[address,setAddress]=useState(machine.savedAddress||machine.address),[port,setPort]=useState('');
 const [target,setTarget]=useState<SSHTarget|undefined>(()=>sshTarget(address,machine.state==='legacy'&&!machine.savedAddress)),[busy,setBusy]=useState(false),[error,setError]=useState(''),[uncertain,setUncertain]=useState(()=>!!pendingMachineMutation(machine.id,'save'));const submitting=useRef(false);
 const save=async()=>{if(submitting.current||error==='machine_receipt_expired')return;submitting.current=true;setBusy(true);setError('');try{
  const result=await mutateMachine(call,{id:newMachineOperationID(),machineId:machine.id,action:'save',name,address:target?sshAddress(target):address===machine.address&&!port?address:botAddress(address,port)});
  onSaved(result.machine);onClose();
 }catch(e){setError(e instanceof MachineMutationError?e.code:e instanceof Error&&e.message==='invalid_bot_port'?'invalid_bot_port':'machine_save_failed');setUncertain(e instanceof MachineMutationError&&e.uncertain)}finally{submitting.current=false;setBusy(false)}};
 useEffect(()=>{if(pendingMachineMutation(machine.id,'save'))void save()},[]);
 return <SettingsDialog title={t('settings.remoteManageTitle')} busy={busy} className="remote-bot-dialog" onClose={onClose}><form onSubmit={e=>{e.preventDefault();void save()}}>
  <div className="remote-add-body">
   <label>{t('settings.machineIdentifier')}<input autoFocus required maxLength={80} value={name} disabled={busy||uncertain} onChange={e=>setName(e.target.value)}/></label>
   {target?<SSHFields value={target} onChange={setTarget} disabled={busy||uncertain} call={call}/>:<div className="remote-endpoint-row"><label>{t('settings.remoteAddress')}<input required autoComplete="off" spellCheck={false} value={address} disabled={busy||uncertain} onChange={e=>setAddress(e.target.value)}/></label><label>{t('settings.sshBotPort')}<PortInput value={port} placeholder={botAddressPort(address)} disabled={busy||uncertain} onChange={setPort}/></label></div>}
   {error&&<p role="alert" className="remote-dialog-error">{t(connectionProblem(error).key)}</p>}
  </div><div className="remote-dialog-footer"><button type="button" disabled={busy} onClick={onClose}>{t('common.cancel')}</button><SetupButton type="submit" busy={busy} disabled={error==='machine_receipt_expired'} className="primary">{t(uncertain?'settings.remoteCheckResult':'common.save')}</SetupButton></div>
 </form></SettingsDialog>
}
