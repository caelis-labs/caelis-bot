import {SettingsChevron} from './SettingsIcons';
import {useCallback,useEffect,useRef,useState} from 'react';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import {changeSystemPermission,type PermissionID,type PermissionRequestResult} from './permission-actions';
import type {MessageKey} from './i18n/catalogs';

type Permission={id:PermissionID;status:string;target?:string};
type PermissionState={supported:boolean;appPath:string;development:boolean;permissions:Permission[]};
const names:Record<PermissionID,MessageKey>={accessibility:'settings.permissionAccessibility',screenCapture:'settings.permissionScreen',automation:'settings.permissionAutomation',notifications:'settings.notifications'};
const descriptions:Record<PermissionID,MessageKey>={accessibility:'settings.permissionAccessibilityHelp',screenCapture:'settings.permissionScreenHelp',automation:'settings.permissionAutomationHelp',notifications:'settings.permissionNotificationsHelp'};
const statuses:Record<string,MessageKey>={authorized:'settings.permissionAuthorized',permissionRequired:'settings.permissionRequired',notDetermined:'settings.permissionRequired',denied:'settings.permissionDenied',notRunning:'settings.permissionNotRunning',unsupported:'settings.permissionUnsupported',unavailable:'settings.permissionUnknown'};
const outcomes:Record<PermissionRequestResult['state'],MessageKey>={requested:'settings.permissionRequested',authorized:'settings.permissionRefreshNeeded',settingsRequired:'settings.permissionNeedsSettings',settingsOpened:'settings.permissionChangeInSystem'};

