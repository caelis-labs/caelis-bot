import { useEffect, useRef, useState } from 'react';
import { desktop, type Placement } from './desktop';
import { RuntimeSettings } from './RuntimeSettings';
import { Maintenance } from './Maintenance';

const sections = [ ['general','桌宠与通知'], ['runtime','接入运行时'], ['storage','附件存储'], ['diagnostics','诊断'], ['updates','关于与更新'] ] as const;
type Section = typeof sections[number][0];
type Update = { state:string; current:string; latest:string; message:string };

export function Settings() {
 const [section,setSection]=useState<Section>('general'),[opened,setOpened]=useState(0),[version,setVersion]=useState('');
 useEffect(()=>{
  const load=()=>{void desktop<string>('SettingsSection').then(value=>{if(sections.some(([id])=>id===value))setSection(value as Section);setOpened(n=>n+1);});};
  const key=(event:KeyboardEvent)=>{if(!event.isComposing&&(event.key==='Escape'||(event.metaKey&&event.key==='w'))){event.preventDefault();void desktop('CloseSettings');}};
  load();void desktop<string>('AppVersion').then(setVersion);
  window.addEventListener('settings-open',load);window.addEventListener('keydown',key);
  return()=>{window.removeEventListener('settings-open',load);window.removeEventListener('keydown',key);};
 },[]);
 return <main className="settings-window">
  <aside><nav aria-label="设置分类">{sections.map(([id,label])=><button key={id} aria-current={section===id?'page':undefined} onClick={()=>setSection(id)}>{label}</button>)}</nav><small>Caelis Bot<br/>{version}</small></aside>
  <div className="settings-content">
   {section==='general'?<General key={opened}/>:section==='runtime'?<RuntimeSettings/>:section==='storage'?<Maintenance key="storage" storage/>:section==='diagnostics'?<Maintenance key="diagnostics" storage={false}/>:<Updates key={opened} version={version}/>}
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
  <h1>桌宠与通知</h1>
  <div className="settings-group">
   <label className="settings-row" htmlFor="pet-size"><span>桌宠大小</span><output>{scale===null?'—':`${Math.round(scale*100)}%`}</output></label>
   <input id="pet-size" type="range" min="0.65" max="1.6" step="any" disabled={scale===null} value={scale??1} aria-valuetext={scale===null?'':`${Math.round(scale*100)}%`} onPointerDown={()=>{dragging.current=true;}} onChange={event=>{const value=event.target.valueAsNumber;setScale(value);pending.current=value;save.current=!dragging.current;setError('');void flush();}} onPointerUp={commit} onPointerCancel={commit} onBlur={commit}/>
   <div className="settings-range-labels"><span>65%</span><span>160%</span></div>
   <p>单击打开输入框，双击打开聊天窗口。拖动角色可调整位置。</p>
  </div>
  <div className="settings-group settings-row"><div><label>系统通知</label><p role="status">{status[permission]??'正在读取…'}</p></div><button disabled={!permission||permission==='unavailable'} onClick={()=>void desktop('ConfigureNotifications').catch(()=>setError('暂时无法打开通知设置'))}>{permission==='notDetermined'?'开启通知…':'通知设置…'}</button></div>
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
  <h1>关于与更新</h1><h2>Caelis Bot</h2><p>当前版本 {result?.current||version}</p>
  <div className="update-result" role="status"><strong>{busy?'正在检查更新…':result?.state==='available'?`新版本 ${result.latest}`:'检查更新'}</strong><p>{error||(!busy&&result?.message)||'正在查看公开发布版本。'}</p></div>
  <div className="maintenance-actions"><button disabled={busy} onClick={()=>void check()}>再次检查</button><button onClick={()=>void desktop('OpenReleasePage').catch(()=>setError('暂时无法打开发布页'))}>{result?.state==='available'?'下载新版本 ↗':'查看发布页 ↗'}</button></div>
  <p className="settings-note">仅在打开此页或点击检查时联网，不会自动下载安装。早期预览版的安装说明见发布页与 README。</p>
 </section>;
}
