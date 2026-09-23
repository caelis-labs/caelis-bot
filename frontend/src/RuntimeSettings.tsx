import { useEffect, useRef, useState } from 'react';
import { backend, desktop } from './desktop';
import { SettingGroup, SettingRow } from './SettingsUI';
import type { RuntimeSettings as Profile, SetupState, SetupOverview, SetupRequest, SetupChoice } from './backend/contract';

const names:Record<string,string>={caelis:'Caelis',codex:'Codex'};
const empty=(runtime:string):Profile=>({runtime,cliPath:'',caelisStore:''});
const request=(settings:Profile,action:string,fields:Partial<SetupRequest>={}):SetupRequest=>({settings,action,provider:'',baseUrl:'',model:'',apiKey:'',...fields});
const errorText=(e:unknown)=>e instanceof Error?e.message:'操作未完成，请重试';

export function RuntimeSettings({onboarding=false,onDone}:{onboarding?:boolean;onDone?:()=>void}) {
 const [overview,setOverview]=useState<SetupOverview|null>(null),[id,setID]=useState('');
 const [profile,setProfile]=useState<Profile>(empty('')),[state,setState]=useState<SetupState|null>(null);
 const [busy,setBusy]=useState(''),[error,setError]=useState(''),[notice,setNotice]=useState(''),[manual,setManual]=useState(false);
 const [form,setForm]=useState<'model'|'key'|''>(''),[confirm,setConfirm]=useState<SetupRequest|null>(null);
 const epoch=useRef(0),working=useRef(false),alive=useRef(true);
 const name=names[id]??'',blocked=!!busy;
 const refreshOverview=async()=>{const v=await backend<SetupOverview>('SetupOverview');if(alive.current)setOverview(v);return v;};
 useEffect(()=>{alive.current=true;void refreshOverview().then(v=>{if(!onboarding)setID(v.pending||v.active);}).catch(e=>setError(errorText(e)));const close=()=>setForm('');window.addEventListener('settings-close',close);return()=>{alive.current=false;epoch.current++;window.removeEventListener('settings-close',close);};},[onboarding]);
 useEffect(()=>{
  if(!id)return;
  const generation=++epoch.current;setState(null);setForm('');setError('');setNotice('');setBusy('detect');working.current=true;
  void backend<Profile>('SetupProfile',id).then(async p=>{if(generation!==epoch.current)return;setProfile(p);setManual(!!p.cliPath);const v=await backend<SetupState>('InspectSetup',p);if(generation===epoch.current)setState(v);}).catch(e=>{if(generation===epoch.current)setError(errorText(e));}).finally(()=>{if(generation===epoch.current){working.current=false;setBusy('');}});
 },[id]);
 const run=async(action:string,fields:Partial<SetupRequest>={})=>{
  if(working.current)return;working.current=true;setBusy(action);setError('');setNotice('');
  try{const v=action==='detect'?await backend<SetupState>('InspectSetup',profile):await backend<SetupState>('ApplySetup',request(profile,action,fields));if(alive.current){setState(v);setForm('');if(['install','update','check-update'].includes(action))setNotice(v.message);}}
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
   <h1>运行时</h1><SettingGroup><SettingRow label="当前使用" description={overview?.pending?`待切换到 ${names[overview.pending]}，重新启动后生效`:'各运行时保留独立的对话和设置'}><span>{names[overview?.active??'']??'—'}</span></SettingRow></SettingGroup>
   <div className="runtime-selection"><div className="runtime-tabs" aria-label="管理运行时">{['caelis','codex'].map(v=><button key={v} disabled={blocked} aria-pressed={id===v} onClick={()=>setID(v)}>{names[v]}</button>)}</div>{id&&overview&&id!==overview.active&&<span className="runtime-switch" title={switchHint} tabIndex={switchDisabled?0:undefined} aria-label={switchDisabled?`无法切换至 ${name}：${switchHint}`:undefined}><button className="primary" disabled={switchDisabled} onClick={()=>setConfirm(request(profile,'switch'))}>切换至 {name}</button></span>}</div>
  </>}
  <SettingGroup title={onboarding?undefined:'本机安装'}><SettingRow label={state?.installation.installed?`${name} · ${state.installation.version}`:state?labels[state.state]:busy==='detect'?'正在检测…':'尚未检测'} description={state?.installation.installed?<span className="setup-path" title={state.installation.path}>{state.installation.path}</span>:undefined}>
   {state?.installation.installed&&<button disabled={blocked} onClick={()=>void run('detect')}>重新检测</button>}
  </SettingRow></SettingGroup>
  {(!state||state.state==='missing'||state.state==='incompatible')&&<div className="runtime-actions"><button className={state?.installation.installed?undefined:'primary'} disabled={blocked||(!state&&!error)} onClick={()=>void run(state?.installation.installed?'check-update':'install')}>{state?.installation.installed?'检查更新':`安装 ${name}`}</button><button disabled={blocked} onClick={()=>void pick()}>选择已有程序</button></div>}
  {state?.state==='service'&&<SettingGroup><SettingRow label="启动 Caelis" description="让本机 Caelis 准备接受连接"><button className="primary" disabled={blocked} onClick={()=>void run('start')}>启动</button></SettingRow></SettingGroup>}
  {state?.installation.installed&&state.state!=='service'&&state.state!=='incompatible'&&state.state!=='unavailable'&&<>
   {id==='codex'&&<SettingGroup title="账号"><SettingRow label={state.loginPending?'等待浏览器登录':state.state==='ready'?'已连接 Codex':'登录 Codex'} description={state.state==='ready'?(state.accountType==='chatgpt'?'ChatGPT 账号':state.accountType==='apiKey'?'OpenAI API Key':'使用 Codex 的连接配置'):undefined}>
    <div className="runtime-actions">{state.loginPending?<><button disabled={blocked} onClick={()=>void run('login')}>打开登录页</button><button disabled={blocked} onClick={()=>void run('cancel-login')}>取消</button></>:<><button disabled={blocked} onClick={()=>void run('login')}>{state.state==='ready'?'重新登录':'使用 ChatGPT 登录'}</button>{state.state==='ready'&&<button disabled={blocked} onClick={()=>setConfirm(request(profile,'logout'))}>退出登录</button>}</>}</div>
   </SettingRow>{!state.loginPending&&<SettingRow label="OpenAI API Key"><button disabled={blocked} onClick={()=>setForm('key')}>使用 API Key</button></SettingRow>}</SettingGroup>}
   {id==='caelis'&&<SettingGroup title="模型"><SettingRow label={state.models.length?'已连接的模型':'还没有连接模型'} description="模型和认证信息保存在 Caelis 中"><button disabled={blocked} onClick={()=>setForm('model')}>连接新模型</button></SettingRow>{state.models.map(m=><SettingRow key={m.value} label={m.label} description={m.noAuth?'需要配置认证':m.value===state.selectedModel?'Bot 使用中':m.current?'Caelis 默认模型':undefined}><div className="runtime-actions"><button disabled={blocked||m.noAuth||m.value===state.selectedModel} onClick={()=>void run('use-model',{model:m.value})}>使用</button><button disabled={blocked||m.current||m.value===state.selectedModel} aria-label={`移除 ${m.label}`} onClick={()=>setConfirm(request(profile,'remove-model',{model:m.value}))}>移除</button></div></SettingRow>)}</SettingGroup>}
   {id==='codex'&&state.state==='ready'&&state.models.length>0&&<SettingGroup title="模型"><SettingRow label="Bot 使用模型" description="推理强度与权限可在「模型与权限」中调整"><select aria-label="Bot 使用模型" disabled={blocked} value={state.selectedModel} onChange={e=>void run('use-model',{model:e.target.value})}><option value="" disabled>选择模型</option>{state.models.map(m=><option key={m.value} value={m.value}>{m.label}</option>)}</select></SettingRow></SettingGroup>}
  </>}
  <details className="runtime-advanced" open={manual||undefined}><summary>更多管理</summary>
   <SettingGroup><SettingRow label="程序位置" description={manual?'选择本机已有的可执行文件':'自动发现本机安装'}><button disabled={blocked} onClick={()=>{setManual(v=>!v);setProfile(p=>({...p,cliPath:''}));setState(null);}}>{manual?'使用自动发现':'更改'}</button></SettingRow>
    {manual&&<div className="setup-path-field"><input aria-label="程序路径" disabled={blocked} value={profile.cliPath} placeholder="输入可执行文件的完整路径" onChange={e=>{setProfile(p=>({...p,cliPath:e.target.value}));setState(null);}}/><button disabled={blocked} onClick={()=>void pick()}>选择文件</button></div>}
    {id==='caelis'&&<SettingRow label="Caelis 数据目录"><input aria-label="Caelis 数据目录" disabled={blocked} value={profile.caelisStore??''} placeholder="默认 ~/.caelis" onChange={e=>{setProfile(p=>({...p,caelisStore:e.target.value}));setState(null);}}/></SettingRow>}
    <SettingRow label="检查连接"><button disabled={blocked} onClick={()=>void run('detect')}>检测并保存</button></SettingRow>
    {state?.installation.installed&&<SettingRow label="运行时更新"><div className="runtime-actions"><button disabled={blocked} onClick={()=>void run('check-update')}>检查更新</button><button disabled={blocked} onClick={()=>setConfirm(request(profile,'update'))}>更新</button></div></SettingRow>}
    <SettingRow label="安装与卸载"><button onClick={()=>void backend('OpenMessageLink',id==='caelis'?'https://caelis.dev':'https://learn.chatgpt.com/docs/codex/cli')}>查看官方指引</button></SettingRow>
   </SettingGroup>
  </details>
  {busy&&<p className="settings-note" role="status">{busy==='install'?'正在下载并安装…':busy==='update'?'正在更新…':busy==='activate'?'正在准备重新启动…':'正在处理…'}</p>}
  {error?<p className="inline-error" role="alert">{error}</p>:notice?<p className="settings-note" role="status">{notice}</p>:state?.message&&(state.state!=='incompatible'||onboarding||overview?.active===id)&&<p className="settings-note" role="status">{state.message}</p>}
  {state?.state==='ready'&&(onboarding||overview?.active===id)&&<div className="setup-end"><button className={onboarding?'primary':undefined} disabled={blocked} onClick={()=>void activate()}>{onboarding?'重新启动并开始':'重新启动并应用'}</button></div>}
  {form&&<ModelForm kind={form} profile={profile} busy={blocked} onClose={()=>setForm('')} onSubmit={async fields=>{return await run(form==='key'?'api-key':'connect-model',fields);}}/>}
  {confirm&&<div className="setup-scrim"><section role="alertdialog" aria-modal="true" aria-labelledby="setup-confirm-title" className="setup-dialog"><h2 id="setup-confirm-title">{confirm.action==='switch'?`切换到 ${name}？`:confirm.action==='logout'?'退出 Codex 登录？':confirm.action==='update'?`更新 ${name}？`:'移除此模型？'}</h2><p>{confirm.action==='switch'?'Caelis Bot 将重新启动，各运行时的对话和配置独立保留。':<>这会影响共享该运行时配置的其他应用。{confirm.action==='update'?'正在运行的共享服务不会自动重启。':''}</>}</p><div className="setup-end"><button autoFocus onClick={()=>setConfirm(null)}>取消</button><button className="primary" onClick={()=>{const r=confirm;setConfirm(null);if(r.action==='switch')void activate();else void run(r.action,r);}}>{confirm.action==='switch'?'切换并重启':'确认'}</button></div></section></div>}
 </section>;
}

