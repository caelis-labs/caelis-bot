import {isWebSurface} from './transport';
import { PencilSimpleIcon } from '@phosphor-icons/react/dist/csr/PencilSimple';
import { TrashIcon } from '@phosphor-icons/react/dist/csr/Trash';
import { useEffectEvent, useEffect, useRef, useState } from 'react';
import { desktop } from './desktop';
import { SettingRow } from './SettingsUI';
import { useI18n } from './i18n';

type Shortcut={enabled:boolean;key:string;control:boolean;alt:boolean;shift:boolean;meta:boolean};
type State={shortcut:Shortcut;registered:boolean;message:string;status?:'available'|'conflict'|'disabled'|'unavailable'};
const initial:Shortcut={enabled:true,key:'Space',control:true,alt:false,shift:true,meta:false};
const mac=navigator.platform.toLowerCase().includes('mac');
function label(v:Shortcut){return [v.control?(mac?'⌃':'Ctrl'):'',v.alt?(mac?'⌥':'Alt'):'',v.shift?(mac?'⇧':'Shift'):'',v.meta?(mac?'⌘':'Meta'):'',v.key.replace(/^Key|^Digit/,'').replace('Space',mac?'␣':'Space')].filter(Boolean).join(mac?'':' + ');}
export function ShortcutSettings(props:{tasks?:boolean;capture?:boolean;paste?:boolean}){return isWebSurface()?null:<NativeShortcutSettings {...props}/>;}
function NativeShortcutSettings({tasks=false,capture=false,paste=false}:{tasks?:boolean;capture?:boolean;paste?:boolean}){
 const screen=capture||paste;
 const defaults:Shortcut=screen?{...initial,key:paste?'F3':'F1',control:false,shift:false}:tasks?{...initial,key:'KeyT'}:initial;
 const prefix=capture?'Capture':paste?'Paste':tasks?'Task':'';
 const title=capture?'settings.captureShortcutTitle':paste?'settings.pasteShortcutTitle':tasks?'settings.taskShortcutTitle':'settings.shortcutTitle';
 const {t}=useI18n();
 const edit=useRef<HTMLButtonElement>(null),recorder=useRef<HTMLInputElement>(null);
 const [value,setValue]=useState<Shortcut>(defaults),[recording,setRecording]=useState(false),[busy,setBusy]=useState(false),[ready,setReady]=useState(false),[message,setMessage]=useState('');
 const [availability,setAvailability]=useState<State['status']>();
 const readGeneration=useRef(0),interacting=useRef(false);
 interacting.current=busy||recording;
 const receive=(s:State)=>{setValue(s.shortcut);setReady(true);setAvailability(s.status??(s.registered?'available':s.shortcut.enabled?'unavailable':'disabled'));};
 const loaded=useEffectEvent(receive);
 const readFailed=useEffectEvent(()=>{setAvailability('unavailable');});
 useEffect(()=>{
  let disposed=false,inFlight=false,failures=0;
  const refresh=async()=>{
   if(disposed||inFlight||interacting.current||document.visibilityState==='hidden')return;
   inFlight=true;const generation=readGeneration.current;
   try{const state=await desktop<State>(prefix+'ShortcutSettings');if(!disposed&&generation===readGeneration.current&&!interacting.current){loaded(state);failures=0;}}
   catch{if(!disposed&&generation===readGeneration.current&&++failures>=2)readFailed();}
   finally{inFlight=false;}
  };
  void refresh();const timer=window.setInterval(()=>void refresh(),1000);
  window.addEventListener('focus',refresh);document.addEventListener('visibilitychange',refresh);
  return()=>{disposed=true;window.clearInterval(timer);window.removeEventListener('focus',refresh);document.removeEventListener('visibilitychange',refresh);};
 },[prefix]);
 useEffect(()=>{if(recording)recorder.current?.focus();},[recording]);
 const cancel=()=>{setRecording(false);setMessage('');requestAnimationFrame(()=>edit.current?.focus());};
 const save=async(v:Shortcut)=>{readGeneration.current++;interacting.current=true;setBusy(true);setRecording(false);setMessage('');try{receive(await desktop<State>('Save'+prefix+'Shortcut',v));}catch{try{receive(await desktop<State>(prefix+'ShortcutSettings'));}catch{}setMessage(t('settings.shortcutChooseAnother'));}finally{interacting.current=false;setBusy(false);requestAnimationFrame(()=>edit.current?.focus());}};
 return <div className="shortcut-setting">
  <SettingRow label={t(title)} description={message?<span role="status">{message}</span>:value.enabled&&(availability==='conflict'||availability==='unavailable')?<span className="shortcut-status" data-state={availability} role="status">{t(availability==='conflict'?'settings.shortcutConflictUnavailable':'settings.shortcutUnavailable')}</span>:undefined}>
   <div className="shortcut-inline" aria-busy={busy}>
    {recording?<><input ref={recorder} className="shortcut-inline-recorder" readOnly value="" placeholder={t('settings.shortcutRecordingPlaceholder')} aria-label={t('settings.shortcutRecordingAria')} onBlur={()=>{setRecording(false);setMessage('');}} onKeyDown={event=>{
     if(event.key==='Tab'){setRecording(false);return;}
     event.preventDefault();event.stopPropagation();
     if(event.key==='Escape'){cancel();return;}
     if(event.repeat||event.nativeEvent.isComposing||['Control','Alt','Shift','Meta'].includes(event.key))return;
     if(!/^(Space|Key[A-Z]|Digit[0-9]|F([1-9]|1[0-2]))$/.test(event.code)||!(event.ctrlKey||event.altKey||event.metaKey||(screen&&/^F([1-9]|1[0-2])$/.test(event.code)))){setMessage(t(screen?'settings.captureShortcutInvalid':'settings.shortcutInvalidPrompt'));return;}
     void save({enabled:true,key:event.code,control:event.ctrlKey,alt:event.altKey,shift:event.shiftKey,meta:event.metaKey});
    }}/><button className="shortcut-cancel" onMouseDown={event=>event.preventDefault()} onClick={cancel}>{t('common.cancel')}</button></>:<>
     {value.enabled?<kbd className="shortcut-keycap">{label(value)}</kbd>:<span className="shortcut-unset">{t('settings.shortcutOff')}</span>}
     <button ref={edit} className="shortcut-icon" disabled={!ready||busy} aria-label={t('settings.shortcutEdit',{title:t(title)})} title={t('settings.shortcutEdit',{title:t(title)})} onClick={()=>{readGeneration.current++;interacting.current=true;setMessage('');setRecording(true);}}><PencilSimpleIcon size={17}/></button>
     {value.enabled&&<button className="shortcut-icon" disabled={!ready||busy} aria-label={t('settings.shortcutClear')} title={t('settings.shortcutClear')} onClick={()=>void save({...value,enabled:false})}><TrashIcon size={17}/></button>}
    </>}
   </div>
  </SettingRow>
 </div>;
}
