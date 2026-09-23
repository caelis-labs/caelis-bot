import { useEffect, useState } from 'react';
import { desktop } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';

type Shortcut={enabled:boolean;key:string;control:boolean;alt:boolean;shift:boolean;meta:boolean};
type State={shortcut:Shortcut;registered:boolean;message:string};
const initial:Shortcut={enabled:true,key:'Space',control:true,alt:false,shift:true,meta:false};
const mac=navigator.platform.toLowerCase().includes('mac');
function label(v:Shortcut){return [v.control?'Ctrl':'',v.alt?(mac?'Option':'Alt'):'',v.shift?'Shift':'',v.meta?(mac?'Command':'Meta'):'',v.key.replace(/^Key|^Digit/,'').replace('Space','Space')].filter(Boolean).join(' + ');}
export function ShortcutSettings(){
 const [value,setValue]=useState<Shortcut>(initial),[recording,setRecording]=useState(false),[busy,setBusy]=useState(false),[ready,setReady]=useState(false),[message,setMessage]=useState('');
 useEffect(()=>{void desktop<State>('ShortcutSettings').then(s=>{setValue(s.shortcut);setMessage(s.message);setReady(true);}).catch(()=>setMessage('暂时无法读取快捷键'));},[]);
 const save=async(v:Shortcut)=>{setBusy(true);setRecording(false);setMessage('');try{const s=await desktop<State>('SaveShortcut',v);setValue(s.shortcut);setMessage(s.message||(s.registered?'已保存':'已关闭全局快捷键'));}catch(e){setMessage(e instanceof Error?e.message:'快捷键未能保存');}finally{setBusy(false);}};
 return <SettingGroup title="快捷输入">
  <SettingRow label="全局唤出" description="在屏幕中央打开输入框" htmlFor="quick-shortcut"><input id="quick-shortcut" type="checkbox" role="switch" className="settings-switch" checked={value.enabled} disabled={!ready||busy} onChange={e=>void save({...value,enabled:e.target.checked})}/></SettingRow>
  <SettingRow label="唤出快捷键"><div className="shortcut-actions"><button className="shortcut-recorder" disabled={!ready||busy} aria-label="自定义全局唤出快捷键" onClick={()=>setRecording(true)} onBlur={()=>setRecording(false)} onKeyDown={e=>{
   if(!recording)return;e.preventDefault();e.stopPropagation();
   if(e.key==='Escape'){setRecording(false);return;}
   if(e.repeat||e.nativeEvent.isComposing||['Control','Alt','Shift','Meta'].includes(e.key))return;
   if(!/^(Space|Key[A-Z]|Digit[0-9]|F([1-9]|1[0-2]))$/.test(e.code)||!(e.ctrlKey||e.altKey||e.metaKey)){setMessage('请使用 Control、Option / Alt 或 Command 加空格、字母、数字或 F1–F12');return;}
   void save({enabled:true,key:e.code,control:e.ctrlKey,alt:e.altKey,shift:e.shiftKey,meta:e.metaKey});
  }}>{recording?'按下快捷键…':label(value)}</button><button disabled={!ready||busy} onClick={()=>void save(initial)}>重置</button><button disabled={busy} onClick={()=>void desktop('ToggleCenteredPanel')}>试用</button></div></SettingRow>
  {message&&<p className="setting-feedback" role="status">{message}</p>}
 </SettingGroup>;
}
