import {MachineMutationError} from './machine-mutation';
import {connectionProblem} from './pairing-errors';
import {useEffect,useRef,useState} from 'react';
import {SetupButton} from '../../SetupLoading';
import {useI18n} from '../../i18n';
import {SettingsDialog} from './SettingsDialog';

// The explanation and named action are the confirmation; no hidden second
// mutation or optimistic success when the original operation is uncertain.
export function ConnectionActionDialog({title,name,identity,description,action,failure,resume=false,onAction,onCheck,onClose}:{title:string;name:string;identity?:string;description:string;action:string;failure:string;resume?:boolean;onAction:()=>Promise<void>;onCheck?:()=>Promise<void>;onClose:()=>void}){
 const {t}=useI18n(),[busy,setBusy]=useState(false),[error,setError]=useState(''),[uncertain,setUncertain]=useState(resume),pending=useRef(false);
 const submit=async()=>{if(pending.current)return;pending.current=true;setBusy(true);setError('');try{await (uncertain&&onCheck?onCheck():onAction());onClose()}catch(e){setError(e instanceof MachineMutationError?e.code:'');setUncertain(e instanceof MachineMutationError?e.uncertain:true);if(!(e instanceof MachineMutationError))setError('unconfirmed')}finally{pending.current=false;setBusy(false)}};
 useEffect(()=>{if(resume)void submit()},[]);
 return <SettingsDialog title={title} className="remote-action-dialog" busy={busy} onClose={onClose}><div className="remote-action-copy"><strong title={identity}>{name}</strong><p>{description}</p></div>{error&&<p role="alert" className="remote-dialog-error">{error==='unconfirmed'?failure:t(connectionProblem(error).key)}</p>}<div className="remote-dialog-footer"><button disabled={busy} onClick={onClose}>{t('common.close')}</button><SetupButton busy={busy} disabled={error==='machine_receipt_expired'} className="remote-destructive" onClick={()=>void submit()}>{uncertain?t('settings.remoteCheckResult'):action}</SetupButton></div></SettingsDialog>;
}
