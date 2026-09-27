import { useEffectEvent, useEffect, useState } from 'react';
import { desktop } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';
import { useI18n } from './i18n';

type Shortcut={enabled:boolean;key:string;control:boolean;alt:boolean;shift:boolean;meta:boolean};
type State={shortcut:Shortcut;registered:boolean;message:string};
const initial:Shortcut={enabled:true,key:'Space',control:true,alt:false,shift:true,meta:false};
const mac=navigator.platform.toLowerCase().includes('mac');
function label(v:Shortcut){return [v.control?'Ctrl':'',v.alt?(mac?'Option':'Alt'):'',v.shift?'Shift':'',v.meta?(mac?'Command':'Meta'):'',v.key.replace(/^Key|^Digit/,'').replace('Space','Space')].filter(Boolean).join(' + ');}
export function ShortcutSettings({tasks=false}:{tasks?:boolean}){
 const defaults:Shortcut=tasks?{...initial,key:'KeyT'}:initial;
 const prefix=tasks?'Task':'';
 const toggleID=tasks?'task-shortcut':'quick-shortcut';
 const {t}=useI18n();
 const loadFailed=useEffectEvent(()=>t('settings.shortcutLoadFailed'));
 const [value,setValue]=useState<Shortcut>(defaults),[recording,setRecording]=useState(false),[busy,setBusy]=useState(false),[ready,setReady]=useState(false),[message,setMessage]=useState('');
 useEffect(()=>{void desktop<State>(prefix+'ShortcutSettings').then(s=>{setValue(s.shortcut);setMessage(s.message);setReady(true);}).catch(()=>setMessage(loadFailed()));},[prefix]);
 const save=async(v:Shortcut)=>{setBusy(true);setRecording(false);setMessage('');try{const s=await desktop<State>('Save'+prefix+'Shortcut',v);setValue(s.shortcut);setMessage(s.message||(s.registered?t('settings.shortcutSaved'):t('settings.shortcutDisabled')));}catch(e){setMessage(e instanceof Error?e.message:t('settings.shortcutSaveFailed'));}finally{setBusy(false);}};
 return <SettingGroup title={t(tasks?'settings.taskShortcutTitle':'settings.shortcutTitle')}>
  <SettingRow label={t(tasks?'settings.taskShortcutToggle':'settings.shortcutGlobalToggle')} description={t(tasks?'settings.taskShortcutDescription':'settings.shortcutGlobalDescription')} htmlFor={toggleID}><input id={toggleID} type="checkbox" role="switch" className="settings-switch" checked={value.enabled} disabled={!ready||busy} onChange={e=>void save({...value,enabled:e.target.checked})}/></SettingRow>
  <SettingRow label={t('settings.shortcutKeyLabel')}><div className="shortcut-actions"><button className="shortcut-recorder" disabled={!ready||busy} aria-label={recording?t('settings.shortcutRecordingAria'):t('settings.shortcutCurrentAria',{label:label(value)})} onClick={()=>setRecording(true)} onBlur={()=>setRecording(false)} onKeyDown={e=>{
   if(!recording)return;e.preventDefault();e.stopPropagation();
   if(e.key==='Escape'){setRecording(false);return;}
   if(e.repeat||e.nativeEvent.isComposing||['Control','Alt','Shift','Meta'].includes(e.key))return;
   if(!/^(Space|Key[A-Z]|Digit[0-9]|F([1-9]|1[0-2]))$/.test(e.code)||!(e.ctrlKey||e.altKey||e.metaKey)){setMessage(t('settings.shortcutInvalidPrompt'));return;}
   void save({enabled:true,key:e.code,control:e.ctrlKey,alt:e.altKey,shift:e.shiftKey,meta:e.metaKey});
  }}>{recording?t('settings.shortcutRecordingPlaceholder'):label(value)}</button><button disabled={!ready||busy} onClick={()=>void save(defaults)}>{t('settings.shortcutResetDefault')}</button><button disabled={busy} onClick={()=>void desktop(tasks?'ToggleTaskDock':'ToggleHistory')}>{t('settings.shortcutTest')}</button></div></SettingRow>
  {message&&<p className="setting-feedback" role="status">{message}</p>}
 </SettingGroup>;
}
