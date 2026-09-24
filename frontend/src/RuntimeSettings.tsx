import { useEffect, useMemo, useRef, useState } from 'react';
import { backend, desktop } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';
import { ConnectionWizard } from './settings/runtime/ConnectionWizard';
import { createRuntimeSettingsClient } from './settings/runtime/client';
import { SettingsDialog } from './settings/runtime/SettingsDialog';
import { RuntimeWorkspace } from './settings/runtime/RuntimeWorkspace';
import type { RuntimeSettings as Profile, SetupState, SetupOverview, SetupRequest } from './backend/contract';

const names:Record<string,string>={caelis:'Caelis',codex:'Codex'};
const empty=(runtime:string):Profile=>({runtime,cliPath:'',caelisStore:''});
const request=(settings:Profile,action:string,fields:Partial<SetupRequest>={}):SetupRequest=>({settings,action,provider:'',baseUrl:'',model:'',apiKey:'',...fields});
const errorText=(e:unknown)=>e instanceof Error?e.message:'操作未完成，请重试';

export function RuntimeSettings({onboarding=false,onDone}:{onboarding?:boolean;onDone?:()=>void}) {
 return onboarding ? <RuntimePreparation onboarding onDone={onDone}/> : <RuntimeWorkspace preparation={(id,onBusy)=><RuntimePreparation initialRuntime={id} onBusy={onBusy}/>}/>;
}

