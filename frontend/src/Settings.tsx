import './settings/settings.css';
import {SettingsChevron} from './SettingsIcons';
import { useEffect, useRef, useState } from 'react';
import { desktop } from './desktop';
import { BotSetup } from './BotSetup';
import { AppearanceSettings } from './AppearanceSettings';
import { RuntimeSettings } from './RuntimeSettings';
import { TelegramSettings } from './TelegramSettings';
import { MessagingSettings } from './MessagingSettings';
import { MachineSettings } from './settings/runtime/MachineSettings';
import { settingsSections as sections, settingsDestination, type SettingsSection as Section } from './settings-navigation';
import { ScreenInputSettings } from './ScreenInputSettings';
import { ShortcutSettings } from './ShortcutSettings';
import { ExecutionSettings } from './ExecutionSettings';
import { PermissionSettings } from './PermissionSettings';
import { TaskSettings } from './TaskSettings';
import { Maintenance } from './Maintenance';
import { useI18n } from './i18n';
import { LanguageSetting } from './i18n/LanguageSetting';
import { SettingGroup, SettingRow } from './SettingsUI';

type Update = { state:string; current:string; latest:string; message:string };
type UpdatePreferences = { available:boolean; automatic:boolean; waiting:boolean; state?:string; message?:string };

export function Settings() {
 const {t}=useI18n();
 const content=useRef<HTMLDivElement>(null);
 const [executionVisited,setExecutionVisited]=useState(false),[runtimeVisited,setRuntimeVisited]=useState(false),[telegramVisited,setTelegramVisited]=useState(false);
 const [section,setSection]=useState<Section>('general'),[opened,setOpened]=useState(0),[version,setVersion]=useState('');
 const sectionRef=useRef(section);sectionRef.current=section;
 useEffect(()=>{
  const load=()=>{void desktop<string>('SettingsSection').then(value=>{const destination=settingsDestination(value);if(destination&&window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection(destination);setOpened(n=>n+1);});};
  const key=(event:KeyboardEvent)=>{if(event.defaultPrevented||event.isComposing)return;if(event.key==='Escape'&&sectionRef.current==='telegram'){event.preventDefault();if(window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection('chatConnections');return;}if(event.key==='Escape'||(event.metaKey&&event.key==='w')){event.preventDefault();void desktop('CloseSettings');}};
  load();void desktop<string>('AppVersion').then(setVersion);
  window.addEventListener('settings-open',load);window.addEventListener('keydown',key);
  return()=>{window.removeEventListener('settings-open',load);window.removeEventListener('keydown',key);};
 },[]);
 useEffect(()=>{if(content.current)content.current.scrollTop=0;if(section==='permissions')setExecutionVisited(true);if(section==='models'||section==='connections')setRuntimeVisited(true);if(section==='telegram')setTelegramVisited(true)},[section]);
 if(section==='setup')return <main className="settings-window setup-window"><div className="setup-page"><BotSetup onDone={()=>{setSection('connections');void desktop('CloseSettings');}}/></div></main>;
 return <main className="settings-window">
  <aside><nav aria-label={t('settings.navLabel')}>{sections.map(id=><button key={id} aria-current={section===id||section==='telegram'&&id==='chatConnections'?'page':undefined} onClick={()=>{if(window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection(id);}}>{t(`settings.${id}`)}</button>)}</nav><small>Caelis Bot<br/>{version}</small></aside>
  <div className="settings-content" ref={content}>
   <div className="settings-page" hidden={section!=='permissions'}>{(executionVisited||section==='permissions')&&<><h1>{t('settings.permissions')}</h1>{section==='permissions'&&<PermissionSettings embedded/>}<ExecutionSettings embedded/></>}</div>
   <div className="settings-page" hidden={section!=='models'&&section!=='connections'}>{(runtimeVisited||section==='models'||section==='connections')&&<RuntimeSettings page={section==='models'?'models':'connections'} active={section==='models'||section==='connections'} refreshKey={opened} onConnections={()=>setSection('connections')}/>}</div>
   <div className="settings-page" hidden={section!=='chatConnections'}>{section==='chatConnections'&&<MessagingSettings openTelegram={()=>setSection('telegram')}/>}</div>
   <div className="settings-page" hidden={section!=='telegram'}>{(telegramVisited||section==='telegram')&&<TelegramSettings active={section==='telegram'} onBack={()=>setSection('chatConnections')}/>}</div>
   <div className="settings-page" hidden={section!=='extras'}>{section==='extras'&&<><h1>{t('settings.extras')}</h1><ScreenInputSettings/></>}</div>
   <div className="settings-page" hidden={section!=='machines'}>{section==='machines'&&<MachineSettings standalone/>}</div>
   <div className="settings-page" key={section} hidden={['permissions','models','connections','chatConnections','telegram','machines','extras'].includes(section)}>
   {section==='general'?<General key={opened}/>:section==='appearance'?<AppearanceSettings/>:['models','connections','chatConnections','telegram','machines','extras','permissions'].includes(section)?null:<Updates key={opened} version={version}/>}
   </div>
  </div>
 </main>;
}

function General() {
 const {t}=useI18n();
 const [storageOpen,setStorageOpen]=useState(false);
 return <section className="general-settings">
  <h1>{t('settings.general')}</h1><LanguageSetting/><TaskSettings/>
  <SettingGroup title={t('settings.shortcuts')}><ShortcutSettings/><ShortcutSettings tasks/></SettingGroup>
  <details className="settings-disclosure" onToggle={e=>setStorageOpen(e.currentTarget.open)}><summary><SettingsChevron/>{t('settings.storage')}</summary>{storageOpen&&<Maintenance storage embedded/>}</details>
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
 const updateStatus=preferences?.state==='failed'?`${t('settings.updateHandoffFailed')}: ${preferences.message||t('settings.updateCheckFailed')}`:
  preferences?.state==='preparing'?t('settings.updatePreparing'):
  preferences?.state==='closing'?t('settings.updateClosing'):
  preferences?.state==='restarting'?t('settings.updateRestarting'):
  preferences?.waiting?t('settings.updateWaiting'):busy?t('common.loading'):result?.message||t('settings.checkNewVersion');
 return <section className="update-settings">
  <h1>{t('settings.updates')}</h1>
  <div className="about-identity"><img src="/icons/caelis-avatar.png" alt="" width="64" height="64"/><div><h2>Caelis Bot</h2><p>{result?.current||version}</p></div></div>
  <SettingGroup>{preferences?.available&&<SettingRow label={t('settings.autoUpdateLabel')} htmlFor="automatic-updates" description={t('settings.autoUpdateDescription')}><input id="automatic-updates" className="settings-switch" role="switch" type="checkbox" checked={preferences.automatic} disabled={busy} onChange={event=>void automatic(event.target.checked)}/></SettingRow>}
  <SettingRow label={t('settings.softwareUpdate')} description={<span role="status">{error||updateStatus}</span>}><button disabled={busy||preferences?.waiting} onClick={()=>void check()}>{t('settings.checkUpdatesButton')}</button></SettingRow>
  <SettingRow label={result?.state==='available'?t('settings.newVersionAvailable',{version:result.latest}):t('settings.releaseAndInstall')}><button onClick={()=>void desktop('OpenReleasePage').catch(()=>setError(t('settings.openReleasePageFailed')))}>{result?.state==='available'?t('settings.downloadNewVersion'):t('settings.viewReleasePage')}</button></SettingRow></SettingGroup>
  <p className="settings-note">{preferences?.available?t('settings.updateNoteAvailable'):t('settings.updateNoteManual')}</p><Maintenance storage={false} embedded/>
 </section>;
}
