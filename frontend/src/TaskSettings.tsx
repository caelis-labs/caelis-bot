import {Recovery} from './SurfaceBoundary';
import {recoverRead} from './read-recovery';
import { SettingsSelect } from './SettingsSelect';
import { SettingsDialog } from './settings/runtime/SettingsDialog';
import {useEffect,useRef,useState} from 'react';
import {desktop} from './desktop';
import {SettingGroup,SettingRow} from './SettingsUI';
import {useI18n} from './i18n';
import type {MessageKey} from './i18n/catalogs';
import {TaskPreferencesSaver,validTaskLimit} from './task-preferences';
import type {TaskPreferences} from './task-preferences';

type Choice={id:string;name:string;available:boolean};
export function TaskSettings({call=desktop}:{call?:typeof desktop}) {
 const {t}=useI18n();
 const [ready,setReady]=useState(false),[limit,setLimit]=useState(''),[terminal,setTerminal]=useState('system'),[choices,setChoices]=useState<Choice[]>([]);
 const [dirty,setDirty]=useState(false);
 const [busy,setBusy]=useState(false),[error,setError]=useState<MessageKey|''>('');
 const [custom,setCustom]=useState(''),[terminalOpen,setTerminalOpen]=useState(false);
 const customSaved=useRef(''),savingCustom=useRef(false);
 const closeTerminal=()=>{saver.current?.discardTerminal();savingCustom.current=false;setTerminalOpen(false);setError('');setDirty(!!saver.current?.hasPending()||ready&&!validTaskLimit(limit));};
 const saver=useRef<TaskPreferencesSaver|null>(null),timer=useRef<ReturnType<typeof setTimeout>|null>(null);
 const flush=()=>{if(timer.current)clearTimeout(timer.current);timer.current=null;void saver.current?.flush();};
 useEffect(()=>{
  let active=true;
  const read=()=>recoverRead(()=>call<TaskPreferences>('TaskPreferences'));
  void Promise.all([read(),call<Choice[]>('TerminalChoices')]).then(([p,c])=>{
   if(!active)return;
   saver.current=new TaskPreferencesSaver(p,read,p=>call<TaskPreferences>('SaveTaskPreferences',p),state=>{if(active){setBusy(state==='saving');setDirty(state!=='saved');setError(state==='failed'?'settings.taskPreferencesSaveFailed':'');}},p=>{if(active){setTerminal(p.terminal);customSaved.current=p.customCommand??'';if(savingCustom.current){savingCustom.current=false;setTerminalOpen(false);}}});
   setLimit(String(p.maxRunning));setTerminal(p.terminal);customSaved.current=p.customCommand??'';setChoices(c);setReady(true);
  }).catch(()=>{if(active)setError('settings.taskPreferencesLoadFailed');});
  return()=>{active=false;flush();};
 },[call]);
 const visibleError=ready&&!validTaskLimit(limit)?'settings.taskLimitInvalid':error;
 const availableChoices=choices.filter(c=>c.available&&c.id!=='custom');
 const terminalAvailable=terminal==='custom'||availableChoices.some(c=>c.id===terminal);
 return <div data-settings-dirty={dirty}><SettingGroup title={t('settings.backgroundTasks')}>
  <SettingRow label={t('settings.maxBackgroundTasks')} htmlFor="max-background-tasks" description={t('settings.maxBackgroundTasksHelp')}>
   <input id="max-background-tasks" className="task-limit" type="number" min="1" step="1" value={limit} disabled={!ready} aria-invalid={ready&&!validTaskLimit(limit)} onBlur={flush} onChange={e=>{
    const value=e.target.value;setLimit(value);setDirty(true);if(timer.current)clearTimeout(timer.current);
    if(validTaskLimit(value)){setError('');saver.current?.change({maxRunning:Number(value)});timer.current=setTimeout(flush,350);}
    else {saver.current?.discardLimit();setError('settings.taskLimitInvalid');}
   }}/>
  </SettingRow>
  <SettingRow label={t('settings.externalTerminal')} htmlFor="external-terminal" description={t('settings.externalTerminalHelp')}>
   <div className="terminal-choice"><SettingsSelect id="external-terminal" value={terminalAvailable?terminal:''} disabled={!ready||busy} onChange={e=>{if(e.target.value==='custom'){setCustom(customSaved.current);setError('');setTerminalOpen(true);return;}setTerminal(e.target.value);saver.current?.change({terminal:e.target.value});flush();}}>
    {!terminalAvailable&&<option value="" disabled hidden>{t('common.loading')}</option>}
    {availableChoices.map(c=><option key={c.id} value={c.id}>{c.id==='system'?t('settings.systemTerminal'):c.name}</option>)}
    <option value="custom">{t('settings.customTerminalOption')}</option>
   </SettingsSelect></div>
  </SettingRow>
  {terminalOpen&&<SettingsDialog title={t('settings.customTerminal')} busy={busy} onClose={closeTerminal}>
   <form onSubmit={event=>{event.preventDefault();if(busy||!custom.trim())return;savingCustom.current=true;saver.current?.change({terminal:'custom',customCommand:custom.trim()});flush();}}>
    <input className="custom-terminal-input" aria-label={t('settings.customTerminalCommand')} value={custom} disabled={busy} spellCheck={false} placeholder={'"/path/to/terminal" --execute /bin/sh {script}'} onChange={event=>{setCustom(event.target.value);setError('');}}/>
    {error&&<Recovery message={t(error)} onRetry={()=>{void recoverRead(()=>call<TaskPreferences>('TaskPreferences')).then(()=>setError('')).catch(()=>{});}}/>}
    <div className="setup-end"><button type="button" disabled={busy} onClick={closeTerminal}>{t('common.cancel')}</button><button type="submit" className="primary" disabled={busy||!custom.trim()}>{t('common.save')}</button></div>
   </form>
  </SettingsDialog>}
  {!terminalOpen&&(visibleError||busy)&&<p className={`task-settings-feedback ${visibleError?'inline-error':'settings-note'}`} role={visibleError?'alert':'status'}>{t(visibleError||'settings.taskPreferencesSaving')}</p>}
 </SettingGroup></div>;
}
