import { useEffect, useState } from 'react';
import { backend, desktop } from './desktop';
import type { RuntimeCheck, RuntimeSettings as Settings } from './backend/contract';

export function RuntimeSettings() {
 const [settings,setSettings]=useState<Settings>({runtime:'codex',cliPath:''});
 const [automatic,setAutomatic]=useState(true),[loaded,setLoaded]=useState(false),[busy,setBusy]=useState(false);
 const [message,setMessage]=useState(''),[error,setError]=useState('');
 useEffect(()=>{
  const load=()=>{void backend<Settings>('RuntimeSettings').then(value=>{setSettings(value);setAutomatic(!value.cliPath);setLoaded(true);setMessage('');setError('');}).catch(()=>setError('暂时无法读取连接配置'));};
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
 return <section className="runtime-settings" aria-label="接入运行时">
  <h1>接入运行时</h1>
  <label>运行时<select value={settings.runtime} disabled={busy||!loaded} onChange={e=>setSettings({...settings,runtime:e.target.value})}><option value="codex">Codex</option></select></label>
  <p className="runtime-explanation">优先连接本机已开放的 Codex 服务，不可用时使用 CLI。不会启动 Codex App。</p>
  <label className="runtime-automatic"><input type="checkbox" checked={automatic} disabled={busy||!loaded} onChange={e=>{setAutomatic(e.target.checked);setMessage('');setError('');}}/>自动发现 CLI</label>
  {!automatic&&<label>CLI 路径<div className="runtime-path"><input aria-label="Codex CLI 路径" type="text" autoComplete="off" spellCheck={false} value={settings.cliPath} disabled={busy||!loaded} placeholder="选择可执行文件，或输入完整路径" onChange={e=>{setSettings({...settings,cliPath:e.target.value});setMessage('');}}/><button disabled={busy||!loaded} onClick={()=>void pick()}>选择…</button></div></label>}
  <div className="runtime-result" aria-live="polite">{error?<p className="inline-error" role="alert">{error}</p>:<p>{message||'检测通过后保存，后续启动自动复用。'}</p>}</div>
  <footer><button className="text-action" onClick={()=>void backend('OpenConnectionHelp')}>安装说明 ↗</button><button className="runtime-save" disabled={!loaded||busy||(!automatic&&!settings.cliPath.trim())} onClick={()=>void save()}>{busy?'正在检测…':'检测并保存'}</button></footer>
 </section>;
}
