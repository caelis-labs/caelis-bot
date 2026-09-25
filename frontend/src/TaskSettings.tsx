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
 const [ready,setReady]=useState(false),[limit,setLimit]=useState('3'),[terminal,setTerminal]=useState('system'),[choices,setChoices]=useState<Choice[]>([]);
 const [busy,setBusy]=useState(false),[error,setError]=useState<MessageKey|''>('');
 const [custom,setCustom]=useState('');
 const saver=useRef<TaskPreferencesSaver|null>(null),timer=useRef<ReturnType<typeof setTimeout>|null>(null);
 const flush=()=>{if(timer.current)clearTimeout(timer.current);timer.current=null;void saver.current?.flush();};
 useEffect(()=>{
  let active=true;
  const read=()=>call<TaskPreferences>('TaskPreferences');
  void Promise.all([read(),call<Choice[]>('TerminalChoices')]).then(([p,c])=>{
   if(!active)return;
   saver.current=new TaskPreferencesSaver(p,read,p=>call<TaskPreferences>('SaveTaskPreferences',p),state=>{if(active){setBusy(state==='saving');setError(state==='failed'?'settings.taskPreferencesSaveFailed':'');}},p=>{if(active)setTerminal(p.terminal);});
   setLimit(String(p.maxRunning));setTerminal(p.terminal);setCustom(p.customCommand??'');setChoices(c);setReady(true);
  }).catch(()=>{if(active)setError('settings.taskPreferencesLoadFailed');});
  return()=>{active=false;flush();};
 },[call]);
 const visibleError=ready&&!validTaskLimit(limit)?'settings.taskLimitInvalid':error;
 const availableChoices=choices.filter(c=>c.available);
 const terminalAvailable=availableChoices.some(c=>c.id===terminal);
 return <SettingGroup title={t('settings.backgroundTasks')}>
  <SettingRow label={t('settings.maxBackgroundTasks')} htmlFor="max-background-tasks" description={t('settings.maxBackgroundTasksHelp')}>
   <input id="max-background-tasks" className="task-limit" type="number" min="1" step="1" value={limit} disabled={!ready} aria-invalid={ready&&!validTaskLimit(limit)} onBlur={flush} onChange={e=>{
    const value=e.target.value;setLimit(value);if(timer.current)clearTimeout(timer.current);
    if(validTaskLimit(value)){setError('');saver.current?.change({maxRunning:Number(value)});timer.current=setTimeout(flush,350);}
    else {saver.current?.discardLimit();setError('settings.taskLimitInvalid');}
   }}/>
  </SettingRow>
  <SettingRow label={t('settings.externalTerminal')} htmlFor="external-terminal" description={t('settings.externalTerminalHelp')}>
   <select id="external-terminal" value={terminalAvailable?terminal:''} disabled={!ready} onChange={e=>{setTerminal(e.target.value);saver.current?.change({terminal:e.target.value});flush();}}>
    {!terminalAvailable&&<option value="" disabled hidden>{t(terminal==='custom'?'settings.customTerminal':'common.loading')}</option>}
    {availableChoices.map(c=><option key={c.id} value={c.id}>{c.id==='system'?t('settings.systemTerminal'):c.name}</option>)}
   </select>
  </SettingRow>
  <details className="task-terminal-advanced">
   <summary>{t('settings.advancedTerminal')}</summary>
   <label htmlFor="custom-terminal-command">{t('settings.customTerminalCommand')}</label>
   <textarea id="custom-terminal-command" rows={2} value={custom} disabled={!ready} spellCheck={false} placeholder={'"/path/to/terminal" --execute /bin/sh {script}'} onBlur={flush} onChange={e=>{setCustom(e.target.value);saver.current?.change({customCommand:e.target.value});if(timer.current)clearTimeout(timer.current);timer.current=setTimeout(flush,350);}}/>
   <p className="settings-note">{t('settings.customTerminalHelp',{script:'{script}'})}</p>
   <label className="custom-terminal-enable"><input type="checkbox" checked={terminal==='custom'} disabled={!ready||!custom.trim()} onChange={e=>{const value=e.target.checked?'custom':'system';setTerminal(value);saver.current?.change({terminal:value});flush();}}/>{t('settings.useCustomTerminal')}</label>
  </details>
  {(visibleError||busy)&&<p className={`task-settings-feedback ${visibleError?'inline-error':'settings-note'}`} role={visibleError?'alert':'status'}>{t(visibleError||'settings.taskPreferencesSaving')}</p>}
 </SettingGroup>;
}
