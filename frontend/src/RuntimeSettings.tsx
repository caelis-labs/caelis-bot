import { useEffect, useState } from 'react';
import { backend, desktop } from './desktop';
import { SettingGroup, SettingRow, SettingHelp } from './SettingsUI';
import type { ProviderInfo, RuntimeCheck, RuntimeSettings as Settings } from './backend/contract';

export function RuntimeSettings() {
 const [settings,setSettings]=useState<Settings>({runtime:'',cliPath:''});
 const [provider,setProvider]=useState<ProviderInfo|null>(null);
 const [automatic,setAutomatic]=useState(true),[loaded,setLoaded]=useState(false),[busy,setBusy]=useState(false);
 const [message,setMessage]=useState(''),[error,setError]=useState('');
 useEffect(()=>{
  const load=()=>{void Promise.all([backend<Settings>('RuntimeSettings'),backend<ProviderInfo>('ProviderInfo')]).then(([value,info])=>{setProvider(info);setSettings(value);setAutomatic(!value.cliPath);setLoaded(true);setMessage('');setError('');}).catch(()=>setError('暂时无法读取连接配置'));};
  load();window.addEventListener('settings-open',load);
  return()=>{window.removeEventListener('settings-open',load);};
 },[]);
 const pick=async()=>{
  setBusy(true);setError('');
  try {const path=await desktop<string>('PickRuntimeCLI');if(path)setSettings({...settings,cliPath:path});}
  catch {setError('无法选择文件，请重试');}finally{setBusy(false);}
 };
 const save=async()=>{
  setBusy(true);setMessage('');setError('');
  try {const result=await backend<RuntimeCheck>('SaveRuntimeSettings',{...settings,cliPath:automatic?'':settings.cliPath.trim()});setMessage(result.message);}
  catch(e){setError(e instanceof Error?e.message:'检测失败，原有配置未更改');}
  finally{setBusy(false);}
 };
 return <section aria-label="连接设置">
  <h1>连接</h1>
  <SettingGroup title="本机运行时">
   <SettingRow label="运行时" htmlFor="runtime-provider"><select id="runtime-provider" value={settings.runtime} disabled={busy||!loaded} onChange={e=>setSettings({...settings,runtime:e.target.value})}><option value={provider?.id??settings.runtime}>{provider?.name??'正在读取…'}</option></select></SettingRow>
   <SettingRow label={`自动查找 ${provider?.name??'运行时'}`} htmlFor="runtime-automatic"><input id="runtime-automatic" className="settings-switch" role="switch" type="checkbox" checked={automatic} disabled={busy||!loaded} onChange={e=>{setAutomatic(e.target.checked);setMessage('');setError('');}}/></SettingRow>
   {!automatic&&<div className="setting-custom-path"><label htmlFor="runtime-path">{provider?.name} 路径</label><div className="runtime-path"><input id="runtime-path" aria-label={`${provider?.name??'运行时'} 路径`} type="text" autoComplete="off" spellCheck={false} value={settings.cliPath} disabled={busy||!loaded} placeholder="输入可执行文件的完整路径" onChange={e=>{setSettings({...settings,cliPath:e.target.value});setMessage('');}}/><button disabled={busy||!loaded} onClick={()=>void pick()}>选择文件</button></div></div>}
  </SettingGroup>
  {error?<p className="inline-error" role="alert">{error}</p>:message&&<p className="settings-note" role="status">{message}</p>}
  <div className="settings-footer"><button className="text-action" onClick={()=>void backend('OpenConnectionHelp')}>安装说明</button><button className="primary" disabled={!loaded||busy||(!automatic&&!settings.cliPath.trim())} onClick={()=>void save()}>{busy?'正在检测…':'检测并保存'}</button></div>
  <SettingHelp><p>{provider?.connectionHint}</p></SettingHelp>
 </section>;
}
