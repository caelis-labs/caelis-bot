import { useEffect, useRef, useState } from 'react';
import { desktop } from './desktop';
import { acceptAppearance, type ContentState, type Selection } from './appearance';
import { SettingGroup, SettingRow } from './SettingsUI';

export function AppearanceSettings(){
 const [state,setState]=useState<ContentState|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const running=useRef(false),alive=useRef(true);
 const receive=(next:ContentState)=>{acceptAppearance(next.appearance);if(alive.current)setState(old=>!old||next.appearance.revision>=old.appearance.revision?next:old);};
 useEffect(()=>{alive.current=true;const refresh=()=>{void desktop<ContentState>('ContentState').then(receive).catch(()=>{if(alive.current)setError('暂时无法读取外观');});};refresh();window.addEventListener('appearance-changed',refresh);return()=>{alive.current=false;window.removeEventListener('appearance-changed',refresh);};},[]);
 const run=async(method:string,...args:unknown[])=>{if(running.current)return;running.current=true;setBusy(true);setError('');try{receive(await desktop<ContentState>(method,...args));}catch(e){if(alive.current)setError(e instanceof Error?e.message:String(e));}finally{running.current=false;if(alive.current)setBusy(false);}};
 const select=(value:Partial<Selection>)=>{if(state){const s={...state.appearance.selection,...value};void run('SelectAppearance',s.character,s.avatar);}};
 return <section><h1>外观</h1>
  <SettingGroup title="形象">
   <SettingRow label="角色与服装" htmlFor="appearance-character"><select id="appearance-character" disabled={!state||busy} value={state?.appearance.selection.character??'builtin:caelis'} onChange={e=>select({character:e.target.value})}>{state?.characters.map(c=><option key={c.id} value={c.id}>{c.name}{c.pack?` — ${c.group}`:''}</option>)}</select></SettingRow>
   <SettingRow label="聊天头像" htmlFor="appearance-avatar"><select id="appearance-avatar" disabled={!state||busy} value={state?.appearance.selection.avatar??'follow'} onChange={e=>select({avatar:e.target.value})}>{state?.avatars.map(a=><option key={a.id} value={a.id}>{a.name}{a.pack?` — ${a.group}`:''}</option>)}</select></SettingRow>
  </SettingGroup>
  <SettingGroup title="内容包"><SettingRow label="导入内容包" description="添加自己制作或获得授权的角色与头像"><button disabled={busy||!state} onClick={()=>void run('ImportContent')}>{busy?'正在处理…':'导入…'}</button></SettingRow>
   {state?.packs.map(p=><SettingRow key={p.id} label={`${p.name} · ${p.version}`} description={`本地导入 · ${p.author} · ${p.license}`}><button disabled={busy||p.active} title={p.active?'先切换正在使用的角色或头像':''} onClick={()=>void run('RemoveContent',p.id)}>{p.active?'使用中':'移除'}</button></SettingRow>)}
  </SettingGroup>
  {(error||state?.notice)&&<p role={error?'alert':'status'} className={error?'inline-error':'settings-note'}>{error||state?.notice}</p>}
 </section>;
}