function RuntimePreparation({onboarding=false,onDone,initialRuntime='',onBusy}:{onboarding?:boolean;onDone?:()=>void;initialRuntime?:string;onBusy?:(busy:boolean)=>void}) {
 const [activeProfile,setActiveProfile]=useState<Profile|null>(null),[restartNeeded,setRestartNeeded]=useState(false);
 const [overview,setOverview]=useState<SetupOverview|null>(null),[id,setID]=useState(initialRuntime);
 const [profile,setProfile]=useState<Profile>(empty('')),[state,setState]=useState<SetupState|null>(null);
 const [busy,setBusy]=useState(''),[error,setError]=useState(''),[notice,setNotice]=useState(''),[manual,setManual]=useState(false);
 const [form,setForm]=useState<'model'|'key'|''>(''),[confirm,setConfirm]=useState<SetupRequest|null>(null);
 useEffect(()=>{void backend<Profile>('RuntimeSettings').then(setActiveProfile).catch(()=>{});void backend<{connection:string}>('ComposerSnapshot').then(s=>{if(!['ready','connecting'].includes(s.connection))setRestartNeeded(true);}).catch(()=>{});},[]);
 const epoch=useRef(0),working=useRef(false),alive=useRef(true);
 const connectionClient=useMemo(()=>createRuntimeSettingsClient(backend,profile),[profile]);
 const name=names[id]??'',blocked=!!busy;
 useEffect(()=>{onBusy?.(blocked||!!form||!!confirm);return()=>onBusy?.(false);},[blocked,form,confirm,onBusy]);
 const refreshOverview=async()=>{const v=await backend<SetupOverview>('SetupOverview');if(alive.current)setOverview(v);return v;};
 useEffect(()=>{alive.current=true;void refreshOverview().then(v=>{if(!onboarding&&!initialRuntime)setID(v.pending||v.active);}).catch(e=>setError(errorText(e)));const close=()=>setForm('');window.addEventListener('settings-close',close);return()=>{alive.current=false;epoch.current++;window.removeEventListener('settings-close',close);};},[onboarding]);
 useEffect(()=>{
  if(!id)return;
  const generation=++epoch.current;setState(null);setForm('');setError('');setNotice('');setBusy('detect');working.current=true;
  void backend<Profile>('SetupProfile',id).then(async p=>{if(generation!==epoch.current)return;setProfile(p);setManual(!!p.cliPath);const v=await backend<SetupState>('InspectSetup',p);if(generation===epoch.current)setState(v);}).catch(e=>{if(generation===epoch.current)setError(errorText(e));}).finally(()=>{if(generation===epoch.current){working.current=false;setBusy('');}});
 },[id]);
 const run=async(action:string,fields:Partial<SetupRequest>={})=>{
  if(working.current)return;working.current=true;setBusy(action);setError('');setNotice('');
  try{const v=action==='detect'?await backend<SetupState>('InspectSetup',profile):await backend<SetupState>('ApplySetup',request(profile,action,fields));if(alive.current){setState(v);setForm('');if(action==='detect')setNotice(v.state==='ready'?'连接检测通过。':v.message);if(['install','update','check-update'].includes(action))setNotice(v.message);if(profile.runtime===overview?.active&&['login','api-key','logout','update','install','start'].includes(action))setRestartNeeded(true);}}
  catch(e){const message=errorText(e);if(alive.current)setError(message);return message;}
  finally{working.current=false;if(alive.current)setBusy('');}
 };
 useEffect(()=>{if(!state?.loginPending)return;const timer=window.setInterval(()=>{if(!working.current)void run('detect');},2000);return()=>clearInterval(timer);},[state?.loginPending,profile]);
 const pick=async()=>{try{const path=await desktop<string>('PickRuntimeCLI');if(path){setManual(true);setProfile(p=>({...p,cliPath:path}));setState(null);}}catch(e){setError(errorText(e));}};
 const activate=async()=>{
  if(working.current)return;working.current=true;setBusy('activate');setError('');
  try{await backend('ActivateRuntime',profile);await refreshOverview();await desktop('RestartForRuntime');}
  catch(e){setError(errorText(e));working.current=false;setBusy('');}
 };
 const skip=async()=>{await backend('DismissSetup');onDone?.();};
 if(onboarding&&!id)return <section className="runtime-welcome">
  <img className="setup-avatar" src="/icons/caelis-avatar.png" alt=""/>
  <h1>让 Caelis Bot 准备好</h1><p className="setup-lead">选择一个运行时，开始和 Caelis Bot 对话。</p>
  <div className="runtime-choices">{['caelis','codex'].map(value=><button key={value} onClick={()=>setID(value)}><div><strong>使用 {names[value]}</strong><span>{value==='caelis'?'连接你喜欢的模型服务':'通过你的 ChatGPT 账号连接'}</span></div><span aria-hidden="true">→</span></button>)}</div>
  <button className="text-action" onClick={()=>void skip().catch(e=>setError(errorText(e)))}>稍后设置</button>{error&&<p role="alert" className="inline-error">{error}</p>}
 </section>;
 const labels:Record<string,string>={missing:`还没有安装 ${name}`,service:`准备 ${name}`,unavailable:'暂时无法连接',incompatible:'需要更新或更换程序',login:state?.loginPending?'等待登录':'连接账号',models:'连接你的第一个模型',ready:'连接已就绪'};
 const switchDisabled=blocked||state?.state!=='ready';
 const switchHint=blocked?'请等待当前操作完成':state?.state==='incompatible'?`当前 ${name} 服务的协议不兼容。请选择支持 Bot 的版本，更新程序后还需重启服务。`:state?.state==='ready'?`切换到 ${name}，重新启动后生效`:state?.message||'完成安装和连接后即可切换';
 return <section className={onboarding?'runtime-onboarding':'runtime-management'} aria-label="运行时设置">
  {onboarding?<><button className="text-action setup-back" disabled={blocked} onClick={()=>setID('')}>← 选择其他运行时</button><h1>准备 {name}</h1><p className="setup-lead">使用你电脑上的 {name}，或安装一个新版本。</p></>:<>
   {id&&overview&&id!==overview.active&&<div className="runtime-selection"><p className="settings-note">完成安装与连接后，切换到 {name}。</p><span className="runtime-switch" title={switchHint}><button className="primary" disabled={switchDisabled} onClick={()=>setConfirm(request(profile,'switch'))}>切换至 {name}</button></span></div>}
  </>}
  <SettingGroup title={onboarding?undefined:'本机安装'}><SettingRow label={state?.installation.installed?`${name} · ${state.installation.version}`:state?labels[state.state]:busy==='detect'?'正在检测…':'尚未检测'} description={state?.installation.installed?<span className="setup-path" title={state.installation.path}>{state.installation.path}</span>:undefined}>
   <button disabled={blocked||!id} onClick={()=>void run('detect')}>{blocked&&busy==='detect'?'正在检测…':'检测连接'}</button>
  </SettingRow></SettingGroup>
  {(state?.state==='missing'||state?.state==='incompatible')&&<div className="runtime-actions"><button className={state?.installation.installed?undefined:'primary'} disabled={blocked||(!state&&!error)} onClick={()=>void run(state?.installation.installed?'check-update':'install')}>{state?.installation.installed?'检查更新':`安装 ${name}`}</button><button disabled={blocked} onClick={()=>void pick()}>选择已有程序</button></div>}
  {state?.state==='service'&&<SettingGroup><SettingRow label="启动 Caelis" description="让本机 Caelis 准备接受连接"><button className="primary" disabled={blocked} onClick={()=>void run('start')}>启动</button></SettingRow></SettingGroup>}
  {state?.installation.installed&&state.state!=='service'&&state.state!=='incompatible'&&state.state!=='unavailable'&&<>
   {id==='codex'&&<SettingGroup title="账号"><SettingRow label={state.loginPending?'等待浏览器登录':state.state==='ready'?'已连接 Codex':'登录 Codex'} description={state.state==='ready'?(state.accountType==='chatgpt'?'ChatGPT 账号':state.accountType==='apiKey'?'OpenAI API Key':'使用 Codex 的连接配置'):undefined}>
    <div className="runtime-actions">{state.loginPending?<><button disabled={blocked} onClick={()=>void run('login')}>打开登录页</button><button disabled={blocked} onClick={()=>void run('cancel-login')}>取消</button></>:<><button disabled={blocked} onClick={()=>void run('login')}>{state.state==='ready'?'重新登录':'使用 ChatGPT 登录'}</button>{state.state==='ready'&&<button disabled={blocked} onClick={()=>setConfirm(request(profile,'logout'))}>退出登录</button>}</>}</div>
   </SettingRow>{!state.loginPending&&<SettingRow label="OpenAI API Key"><button disabled={blocked} onClick={()=>setForm('key')}>使用 API Key</button></SettingRow>}</SettingGroup>}
   {id==='caelis'&&(onboarding||!state.models.length)&&<SettingGroup title="模型连接"><SettingRow label={state.models.length?'已连接的模型':'还没有连接模型'} description="模型和认证信息保存在 Caelis 中"><button disabled={blocked} onClick={()=>setForm('model')}>连接新模型</button></SettingRow>{state.models.map(m=><SettingRow key={m.value} label={m.label} description={m.noAuth?'需要配置认证':m.value===state.selectedModel?'Bot 使用中':m.current?'Caelis 默认模型':undefined}><div className="runtime-actions">{onboarding&&<button disabled={blocked||m.noAuth||m.value===state.selectedModel} onClick={()=>void run('use-model',{model:m.value})}>使用</button>}<button disabled={blocked||m.current||m.value===state.selectedModel} aria-label={`移除 ${m.label}`} onClick={()=>setConfirm(request(profile,'remove-model',{model:m.value}))}>移除</button></div></SettingRow>)}</SettingGroup>}
   {onboarding&&id==='codex'&&state.state==='ready'&&state.models.length>0&&<SettingGroup title="模型"><SettingRow label="Bot 使用模型" description="可稍后在「运行时与模型」和「权限」中调整"><select aria-label="Bot 使用模型" disabled={blocked} value={state.selectedModel} onChange={e=>void run('use-model',{model:e.target.value})}><option value="" disabled>选择模型</option>{state.models.map(m=><option key={m.value} value={m.value}>{m.label}</option>)}</select></SettingRow></SettingGroup>}
  </>}
  <details className="runtime-advanced" open={manual||undefined}><summary>高级连接管理</summary>
   <SettingGroup><SettingRow label="程序位置" description={manual?'选择本机已有的可执行文件':'自动发现本机安装'}><button disabled={blocked} onClick={()=>{setManual(v=>!v);if(manual){setProfile(p=>({...p,cliPath:''}));setState(null);}}}>{manual?'使用自动发现':'更改'}</button></SettingRow>
    {manual&&<div className="setup-path-field"><input aria-label="程序路径" disabled={blocked} value={profile.cliPath} placeholder="输入可执行文件的完整路径" onChange={e=>{setProfile(p=>({...p,cliPath:e.target.value}));setState(null);}}/><button disabled={blocked} onClick={()=>void pick()}>选择文件</button></div>}
    {id==='caelis'&&<SettingRow label="Caelis 数据目录"><input aria-label="Caelis 数据目录" disabled={blocked} value={profile.caelisStore??''} placeholder="默认 ~/.caelis" onChange={e=>{setProfile(p=>({...p,caelisStore:e.target.value}));setState(null);}}/></SettingRow>}
    {state?.installation.installed&&<SettingRow label="运行时更新"><div className="runtime-actions"><button disabled={blocked} onClick={()=>void run('check-update')}>检查更新</button><button disabled={blocked} onClick={()=>setConfirm(request(profile,'update'))}>更新</button></div></SettingRow>}
    <SettingRow label="安装与卸载"><button onClick={()=>void backend('OpenMessageLink',id==='caelis'?'https://caelis.dev':'https://learn.chatgpt.com/docs/codex/cli')}>查看官方指引</button></SettingRow>
   </SettingGroup>
  </details>
  {busy&&<p className="settings-note" role="status">{busy==='install'?'正在下载并安装…':busy==='update'?'正在更新…':busy==='activate'?'正在准备重新启动…':'正在处理…'}</p>}
  {error?<p className="inline-error" role="alert">{error}</p>:notice?<p className="settings-note" role="status">{notice}</p>:state?.message&&(state.state!=='incompatible'||onboarding||overview?.active===id)&&<p className="settings-note" role="status">{state.message}</p>}
  {state?.state==='ready'&&(onboarding||(overview?.active===id&&(restartNeeded||overview.pending||!!activeProfile&&(profile.cliPath!==activeProfile.cliPath||(profile.caelisStore??'')!==(activeProfile.caelisStore??'')))))&&<div className="setup-end">{!onboarding&&<span className="settings-note">连接更改或重新连接需要重启。</span>}<button className={onboarding?'primary':undefined} disabled={blocked} onClick={()=>void activate()}>{onboarding?'重新启动并开始':'重新启动并应用'}</button></div>}
  {form==='model'&&<ConnectionWizard client={connectionClient} onClose={()=>setForm('')} onConnected={async()=>{setForm('');await run('detect');}}/>}
  {form==='key'&&<APIKeyForm busy={blocked} onClose={()=>setForm('')} onSubmit={key=>run('api-key',{apiKey:key})}/>}
  {confirm&&<div className="setup-scrim"><section role="alertdialog" aria-modal="true" aria-labelledby="setup-confirm-title" className="setup-dialog"><h2 id="setup-confirm-title">{confirm.action==='switch'?`切换到 ${name}？`:confirm.action==='logout'?'退出 Codex 登录？':confirm.action==='update'?`更新 ${name}？`:'移除此模型？'}</h2><p>{confirm.action==='switch'?'Caelis Bot 将重新启动，各运行时的对话和配置独立保留。':<>这会影响共享该运行时配置的其他应用。{confirm.action==='update'?'正在运行的共享服务不会自动重启。':''}</>}</p><div className="setup-end"><button autoFocus onClick={()=>setConfirm(null)}>取消</button><button className="primary" onClick={()=>{const r=confirm;setConfirm(null);if(r.action==='switch')void activate();else void run(r.action,r);}}>{confirm.action==='switch'?'切换并重启':'确认'}</button></div></section></div>}
 </section>;
}

function APIKeyForm({busy,onClose,onSubmit}:{busy:boolean;onClose:()=>void;onSubmit:(key:string)=>Promise<string|undefined>}) {
 const [key,setKey]=useState(''),[error,setError]=useState('');
 return <SettingsDialog title="使用 OpenAI API Key" description="密钥交给 Codex 保存。" busy={busy} onClose={()=>{setKey('');onClose();}}>
  <form onSubmit={e=>{e.preventDefault();const secret=key;setKey('');void onSubmit(secret).then(message=>{if(message)setError(message);});}}>
   <label>API Key<input type="password" required disabled={busy} value={key} autoComplete="off" spellCheck={false} onChange={e=>setKey(e.target.value)}/></label>
   {error&&<p role="alert" className="inline-error">{error}</p>}
   <div className="setup-end"><button type="button" disabled={busy} onClick={onClose}>取消</button><button className="primary" disabled={busy||!key.trim()}>{busy?'正在连接…':'连接'}</button></div>
  </form>
 </SettingsDialog>;
}