export function PermissionSettings({onDone,onBack,embedded=false,active=true,focusVersion=0,call=desktop}:{onDone?:()=>void;onBack?:()=>void;embedded?:boolean;active?:boolean;focusVersion?:number;call?:typeof desktop}) {
 const {t}=useI18n();
 const onboarding=!!onDone;
 const [state,setState]=useState<PermissionState|null>(null),[busy,setBusy]=useState(''),[error,setError]=useState<MessageKey|''>(''),[notice,setNotice]=useState(false);
 const [feedback,setFeedback]=useState<{id:PermissionID;state:PermissionRequestResult['state']}|null>(null);
 const [repair,setRepair]=useState<PermissionID>('accessibility'),[confirmed,setConfirmed]=useState(false);
 const [captureEnabled,setCaptureEnabled]=useState<boolean|null>(onDone?null:true);
 const alive=useRef(true),loading=useRef<Promise<void>|null>(null),acting=useRef(false),lastRead=useRef(0),wasActive=useRef(false);
 const latestDone=useRef(onDone);
 latestDone.current=onDone;
 const refresh=useCallback(async()=>{
  const previous=loading.current;
  const request=(async()=>{
   if(previous)await previous;
   try{const next=await call<PermissionState>('SystemPermissions');if(alive.current){setState(next);setError('');}}
   catch{if(alive.current)setError('settings.permissionLoadFailed');}
  })();
  loading.current=request;
  try{await request;}finally{if(loading.current===request)loading.current=null;}
 },[call]);
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;};},[]);
 useEffect(()=>{
  if(!active){wasActive.current=false;return;}
  const opening=!wasActive.current;wasActive.current=true;
  if(acting.current||!opening&&Date.now()-lastRead.current<1000)return;
  lastRead.current=Date.now();void refresh();
 },[active,focusVersion,refresh]);
 useEffect(()=>{if(!onboarding)return;let live=true;void call<{enabled:boolean}>('CapturePreferences').then(v=>{if(live)setCaptureEnabled(v.enabled)}).catch(()=>{if(live)setError('settings.screenPreferencesFailed')});return()=>{live=false}},[call,onboarding]);
 const perform=async(id:string,action:()=>Promise<unknown>)=>{
  if(acting.current)return;acting.current=true;setBusy(id);setError('');
  try{
   // Drain an earlier read so its result cannot overwrite the action's refresh.
   if(loading.current)await loading.current;
   await action();if(loading.current)await loading.current;await refresh();
  }catch{if(alive.current)setError('settings.permissionActionFailed');}
  finally{acting.current=false;if(alive.current)setBusy('');}
 };
 const change=(p:Permission)=>perform(p.id,async()=>{
  setFeedback(null);
  const result=await changeSystemPermission(call,p.id,p.status==='authorized');
  if(alive.current)setFeedback({id:p.id,state:result.state});
 });
 const permissionRow=(p:Permission)=>{
     const checked=p.status==='authorized';
     const action=checked?'settings.managePermission':p.status==='notRunning'||p.status==='unavailable'?'settings.checkPermission':p.status==='denied'?'settings.openSystemSettings':'settings.allowPermission';
     const hint=feedback?.id===p.id&&(feedback.state==='settingsOpened'||!checked)?outcomes[feedback.state]:null;
     return <div className="permission-row" key={p.id}>
      <div className="permission-description">
       <label id={`permission-label-${p.id}`} htmlFor={`permission-${p.id}`}>{t(names[p.id])}</label>
       <p id={`permission-description-${p.id}`}>{t(descriptions[p.id])}</p>
       {p.target&&<small>{t('settings.permissionTarget',{name:p.target})}</small>}
      </div>
      <div className="permission-toggle">
       <span id={`permission-status-${p.id}`} data-status={p.status} role="status">{t(busy===p.id?'settings.permissionWaiting':statuses[p.status]??'settings.permissionUnknown')}</span>
       <button id={`permission-${p.id}`} aria-label={`${t(names[p.id])} · ${t(action)}`}
        aria-describedby={`permission-description-${p.id} permission-status-${p.id}`} aria-busy={busy===p.id}
        disabled={!!busy||p.status==='unsupported'} onClick={()=>void change(p)}>{t(action)}</button>
      </div>
      {hint&&<div className="permission-feedback" role="status"><span>{t(hint)}</span>
       {feedback?.state!=='settingsOpened'&&<button className="text-action" disabled={!!busy} onClick={()=>void perform(p.id,()=>call('OpenSystemPermissionSettings',p.id))}>{t('settings.openSystemSettings')}</button>}
      </div>}
     </div>;
 };
 return <section className="permission-settings">
  {embedded?<h2 className="settings-section-title">{t('settings.systemPermissions')}</h2>:<h1>{t(onDone?'settings.permissionGuideTitle':'settings.permissions')}</h1>}
  <p className="permission-intro">{t(onDone?'settings.permissionOptionalIntro':'settings.permissionIntroduction')}</p>
  {!state&&<p role="status">{t('common.loading')}</p>}
  {state&&!state.supported&&<p>{t('settings.permissionUnsupported')}</p>}
  {state?.supported&&<>
   <div className="permission-list">
    {state.permissions.filter(p=>p.id==='accessibility'||p.id==='notifications'||p.id==='screenCapture'&&(!onDone||captureEnabled===true)).map(permissionRow)}
   </div>
   {!onDone&&<details className="permission-more"><summary><SettingsChevron/>{t('settings.morePermissions')}</summary><div className="permission-list">{state.permissions.filter(p=>p.id==='automation').map(permissionRow)}</div></details>}
   <p className="permission-system-note">{t('settings.permissionSwitchHelp')}</p>
   {onDone?<details className="permission-privacy"><summary>{t('settings.permissionPrivacyTitle')}</summary><p>{t('settings.permissionPrivacyBody')}</p></details>:<section className="permission-privacy" aria-labelledby="permission-privacy-title">
    <h2 id="permission-privacy-title">{t('settings.permissionPrivacyTitle')}</h2>
    <p>{t('settings.permissionPrivacyBody')}</p>
   </section>}
   {!onDone&&<details className="permission-repair">
    <summary><SettingsChevron/>{t('settings.permissionRepairTitle')}</summary>
    <p className="settings-note">{t('settings.permissionMissingApp')}</p>
    <div className="permission-app-path"><code>{state.appPath}</code><button disabled={!!busy} onClick={()=>void perform('reveal',()=>call('RevealPermissionApp'))}>{t('settings.permissionReveal')}</button></div>
    <p className="settings-note">{t('settings.permissionRepairHelp')}</p>
    {state.development&&<p className="settings-note">{t('settings.permissionDevelopment')}</p>}
    {state.permissions.some(p=>p.id==='notifications'&&p.status==='authorized')&&<button className="text-action" disabled={!!busy} onClick={()=>void perform('test-notification',()=>call('Notify',`notification-test-${crypto.randomUUID()}`,t('settings.notificationTestTitle'),t('settings.notificationTestBody'),true))}>{t('settings.notificationTest')}</button>}
    <div className="permission-reset-category"><label htmlFor="permission-repair-category">{t('settings.permissionRepairCategory')}</label>
     <select id="permission-repair-category" value={repair} disabled={!!busy} onChange={e=>{setRepair(e.target.value as PermissionID);setConfirmed(false);setNotice(false);}}>
      {(['accessibility','screenCapture','automation'] as const).map(id=><option key={id} value={id}>{t(names[id])}</option>)}
     </select>
    </div>
    <p className="settings-note">{t('settings.permissionResetScope')}</p>
    <label className="permission-reset-consent"><input type="checkbox" checked={confirmed} disabled={!!busy} onChange={e=>setConfirmed(e.target.checked)}/>{t('settings.permissionResetConfirm')}</label>
    <button disabled={!confirmed||!!busy} onClick={()=>void perform('reset',async()=>{await call('ResetSystemPermission',repair);if(alive.current){setNotice(true);setConfirmed(false);}})}>{t('settings.permissionReset')}</button>
    {notice&&<p role="status">{t('settings.permissionResetDone')}</p>}
   </details>}
  </>}
  {error&&<p className="inline-error" role="alert">{t(error)}</p>}
  {onDone&&<div className="setup-end">{onBack&&<button type="button" disabled={!!busy} onClick={onBack}>{t('settings.setupBack')}</button>}<button className="primary" disabled={!!busy} onClick={()=>void perform('finish',async()=>{await call('FinishPermissionGuide');latestDone.current?.()})}>{t('settings.permissionContinue')}</button></div>}
 </section>;
}
