import {useEffect,useRef,useState} from 'react';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import {finishCaptureChoice} from './setup-capture-choice';

export function SetupExtras({onDone,call=desktop}:{onDone:()=>void;call?:typeof desktop}) {
 const {t}=useI18n();
 const [choice,setChoice]=useState<boolean|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState<'load'|'save'|''>(''),[reload,setReload]=useState(0);
 const edited=useRef(false),acting=useRef(false),latestDone=useRef(onDone);
 latestDone.current=onDone;
 useEffect(()=>{let live=true;void call<{enabled:boolean}>('CapturePreferences').then(value=>{if(live&&!edited.current){setChoice(value.enabled);setError('')}}).catch(()=>{if(live)setError('load')});return()=>{live=false}},[call,reload]);
 const continueSetup=async()=>{
  if(acting.current||choice===null)return;
  acting.current=true;setBusy(true);setError('');
  try{await finishCaptureChoice(call,choice,()=>latestDone.current())}
  catch{setError('save')}finally{acting.current=false;setBusy(false)}
 };
 return <section className="setup-extras" aria-labelledby="setup-extras-title">
  <h1 id="setup-extras-title">{t('settings.setupExtrasTitle')}</h1>
  <p className="permission-intro">{t('settings.setupExtrasIntro')}</p>
  <div className="setup-capture-choice"><label htmlFor="setup-capture-enabled"><input id="setup-capture-enabled" type="checkbox" role="switch" checked={choice??true} disabled={choice===null||busy} onChange={event=>{edited.current=true;setChoice(event.target.checked);setError('')}}/>{t('settings.extrasCaptureToggle')}</label><p>{t('settings.extrasCaptureDescription')}</p></div>
  <p className="settings-note">{t('settings.setupExtrasLater')}</p>
  {error&&<p role="alert" className="inline-error">{t(error==='load'?'settings.setupExtrasLoadFailed':'settings.setupExtrasSaveFailed')}</p>}
  {error==='load'&&<button type="button" onClick={()=>{setError('');setReload(n=>n+1)}}>{t('settings.setupExtrasRetry')}</button>}
  <div className="setup-end"><button type="button" className="primary" disabled={choice===null||busy} onClick={()=>void continueSetup()}>{t('settings.permissionContinue')}</button></div>
 </section>;
}
