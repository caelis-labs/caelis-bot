import {SetupButton,SetupLoading} from './SetupLoading';
import {SetupBack} from './SetupBack';
import {useEffect,useRef,useState} from 'react';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import {finishCaptureChoice} from './setup-capture-choice';

export function SetupExtras({onDone,onBack,call=desktop}:{onDone:()=>void;onBack?:()=>void;call?:typeof desktop}) {
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
 if(choice===null&&!error)return <SetupLoading/>;
 return <section className="setup-extras" aria-labelledby="setup-extras-title">
  {onBack&&<SetupBack disabled={busy} onClick={onBack}/>}
  <h1 id="setup-extras-title">{t('settings.setupExtrasTitle')}</h1>
  <p className="permission-intro">{t('settings.setupExtrasIntro')}</p>
  <div className="setup-capture-choice"><label htmlFor="setup-capture-enabled"><span>{t('settings.extrasCaptureToggle')}</span><input id="setup-capture-enabled" className="settings-switch" type="checkbox" role="switch" checked={choice??true} disabled={choice===null||busy} onChange={event=>{edited.current=true;setChoice(event.target.checked);setError('')}}/></label><p>{t('settings.extrasCaptureDescription')}</p></div>
  {error&&<p role="alert" className="inline-error">{t(error==='load'?'settings.setupExtrasLoadFailed':'settings.setupExtrasSaveFailed')}</p>}
  {error==='load'&&<button type="button" onClick={()=>{setError('');setReload(n=>n+1)}}>{t('settings.setupExtrasRetry')}</button>}
  <div className="setup-end"><SetupButton type="button" className="primary" busy={busy} disabled={choice===null} onClick={()=>void continueSetup()}>{t('settings.setupNext')}</SetupButton></div>
 </section>;
}
