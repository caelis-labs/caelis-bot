import { useEffect, useRef, useState } from 'react';
import { desktop, type Placement } from './desktop';
import { BotSetup } from './BotSetup';
import { AppearanceSettings } from './AppearanceSettings';
import { RuntimeSettings } from './RuntimeSettings';
import { ShortcutSettings } from './ShortcutSettings';
import { ExecutionSettings } from './ExecutionSettings';
import { Maintenance } from './Maintenance';
import { SettingGroup, SettingRow } from './SettingsUI';

const sections = [ ['general','常规'], ['appearance','外观'], ['runtime','运行时'], ['execution','模型与权限'], ['storage','存储'], ['diagnostics','诊断'], ['updates','关于'] ] as const;
type Section = typeof sections[number][0] | 'setup';
type Update = { state:string; current:string; latest:string; message:string };

export function Settings() {
 const [section,setSection]=useState<Section>('general'),[opened,setOpened]=useState(0),[version,setVersion]=useState('');
 useEffect(()=>{
  const load=()=>{void desktop<string>('SettingsSection').then(value=>{if((value==='setup'||sections.some(([id])=>id===value))&&window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection(value as Section);setOpened(n=>n+1);});};
  const key=(event:KeyboardEvent)=>{if(!event.isComposing&&(event.key==='Escape'||(event.metaKey&&event.key==='w'))){event.preventDefault();void desktop('CloseSettings');}};
  load();void desktop<string>('AppVersion').then(setVersion);
  window.addEventListener('settings-open',load);window.addEventListener('keydown',key);
  return()=>{window.removeEventListener('settings-open',load);window.removeEventListener('keydown',key);};
 },[]);
 if(section==='setup')return <main className="settings-window setup-window"><div className="setup-page"><BotSetup onDone={()=>{setSection('runtime');void desktop('CloseSettings');}}/></div></main>;
 return <main className="settings-window">
  <aside><nav aria-label="设置分类">{sections.map(([id,label])=><button key={id} aria-current={section===id?'page':undefined} onClick={()=>{if(window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setSection(id);}}>{label}</button>)}</nav><small>Caelis Bot<br/>{version}</small></aside>
  <div className="settings-content" key={section}>
   {section==='general'?<General key={opened}/>:section==='appearance'?<AppearanceSettings/>:section==='runtime'?<RuntimeSettings/>:section==='execution'?<ExecutionSettings/>:section==='storage'?<Maintenance key="storage" storage/>:section==='diagnostics'?<Maintenance key="diagnostics" storage={false}/>:<Updates key={opened} version={version}/>}
  </div>
 </main>;
}

function General() {
 const [scale,setScale]=useState<number|null>(null),[permission,setPermission]=useState(''),[error,setError]=useState('');
 const pending=useRef<number|null>(null),pumping=useRef(false),save=useRef(false),dragging=useRef(false);
 useEffect(()=>{
  void desktop<Placement>('Placement').then(p=>setScale(p.scale)).catch(()=>setError('暂时无法读取桌宠大小'));
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
  }catch{pending.current=null;save.current=false;setError('暂时无法保存桌宠大小，请重试');}
  finally{pumping.current=false;}
 };
 const commit=()=>{dragging.current=false;save.current=true;void flush();};
 const status:Record<string,string>={authorized:'已开启',denied:'未开启',notDetermined:'尚未开启',unavailable:'暂不可用'};
 return <section className="general-settings">
  <h1>常规</h1><ShortcutSettings/>
  <SettingGroup title="桌宠">
   <SettingRow label="角色大小" htmlFor="pet-size"><div className="size-control">
    <input id="pet-size" type="range" min="0.65" max="1.6" step="any" disabled={scale===null} value={scale??1} aria-valuetext={scale===null?'':`${Math.round(scale*100)}%`} onPointerDown={()=>{dragging.current=true;}} onChange={event=>{const value=event.target.valueAsNumber;setScale(value);pending.current=value;save.current=!dragging.current;setError('');void flush();}} onPointerUp={commit} onPointerCancel={commit} onBlur={commit}/>
    <output htmlFor="pet-size">{scale===null?'—':`${Math.round(scale*100)}%`}</output>
   </div></SettingRow>
  </SettingGroup>
  <SettingGroup title="通知"><SettingRow label="系统通知" description={<span role="status">{status[permission]??'正在读取…'}</span>}><button disabled={!permission||permission==='unavailable'} onClick={()=>void desktop('ConfigureNotifications').catch(()=>setError('暂时无法打开通知设置'))}>{permission==='notDetermined'?'开启通知':'打开系统设置'}</button></SettingRow></SettingGroup>
  {error&&<p role="alert" className="inline-error">{error}</p>}
 </section>;
}

function Updates({version}:{version:string}) {
 const [result,setResult]=useState<Update|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const checking=useRef(false);
 const check=async()=>{
  if(checking.current)return;
  checking.current=true;setBusy(true);setError('');
  try{setResult(await desktop<Update>('CheckUpdates'));}catch{setError('暂时无法检查更新，请重试或前往发布页查看。');}finally{checking.current=false;setBusy(false);}
 };
 useEffect(()=>{void check();},[]);
 return <section className="update-settings">
  <h1>关于</h1>
  <div className="about-identity"><img src="/icons/caelis-avatar.png" alt="" width="64" height="64"/><div><h2>Caelis Bot</h2><p>{result?.current||version}</p></div></div>
  <SettingGroup><SettingRow label="软件更新" description={<span role="status">{busy?'正在检查…':error||result?.message||'检查新版本'}</span>}><button disabled={busy} onClick={()=>void check()}>检查更新</button></SettingRow>
  <SettingRow label={result?.state==='available'?`新版本 ${result.latest}`:'发布与安装'}><button onClick={()=>void desktop('OpenReleasePage').catch(()=>setError('暂时无法打开发布页'))}>{result?.state==='available'?'下载新版本':'查看发布页'}</button></SettingRow></SettingGroup>
  <p className="settings-note">更新需手动安装。</p>
 </section>;
}