function ModelForm({kind,profile,busy,onClose,onSubmit}:{kind:'model'|'key';profile:Profile;busy:boolean;onClose:()=>void;onSubmit:(v:Partial<SetupRequest>)=>Promise<string|undefined>}) {
 const [providers,setProviders]=useState<SetupChoice[]>([]),[endpoints,setEndpoints]=useState<SetupChoice[]>([]),[models,setModels]=useState<SetupChoice[]>([]);
 const [provider,setProvider]=useState(''),[baseUrl,setBaseURL]=useState(''),[model,setModel]=useState(''),[key,setKey]=useState(''),[error,setError]=useState(''),[loading,setLoading]=useState(false);
 const formRef=useRef<HTMLDivElement>(null);
 useEffect(()=>{if(kind!=='model')return;let valid=true;setLoading(true);void backend<SetupChoice[]>('SetupCatalog',request(profile,'providers')).then(v=>{if(valid)setProviders(v);}).catch(e=>{if(valid)setError(errorText(e));}).finally(()=>{if(valid)setLoading(false);});return()=>{valid=false;};},[kind,profile]);
 useEffect(()=>{if(!provider)return;let valid=true;setLoading(true);setKey('');setBaseURL('');setModels([]);void backend<SetupChoice[]>('SetupCatalog',request(profile,'endpoints',{provider})).then(v=>{if(valid){setEndpoints(v);setBaseURL(v[0]?.value??'');}}).catch(e=>{if(valid)setError(errorText(e));}).finally(()=>{if(valid)setLoading(false);});return()=>{valid=false;};},[provider]);
 useEffect(()=>{if(!provider)return;let valid=true;const timer=window.setTimeout(()=>{void backend<SetupChoice[]>('SetupCatalog',request(profile,'models',{provider,baseUrl})).then(v=>{if(valid)setModels(v);}).catch(()=>{if(valid)setModels([]);});},250);return()=>{valid=false;clearTimeout(timer);};},[provider,baseUrl]);
 useEffect(()=>{const prior=document.activeElement as HTMLElement|null;formRef.current?.querySelector<HTMLElement>('select,input,button')?.focus();return()=>prior?.focus();},[]);
 const close=()=>{setKey('');onClose();};
 const reuse=(providers.some(p=>p.value===provider&&p.noAuth)&&baseUrl===(endpoints[0]?.value??''))||endpoints.some(e=>e.value===baseUrl&&e.noAuth);
 const customEndpoint=provider.includes('compatible')||!endpoints[0]?.value;
 return <div className="setup-scrim" onPointerDown={e=>{if(e.target===e.currentTarget&&!busy)close();}}><div className="setup-dialog" ref={formRef} role="dialog" aria-modal="true" aria-labelledby="setup-form-title" onKeyDown={e=>{
  if(e.key==='Escape'){e.stopPropagation();if(!busy)close();}
  if(e.key==='Tab'){const nodes=formRef.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),summary');if(nodes?.length){const first=nodes[0],last=nodes[nodes.length-1];if(e.shiftKey&&document.activeElement===first){e.preventDefault();last.focus();}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus();}}}
 }}>
  <h2 id="setup-form-title">{kind==='key'?'使用 OpenAI API Key':'连接新模型'}</h2><p>{kind==='key'?'密钥交给 Codex 保存。':'由 Caelis 保存配置和认证信息。'}</p>
  <form onSubmit={e=>{e.preventDefault();const apiKey=key;setKey('');void onSubmit({provider,baseUrl,model,apiKey}).then(message=>{if(message)setError(message);});}}>
   {kind==='model'&&<><label>模型服务<select required disabled={busy||loading} value={provider} onChange={e=>{setProvider(e.target.value);setModel('');setError('');}}><option value="">选择服务</option>{providers.map(p=><option key={p.value} value={p.value}>{p.label}</option>)}</select></label>
    {provider&&<label>模型<input required disabled={busy} list="setup-models" value={model} autoComplete="off" placeholder="选择或输入模型名称" onChange={e=>setModel(e.target.value)}/><datalist id="setup-models">{models.map(m=><option key={m.value} value={m.value}>{m.label}</option>)}</datalist></label>}
   </>}
   {(kind==='key'||provider)&&<label>API Key{reuse&&<span className="settings-note">可留空，使用已有认证或本地服务</span>}<input type="password" required={!reuse} disabled={busy} value={key} autoComplete="off" spellCheck={false} placeholder={reuse?'使用已有认证':'输入 API Key'} onChange={e=>setKey(e.target.value)}/></label>}
   {kind==='model'&&provider&&(customEndpoint?<label>服务地址<input type="url" required disabled={busy} value={baseUrl} placeholder="https://…/v1" onChange={e=>{setBaseURL(e.target.value);setKey('');}}/></label>:<details className="runtime-advanced"><summary>服务地址</summary><label>API 地址<input type="url" disabled={busy} value={baseUrl} list="setup-endpoints" placeholder="默认服务地址" onChange={e=>{setBaseURL(e.target.value);setKey('');}}/><datalist id="setup-endpoints">{endpoints.map(p=><option key={p.value} value={p.value}>{p.label}</option>)}</datalist></label></details>)}
   {error&&<p role="alert" className="inline-error">{error}</p>}
   <div className="setup-end"><button type="button" disabled={busy} onClick={close}>取消</button><button className="primary" disabled={busy||loading||(!reuse&&!key.trim())||(kind==='model'&&(!provider||!model.trim()||(customEndpoint&&!baseUrl.trim())))}>{busy?'正在连接…':'连接'}</button></div>
  </form>
 </div></div>;
}
