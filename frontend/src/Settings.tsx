import { useEffect, useRef, useState } from 'react';
import { desktop } from './desktop';
import { BotSetup } from './BotSetup';
import { AppearanceSettings } from './AppearanceSettings';
import { RuntimeSettings } from './RuntimeSettings';
import { ScreenInputSettings } from './ScreenInputSettings';
import { ShortcutSettings } from './ShortcutSettings';
import { ExecutionSettings } from './ExecutionSettings';
import { PermissionSettings } from './PermissionSettings';
import { TaskSettings } from './TaskSettings';
import { Maintenance } from './Maintenance';
import { useI18n } from './i18n';
import { LanguageSetting } from './i18n/LanguageSetting';
import { SettingGroup, SettingRow } from './SettingsUI';

const sections = ['general','appearance','runtime','permissions','updates'] as const;
type Section = typeof sections[number] | 'setup';
type Update = { state:string; current:string; latest:string; message:string };
type UpdatePreferences = { available:boolean; automatic:boolean; waiting:boolean };

export function Settings() {
 const {t}=useI18n();
 const content=useRef<HTMLDivElement>(null);
 const [executionVisited,setExecutionVisited]=useState(false),[runtimeVisited,setRuntimeVisited]=useState(false);
 const [section,setSection]=useState<Section>('general'),[opened,setOpened]=useState(0),[version,setVersion]=useState('');
 useEffect(()=>{
  const load=()=>{void desktop<string>('SettingsSection').then(value=>{const destination=({execution:'permissions',storage:'general',diagnostics:'updates'} as Record<string,string>)[value]??value;if((destination==='setup'||sections.some(id=>id===destination))&&window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection(destination as Section);setOpened(n=>n+1);});};
  const key=(event:KeyboardEvent)=>{if(!event.defaultPrevented&&!event.isComposing&&(event.key==='Escape'||(event.metaKey&&event.key==='w'))){event.preventDefault();void desktop('CloseSettings');}};
  load();void desktop<string>('AppVersion').then(setVersion);
  window.addEventListener('settings-open',load);window.addEventListener('keydown',key);
  return()=>{window.removeEventListener('settings-open',load);window.removeEventListener('keydown',key);};
 },[]);
 useEffect(()=>{if(content.current)content.current.scrollTop=0;if(section==='permissions')setExecutionVisited(true);if(section==='runtime')setRuntimeVisited(true);},[section]);
 if(section==='setup')return <main className="settings-window setup-window"><div className="setup-page"><BotSetup onDone={()=>{setSection('runtime');void desktop('CloseSettings');}}/></div></main>;
 return <main className="settings-window">
  <aside><nav aria-label={t('settings.navLabel')}>{sections.map(id=><button key={id} aria-current={section===id?'page':undefined} onClick={()=>{if(window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection(id);}}>{t(`settings.${id}`)}</button>)}</nav><small>Caelis Bot<br/>{version}</small></aside>
  <div className="settings-content" ref={content}>
   <div className="settings-page" hidden={section!=='permissions'}>{(executionVisited||section==='permissions')&&<><h1>{t('settings.permissions')}</h1>{section==='permissions'&&<><PermissionSettings embedded/><ScreenInputSettings/></>}<ExecutionSettings embedded/></>}</div>
<div className="settings-page" hidden={section!=='runtime'}>{(runtimeVisited||section==='runtime')&&<RuntimeSettings active={section==='runtime'} refreshKey={opened}/>}</div>
   <div className="settings-page" key={section} hidden={section==='permissions'||section==='runtime'}>
   {section==='general'?<General key={opened}/>:section==='appearance'?<AppearanceSettings/>:section==='runtime'?null:section==='permissions'?null:<Updates key={opened} version={version}/>}
   </div>
  </div>
 </main>;
}

function General() {
 const {t}=useI18n();
 const [storageOpen,setStorageOpen]=useState(false),[tasksOpen,setTasksOpen]=useState(false);
 return <section className="general-settings">
  <h1>{t('settings.general')}</h1><LanguageSetting/>
  <SettingGroup title={t('settings.shortcuts')}><ShortcutSettings/><ShortcutSettings tasks/><ShortcutSettings capture/><ShortcutSettings paste/></SettingGroup>
  <details className="settings-disclosure" onToggle={e=>setStorageOpen(e.currentTarget.open)}><summary>{t('settings.storage')}</summary>{storageOpen&&<Maintenance storage embedded/>}</details>
  <details className="settings-disclosure" onToggle={e=>setTasksOpen(e.currentTarget.open)}><summary>{t('settings.advancedTasks')}</summary>{tasksOpen&&<TaskSettings/>}</details>
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
  <SettingGroup>{preferences?.available&&<SettingRow label={t('settings.autoUpdateLabel')} htmlFor="automatic-updates" description={t('settings.autoUpdateDescription')}><input id="automatic-updates" className="settings-switch" role="switch" type="checkbox" checked={preferences.automatic} disabled={busy} onChange={event=>void automatic(event.target.checked)}/></SettingRow>}
  <SettingRow label={t('settings.softwareUpdate')} description={<span role="status">{error||(preferences?.waiting?t('settings.updateWaiting'):busy?t('common.loading'):result?.message||t('settings.checkNewVersion'))}</span>}><button disabled={busy||preferences?.waiting} onClick={()=>void check()}>{t('settings.checkUpdatesButton')}</button></SettingRow>
  <SettingRow label={result?.state==='available'?t('settings.newVersionAvailable',{version:result.latest}):t('settings.releaseAndInstall')}><button onClick={()=>void desktop('OpenReleasePage').catch(()=>setError(t('settings.openReleasePageFailed')))}>{result?.state==='available'?t('settings.downloadNewVersion'):t('settings.viewReleasePage')}</button></SettingRow></SettingGroup>
  <p className="settings-note">{preferences?.available?t('settings.updateNoteAvailable'):t('settings.updateNoteManual')}</p><Maintenance storage={false} embedded/>
 </section>;
}
