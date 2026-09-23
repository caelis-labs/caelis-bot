import { useEffect, useState } from 'react';
import { backend, desktop } from './desktop';
import { SettingGroup, SettingRow, SettingHelp } from './SettingsUI';
import type { ProviderInfo, RuntimeCheck, RuntimeStatus, RuntimeSettings as Settings } from './backend/contract';

export function RuntimeSettings() {
 const [settings,setSettings]=useState<Settings>({runtime:'',cliPath:'',caelisStore:''});
 const [providers,setProviders]=useState<ProviderInfo[]>([]);
 const [active,setActive]=useState<ProviderInfo|null>(null);
 const [automatic,setAutomatic]=useState(true),[loaded,setLoaded]=useState(false),[busy,setBusy]=useState('');
 const [status,setStatus]=useState<RuntimeStatus|null>(null);
 const [message,setMessage]=useState(''),[error,setError]=useState('');
 const provider=providers.find(p=>p.id===settings.runtime)??active;
 const caelis=settings.runtime==='caelis';
 useEffect(()=>{
  const load=()=>{void Promise.all([backend<Settings>('RuntimeSettings'),backend<ProviderInfo[]>('RuntimeProviders'),backend<ProviderInfo>('ProviderInfo')]).then(([value,list,info])=>{setActive(info);setProviders(list);setSettings(value);setAutomatic(!value.cliPath);setLoaded(true);setMessage('');setError('');setStatus(null);}).catch(()=>setError('暂时无法读取连接配置'));};
  load();window.addEventListener('settings-open',load);
  return()=>{window.removeEventListener('settings-open',load);};
 },[]);
 const value=()=>({...settings,cliPath:automatic?'':settings.cliPath.trim(),caelisStore:settings.caelisStore?.trim()});
 const pick=async()=>{
  setBusy('pick');setError('');
  try {const path=await desktop<string>('PickRuntimeCLI');if(path){setSettings({...settings,cliPath:path});setStatus(null);}}
  catch {setError('无法选择文件，请重试');}finally{setBusy('');}
 };
 const save=async()=>{
  setBusy('save');setMessage('');setError('');
  try {const result=await backend<RuntimeCheck>('SaveRuntimeSettings',value());setMessage(result.message);}
  catch(e){setError(e instanceof Error?e.message:'检测失败，原有配置未更改');}
  finally{setBusy('');}
 };
 const manage=async(action:string)=>{
  setBusy(action);setMessage('');setError('');
  try {const result=await backend<RuntimeStatus>('ManageRuntime',action,value());setStatus(result);setMessage(result.message);}
  catch(e){setError(e instanceof Error?e.message:'运行时操作未完成');}
  finally{setBusy('');}
 };
 const disabled=!!busy||!loaded;
 return <section aria-label="连接设置">
  <h1>连接</h1>
  <SettingGroup title="本机运行时">
   <SettingRow label="运行时" htmlFor="runtime-provider"><select id="runtime-provider" value={settings.runtime} disabled={disabled} onChange={e=>{setSettings({runtime:e.target.value,cliPath:'',caelisStore:''});setAutomatic(true);setStatus(null);setMessage('');setError('');}}>{providers.map(p=><option key={p.id} value={p.id}>{p.name}</option>)}</select></SettingRow>
   <SettingRow label={`自动查找 ${provider?.name??'运行时'}`} htmlFor="runtime-automatic"><input id="runtime-automatic" className="settings-switch" role="switch" type="checkbox" checked={automatic} disabled={disabled} onChange={e=>{setAutomatic(e.target.checked);setStatus(null);setMessage('');setError('');}}/></SettingRow>
   {!automatic&&<div className="setting-custom-path"><label htmlFor="runtime-path">{provider?.name} 路径</label><div className="runtime-path"><input id="runtime-path" aria-label={`${provider?.name??'运行时'} 路径`} type="text" autoComplete="off" spellCheck={false} value={settings.cliPath} disabled={disabled} placeholder="输入可执行文件的完整路径" onChange={e=>{setSettings({...settings,cliPath:e.target.value});setStatus(null);setMessage('');}}/><button disabled={disabled} onClick={()=>void pick()}>选择文件</button></div></div>}
  </SettingGroup>
  {caelis&&<SettingGroup title="Caelis 安装与服务">
   <SettingRow label={status?.installed?`已安装 · ${status.version}`:'本机安装'} description={status?.path||'由 Caelis 官方工具安装和更新'}><div className="runtime-actions"><button disabled={disabled} onClick={()=>void manage('detect')}>检测</button>{status&&!status.installed&&automatic&&<button disabled={disabled} onClick={()=>void manage('install')}>安装 Caelis</button>}{status?.installed&&<><button disabled={disabled} onClick={()=>void manage('check-update')}>检查更新</button><button disabled={disabled} onClick={()=>void manage('update')}>更新</button></>}</div></SettingRow>
   <SettingRow label="本机服务" description="通过 Caelis 启动服务，Bot 退出时服务继续运行"><button disabled={disabled} onClick={()=>void manage('start')}>启动服务</button></SettingRow>
   <div className="setting-custom-path"><label htmlFor="caelis-store">数据目录</label><div className="runtime-path"><input id="caelis-store" value={settings.caelisStore??''} disabled={disabled} autoComplete="off" spellCheck={false} placeholder="默认 ~/.caelis" onChange={e=>setSettings({...settings,caelisStore:e.target.value})}/></div></div>
  </SettingGroup>}
  {error?<p className="inline-error" role="alert">{error}</p>:message&&<p className="settings-note" role="status">{message}</p>}
  {busy&&<p className="settings-note" role="status">{busy==='install'?'正在安装 Caelis…':busy==='update'?'正在更新 Caelis…':'正在处理…'}</p>}
  <div className="settings-footer"><button className="text-action" onClick={()=>void backend('OpenMessageLink',provider?.helpUrl??'https://caelis.dev')}>安装说明</button><button className="primary" disabled={disabled||(!automatic&&!settings.cliPath.trim())} onClick={()=>void save()}>检测并保存</button></div>
  <SettingHelp><p>{provider?.connectionHint}</p>{caelis&&<p>请先在 Caelis 中配置模型。安装或更新后启动服务，再检测并保存连接。后端切换在重新启动 Bot 后生效。</p>}</SettingHelp>
 </section>;
}
