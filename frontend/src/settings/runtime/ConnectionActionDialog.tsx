import {useRef,useState} from 'react';
import {SetupButton} from '../../SetupLoading';
import {useI18n} from '../../i18n';
import {SettingsDialog} from './SettingsDialog';

// The explanation and named action are the confirmation; no hidden second
// mutation or optimistic success when the original operation is uncertain.
export function ConnectionActionDialog({title,name,description,action,failure,onAction,onClose}:{title:string;name:string;description:string;action:string;failure:string;onAction:()=>Promise<void>;onClose:()=>void}){
 const {t}=useI18n(),[busy,setBusy]=useState(false),[error,setError]=useState(false),pending=useRef(false);
 const submit=async()=>{if(pending.current)return;pending.current=true;setBusy(true);setError(false);try{await onAction();onClose()}catch{setError(true)}finally{pending.current=false;setBusy(false)}};
 return <SettingsDialog title={title} className="remote-action-dialog" busy={busy} onClose={onClose}><div className="remote-action-copy"><strong>{name}</strong><p>{description}</p></div>{error&&<p role="alert" className="remote-dialog-error">{failure}</p>}<div className="remote-dialog-footer"><button disabled={busy} onClick={onClose}>{t('common.close')}</button><SetupButton busy={busy} className="remote-destructive" onClick={()=>void submit()}>{action}</SetupButton></div></SettingsDialog>;
}
