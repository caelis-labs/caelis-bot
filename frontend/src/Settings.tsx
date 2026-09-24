import { useEffect, useRef, useState } from 'react';
import { desktop, type Placement } from './desktop';
import { BotSetup } from './BotSetup';
import { AppearanceSettings } from './AppearanceSettings';
import { RuntimeSettings } from './RuntimeSettings';
import { ShortcutSettings } from './ShortcutSettings';
import { ExecutionSettings } from './ExecutionSettings';
import { Maintenance } from './Maintenance';
import { useI18n } from './i18n';
import { LanguageSetting } from './i18n/LanguageSetting';
import { SettingGroup, SettingRow } from './SettingsUI';

const sections = ['general','appearance','runtime','execution','storage','diagnostics','updates'] as const;
type Section = typeof sections[number] | 'setup';
type Update = { state:string; current:string; latest:string; message:string };
type UpdatePreferences = { available:boolean; automatic:boolean; waiting:boolean };

export function Settings() {
 const {t}=useI18n();
 const content=useRef<HTMLDivElement>(null);
 const [executionVisited,setExecutionVisited]=useState(false);
 const [section,setSection]=useState<Section>('general'),[opened,setOpened]=useState(0),[version,setVersion]=useState('');
 useEffect(()=>{
  const load=()=>{void desktop<string>('SettingsSection').then(value=>{if((value==='setup'||sections.some(id=>id===value))&&window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection(value as Section);setOpened(n=>n+1);});};
  const key=(event:KeyboardEvent)=>{if(!event.isComposing&&(event.key==='Escape'||(event.metaKey&&event.key==='w'))){event.preventDefault();void desktop('CloseSettings');}};
  load();void desktop<string>('AppVersion').then(setVersion);
  window.addEventListener('settings-open',load);window.addEventListener('keydown',key);
  return()=>{window.removeEventListener('settings-open',load);window.removeEventListener('keydown',key);};
 },[]);
 useEffect(()=>{if(content.current)content.current.scrollTop=0;if(section==='execution')setExecutionVisited(true);},[section]);
 if(section==='setup')return <main className="settings-window setup-window"><div className="setup-page"><BotSetup onDone={()=>{setSection('runtime');void desktop('CloseSettings');}}/></div></main>;
 return <main className="settings-window">
  <aside><nav aria-label={t('settings.navLabel')}>{sections.map(id=><button key={id} aria-current={section===id?'page':undefined} onClick={()=>{if(window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection(id);}}>{t(`settings.${id}`)}</button>)}</nav><small>Caelis Bot<br/>{version}</small></aside>
  <div className="settings-content" ref={content}>
   <div className="settings-page" hidden={section!=='execution'}>{(executionVisited||section==='execution')&&<ExecutionSettings/>}</div>
   <div className="settings-page" key={section} hidden={section==='execution'}>
   {section==='general'?<General key={opened}/>:section==='appearance'?<AppearanceSettings/>:section==='runtime'?<RuntimeSettings/>:section==='execution'?null:section==='storage'?<Maintenance key="storage" storage/>:section==='diagnostics'?<Maintenance key="diagnostics" storage={false}/>:<Updates key={opened} version={version}/>}
   </div>
  </div>
 </main>;
}

function General() {
 const {t,number}=useI18n();
 const [scale,setScale]=useState<number|null>(null),[permission,setPermission]=useState(''),[error,setError]=useState<'settings.sizeLoadFailed'|'settings.sizeSaveFailed'|'settings.notificationTestFailed'|'settings.notificationSettingsFailed'|''>('');
 const pending=useRef<number|null>(null),pumping=useRef(false),save=useRef(false),dragging=useRef(false);
 useEffect(()=>{
  void desktop<Placement>('Placement').then(p=>setScale(p.scale)).catch(()=>setError('settings.sizeLoadFailed'));
  const refresh=()=>{void desktop<string>('NotificationStatus').then(setPermission).catch(()=>setPermission('unavailable'));};
  refresh();const timer=window.setInterval(()=>{if(document.hasFocus())refresh();},2000);window.addEventListener('focus',refresh);
  return()=>{clearInterval(timer);window.removeEventListener('focus',refresh);};
 },[]);
 // At most one geometry request is in flight. Coalesce motion, then save the
 // final value on release; late bridge responses cannot rewind the thumb.
 const flush=async()=>{
  if(pumping.current)return;
  pumping.current=true;
  try {
   while(pending.current!==null||save.current){
    if(pending.current!==null){const value=pending.current;pending.current=null;await desktop('PreviewScale',value);}
    else {save.current=false;await desktop('CommitScale');}
   }
  }catch{pending.current=null;save.current=false;setError('settings.sizeSaveFailed');}
  finally{pumping.current=false;}
 };
 const commit=()=>{dragging.current=false;save.current=true;void flush();};
 const status:Record<string,string>={authorized:t('settings.notificationEnabled'),denied:t('settings.notificationDisabled'),notDetermined:t('settings.notificationNotDetermined'),unavailable:t('settings.unavailable')};
 return <section className="general-settings">
  <h1>{t('settings.general')}</h1><LanguageSetting/><ShortcutSettings/>
  <SettingGroup title={t('settings.pet')}>
   <SettingRow label={t('settings.petSize')} htmlFor="pet-size"><div className="size-control">
    <input id="pet-size" type="range" min="0.65" max="1.6" step="any" disabled={scale===null} value={scale??1} aria-valuetext={scale===null?'':number(scale,{style:'percent',maximumFractionDigits:0})} onPointerDown={()=>{dragging.current=true;}} onChange={event=>{const value=event.target.valueAsNumber;setScale(value);pending.current=value;save.current=!dragging.current;setError('');void flush();}} onPointerUp={commit} onPointerCancel={commit} onBlur={commit}/>
    <output htmlFor="pet-size">{scale===null?'—':number(scale,{style:'percent',maximumFractionDigits:0})}</output>
   </div></SettingRow>
  </SettingGroup>
  <SettingGroup title={t('settings.notifications')}><SettingRow label={t('settings.systemNotifications')} description={<span role="status">{status[permission]??t('common.loading')}</span>}>{permission==='authorized'&&<button onClick={()=>void desktop('Notify',`notification-test-${crypto.randomUUID()}`,t('settings.notificationTestTitle'),t('settings.notificationTestBody'),true).catch(()=>setError('settings.notificationTestFailed'))}>{t('settings.notificationTest')}</button>}<button disabled={!permission||permission==='unavailable'} onClick={()=>void desktop('ConfigureNotifications').catch(()=>setError('settings.notificationSettingsFailed'))}>{t(permission==='notDetermined'?'settings.enableNotifications':'settings.openSystemSettings')}</button></SettingRow></SettingGroup>
  {error&&<p role="alert" className="inline-error">{t(error)}</p>}
 </section>;
}

function Updates({version}:{version:string}) {
 const {t}=useI18n();
 const [result,setResult]=useState<Update|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const [preferences,setPreferences]=useState<UpdatePreferences|null>(null);
 const checking=useRef(false);
 const check=async()=>{
  if(checking.current)return;
  checking.current=true;setBusy(true);setError('');
  try{setResult(await desktop<Update>('CheckUpdates'));}catch{setError(t('settings.updateCheckFailed'));}finally{checking.current=false;setBusy(false);}
 };
 useEffect(()=>{
  const refresh=()=>{void desktop<UpdatePreferences>('UpdatePreferences').then(setPreferences).catch(()=>setError(t('settings.updatePreferencesLoadFailed')));};
  refresh();const timer=window.setInterval(refresh,2000);return()=>clearInterval(timer);
 },[t]);
 const automatic=async(value:boolean)=>{
  setBusy(true);setError('');
  try{await desktop('SetAutomaticUpdates',value);setPreferences(await desktop<UpdatePreferences>('UpdatePreferences'));}
  catch{setError(t('settings.updatePreferencesSaveFailed'));}finally{setBusy(false);}
 };
 return <section className="update-settings">
  <h1>{t('settings.updates')}</h1>
  <div className="about-identity"><img src="/icons/caelis-avatar.png" alt="" width="64" height="64"/><div><h2>Caelis Bot</h2><p>{result?.current||version}</p></div></div>
  <SettingGroup>{preferences?.available&&<SettingRow label={t('settings.autoUpdateLabel')} htmlFor="automatic-updates" description={t('settings.autoUpdateDescription')}><input id="automatic-updates" type="checkbox" checked={preferences.automatic} disabled={busy} onChange={event=>void automatic(event.target.checked)}/></SettingRow>}
  <SettingRow label={t('settings.softwareUpdate')} description={<span role="status">{error||(preferences?.waiting?t('settings.updateWaiting'):busy?t('common.loading'):result?.message||t('settings.checkNewVersion'))}</span>}><button disabled={busy||preferences?.waiting} onClick={()=>void check()}>{t('settings.checkUpdatesButton')}</button></SettingRow>
  <SettingRow label={result?.state==='available'?t('settings.newVersionAvailable',{version:result.latest}):t('settings.releaseAndInstall')}><button onClick={()=>void desktop('OpenReleasePage').catch(()=>setError(t('settings.openReleasePageFailed')))}>{result?.state==='available'?t('settings.downloadNewVersion'):t('settings.viewReleasePage')}</button></SettingRow></SettingGroup>
  <p className="settings-note">{preferences?.available?t('settings.updateNoteAvailable'):t('settings.updateNoteManual')}</p>
 </section>;
}
