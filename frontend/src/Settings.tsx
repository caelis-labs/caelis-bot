import './settings/settings.css';
import {SettingsChevron} from './SettingsIcons';
import { useEffect, useRef, useState } from 'react';
import { desktop } from './desktop';
import { BotSetup } from './BotSetup';
import { AppearanceSettings } from './AppearanceSettings';
import { RuntimeSettings } from './RuntimeSettings';
import { TelegramSettings } from './TelegramSettings';
import { WeixinSettings } from './WeixinSettings';
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
import { LoginAtLoginSetting } from './LoginAtLoginSetting';
import { PluginSettings } from './PluginSettings';
import { SettingGroup, SettingRow } from './SettingsUI';

type Update = { state:string; current:string; latest:string; message:string };
type UpdatePreferences = { available:boolean; automatic:boolean; waiting:boolean; state?:string; message?:string };

export function Settings() {
 const {t}=useI18n();
 const content=useRef<HTMLDivElement>(null);
 const [executionVisited,setExecutionVisited]=useState(false),[runtimeVisited,setRuntimeVisited]=useState(false),[telegramVisited,setTelegramVisited]=useState(false),[weixinVisited,setWeixinVisited]=useState(false);
 const [section,setSection]=useState<Section>('general'),[opened,setOpened]=useState(0),[version,setVersion]=useState('');
 const [visible,setVisible]=useState(false),[focusVersion,setFocusVersion]=useState(0);
 const visibleRef=useRef(false),visibilitySeq=useRef(0);
 const sectionRef=useRef(section);sectionRef.current=section;
 useEffect(()=>{
  const load=(opening=false)=>{
   const seq=visibilitySeq.current;
   void Promise.all([desktop<string>('SettingsSection').catch(()=>''),desktop<boolean>('SettingsVisible').catch(()=>opening)]).then(([value,shown])=>{
    if(seq!==visibilitySeq.current)return;
    const destination=settingsDestination(value);
    if(destination&&window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection(destination);
    setOpened(n=>n+1);
    if(opening||shown){visibleRef.current=true;setVisible(true);}
   });
  };
  const onOpen=()=>{visibilitySeq.current++;load(true);};
  const onClose=()=>{visibilitySeq.current++;visibleRef.current=false;setVisible(false);};
  const onFocus=()=>{if(visibleRef.current)setFocusVersion(n=>n+1);};
  const key=(event:KeyboardEvent)=>{if(event.defaultPrevented||event.isComposing)return;if(event.key==='Escape'&&(sectionRef.current==='telegram'||sectionRef.current==='weixin')){event.preventDefault();if(window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection('chatConnections');return;}if(event.key==='Escape'||(event.metaKey&&event.key==='w')){event.preventDefault();void desktop('CloseSettings');}};
  load();void desktop<string>('AppVersion').then(setVersion);
  window.addEventListener('settings-open',onOpen);window.addEventListener('settings-close',onClose);window.addEventListener('settings-focus',onFocus);window.addEventListener('keydown',key);
  return()=>{window.removeEventListener('settings-open',onOpen);window.removeEventListener('settings-close',onClose);window.removeEventListener('settings-focus',onFocus);window.removeEventListener('keydown',key);};
 },[]);
 useEffect(()=>{if(content.current)content.current.scrollTop=0;if(section==='permissions')setExecutionVisited(true);if(section==='models'||section==='connections')setRuntimeVisited(true);if(section==='telegram')setTelegramVisited(true);if(section==='weixin')setWeixinVisited(true)},[section]);
 if(section==='setup')return <main className="settings-window setup-window"><div className="setup-page"><BotSetup active={visible} focusVersion={focusVersion} onDone={()=>{setSection('connections');void desktop('CloseSettings');}}/></div></main>;
 return <main className="settings-window">
  <aside><nav aria-label={t('settings.navLabel')}>{sections.map(id=><button key={id} aria-current={section===id||(section==='telegram'||section==='weixin')&&id==='chatConnections'?'page':undefined} onClick={()=>{if(window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection(id);}}>{t(`settings.${id}`)}</button>)}</nav><small>Caelis Bot<br/>{version}</small></aside>
  <div className="settings-content" ref={content}>
   <div className="settings-page" data-section="permissions" hidden={section!=='permissions'}>{(executionVisited||section==='permissions')&&<><h1>{t('settings.permissions')}</h1>{section==='permissions'&&<PermissionSettings embedded active={visible} focusVersion={focusVersion}/>}<ExecutionSettings embedded/></>}</div>
   <div className="settings-page" data-section={section==='models'?'models':'connections'} hidden={section!=='models'&&section!=='connections'}>{(runtimeVisited||section==='models'||section==='connections')&&<RuntimeSettings page={section==='models'?'models':'connections'} active={visible&&(section==='models'||section==='connections')} refreshKey={opened} onConnections={()=>setSection('connections')}/>}</div>
   <div className="settings-page" data-section="chatConnections" hidden={section!=='chatConnections'}>{section==='chatConnections'&&<MessagingSettings active={visible} focusVersion={focusVersion} openTelegram={()=>setSection('telegram')} openWeixin={()=>setSection('weixin')}/>}</div>
   <div className="settings-page" data-section="telegram" hidden={section!=='telegram'}>{(telegramVisited||section==='telegram')&&<TelegramSettings active={visible&&section==='telegram'} onBack={()=>setSection('chatConnections')}/>}</div>
   <div className="settings-page" data-section="weixin" hidden={section!=='weixin'}>{(weixinVisited||section==='weixin')&&<WeixinSettings active={visible&&section==='weixin'} onBack={()=>setSection('chatConnections')}/>}</div>
   <div className="settings-page" data-section="extras" hidden={section!=='extras'}>{section==='extras'&&<><h1>{t('settings.extras')}</h1><ScreenInputSettings/></>}</div>
   <div className="settings-page" data-section="plugins" hidden={section!=='plugins'}>{section==='plugins'&&<PluginSettings visible={visible}/>}</div>
   <div className="settings-page" data-section="machines" hidden={section!=='machines'}>{section==='machines'&&<MachineSettings standalone active={visible}/>}</div>
   <div className="settings-page" data-section={section} key={section} hidden={['permissions','models','connections','chatConnections','telegram','weixin','machines','extras','plugins'].includes(section)}>
   {section==='general'?<General key={opened} active={visible} focusVersion={focusVersion}/>:section==='appearance'?<AppearanceSettings/>:['models','connections','chatConnections','telegram','weixin','machines','extras','plugins','permissions'].includes(section)?null:<Updates key={opened} version={version} active={visible} focusVersion={focusVersion}/>}
   </div>
  </div>
 </main>;
}

function General({active,focusVersion}:{active:boolean;focusVersion:number}) {
 const {t}=useI18n();
 const [storageOpen,setStorageOpen]=useState(false);
 return <section className="general-settings">
  <h1>{t('settings.general')}</h1><div className="general-preferences"><LanguageSetting/><LoginAtLoginSetting active={active} focusVersion={focusVersion}/></div><TaskSettings/>
  <SettingGroup title={t('settings.shortcuts')}><ShortcutSettings/><ShortcutSettings tasks/></SettingGroup>
  <details className="settings-disclosure" onToggle={e=>setStorageOpen(e.currentTarget.open)}><summary><SettingsChevron/>{t('settings.storage')}</summary>{storageOpen&&<Maintenance storage embedded/>}</details>
 </section>;
}

function Updates({version,active,focusVersion}:{version:string;active:boolean;focusVersion:number}) {
 const {t}=useI18n();
 const [result,setResult]=useState<Update|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const [preferences,setPreferences]=useState<UpdatePreferences|null>(null);
 const checking=useRef(false),lastRead=useRef(0),wasActive=useRef(false);
 const refresh=async()=>{try{setPreferences(await desktop<UpdatePreferences>('UpdatePreferences'));}catch{setError(t('settings.updatePreferencesLoadFailed'));}};
 const check=async()=>{
  if(checking.current)return;
  checking.current=true;setBusy(true);setError('');
  try{setResult(await desktop<Update>('CheckUpdates'));await refresh();}catch{setError(t('settings.updateCheckFailed'));}finally{checking.current=false;setBusy(false);}
 };
 useEffect(()=>{
  if(!active){wasActive.current=false;return;}
  const opening=!wasActive.current;wasActive.current=true;
  if(!opening&&Date.now()-lastRead.current<1000)return;
  lastRead.current=Date.now();void refresh();
 },[active,focusVersion,t]);
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
