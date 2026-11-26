import {SetupButton} from './SetupLoading';
import {ArrowSquareOutIcon,GearSixIcon} from './SettingsIcons';
import {SettingsSelect} from './SettingsSelect';
import {SettingsDialog} from './settings/runtime/SettingsDialog';
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
const outcomes:Partial<Record<PermissionRequestResult['state'],MessageKey>>={requested:'settings.permissionRequested',authorized:'settings.permissionRefreshNeeded',settingsRequired:'settings.permissionNeedsSettings'};

export function PermissionSettings({embedded=false,active=true,focusVersion=0,call=desktop}:{embedded?:boolean;active?:boolean;focusVersion?:number;call?:typeof desktop}) {
 const {t}=useI18n();
 const [state,setState]=useState<PermissionState|null>(null),[busy,setBusy]=useState(''),[error,setError]=useState<MessageKey|''>(''),[notice,setNotice]=useState(false);
 const [feedback,setFeedback]=useState<{id:PermissionID;state:PermissionRequestResult['state']}|null>(null);
 const [repairOpen,setRepairOpen]=useState(false);
 const [repair,setRepair]=useState<PermissionID>('accessibility'),[confirmed,setConfirmed]=useState(false);
 const repairTriggerRef=useRef<HTMLButtonElement>(null);
 const alive=useRef(true),loading=useRef<Promise<void>|null>(null),acting=useRef(false),lastRead=useRef(0),wasActive=useRef(false);
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
 const closeRepair=()=>{if(busy)return;setRepairOpen(false);setConfirmed(false);setNotice(false);};
 const permissionRow=(p:Permission)=>{
     const checked=p.status==='authorized';
     const action=checked?'settings.managePermission':p.status==='notRunning'||p.status==='unavailable'?'settings.checkPermission':p.status==='denied'?'settings.openSystemSettings':'settings.allowPermission';
     const hint=feedback?.id===p.id&&!checked&&feedback.state!=='settingsOpened'?outcomes[feedback.state]??null:null;
     return <div className="permission-row" key={p.id}>
      <div className="permission-description">
       <label id={`permission-label-${p.id}`} htmlFor={`permission-${p.id}`}>{t(names[p.id])}</label>
       <p id={`permission-description-${p.id}`}>{t(descriptions[p.id])}</p>
      </div>
      <div className="permission-toggle">
       <span id={`permission-status-${p.id}`} data-status={p.status} role="status">{t(busy===p.id?'settings.permissionWaiting':statuses[p.status]??'settings.permissionUnknown')}</span>
       <SetupButton busy={busy===p.id} id={`permission-${p.id}`} className={checked?'permission-icon-action':undefined} title={checked?t(action):undefined} aria-label={`${t(names[p.id])} · ${t(action)}`}
        aria-describedby={`permission-description-${p.id} permission-status-${p.id}`} aria-busy={busy===p.id}
        disabled={!!busy||p.status==='unsupported'} onClick={()=>void change(p)}>{checked?<GearSixIcon size={17} aria-hidden="true"/>:t(action)}</SetupButton>
      </div>
      {hint&&<div className="permission-feedback" role="status"><span>{t(hint)}</span>
       <button className="text-action" disabled={!!busy} onClick={()=>void perform(p.id,()=>call('OpenSystemPermissionSettings',p.id))}>{t('settings.openSystemSettings')}</button>
      </div>}
     </div>;
 };
 return <section className="permission-settings">
  {!embedded&&<h1>{t('settings.permissions')}</h1>}
  {!state&&<p role="status">{t('common.loading')}</p>}
  {state&&!state.supported&&<p>{t('settings.permissionUnsupported')}</p>}
  {state?.supported&&<>
   <div className="permission-list">
    {state.permissions.filter(p=>p.id in names).map(permissionRow)}
   </div>
   <div className="permission-footer">
    <button ref={repairTriggerRef} type="button" className="permission-repair-trigger" onClick={()=>{setNotice(false);setConfirmed(false);setError('');setRepairOpen(true);}}>{t('settings.permissionRepairTitle')}</button>
   </div>
   {repairOpen&&<SettingsDialog className="permission-repair-dialog" title={t('settings.permissionRepairTitle')} busy={!!busy} returnFocus={repairTriggerRef.current} onClose={closeRepair}><div className="permission-repair">
    <div className="permission-repair-field">
     <div className="permission-repair-field-head">
      <span>{t('settings.permissionCurrentApp').replace(/[:：]\s*$/,'')}</span>
      <button type="button" className="permission-reveal-link" disabled={!!busy} onClick={()=>void perform('reveal',()=>call('RevealPermissionApp'))}><span>{t('settings.permissionReveal')}</span><ArrowSquareOutIcon size={14} aria-hidden="true"/></button>
     </div>
     <div className="permission-app-path"><code>{state.appPath}</code></div>
    </div>
    <div className="permission-repair-field">
     <label className="permission-repair-label" htmlFor="permission-repair-category">{t('settings.permissionRepairCategory')}</label>
     <SettingsSelect id="permission-repair-category" centered={false} value={repair} disabled={!!busy} onChange={e=>{setRepair(e.target.value as PermissionID);setConfirmed(false);setNotice(false);}}>
      {(['accessibility','screenCapture','automation'] as const).map(id=><option key={id} value={id}>{t(names[id])}</option>)}
     </SettingsSelect>
     <label className="permission-reset-consent"><input type="checkbox" checked={confirmed} disabled={!!busy} onChange={e=>setConfirmed(e.target.checked)}/><span>{t('settings.permissionResetConfirm')}</span></label>
    </div>
    {notice&&<p className="permission-repair-status" role="status">{t('settings.permissionResetDone')}</p>}
    {error&&<p className="inline-error permission-repair-error" role="alert">{t(error)}</p>}
    <div className="permission-repair-footer">
     <button type="button" disabled={!!busy} onClick={closeRepair}>{t('common.cancel')}</button>
     <button type="button" className="permission-reset-submit" disabled={!confirmed||!!busy} onClick={()=>void perform('reset',async()=>{await call('ResetSystemPermission',repair);if(alive.current){setNotice(true);setConfirmed(false);}})}>{t('settings.permissionReset')}</button>
    </div>
   </div></SettingsDialog>}
  </>}
  {error&&<p className="inline-error" role="alert">{t(error)}</p>}
 </section>;
}
