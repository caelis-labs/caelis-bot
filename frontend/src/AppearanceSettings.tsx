import { useEffectEvent, useEffect, useRef, useState } from 'react';
import { desktop } from './desktop';
import { acceptAppearance, type ContentState, type Selection } from './appearance';
import { SettingGroup, SettingRow } from './SettingsUI';
import { useI18n } from './i18n';

export function AppearanceSettings(){
 const {t}=useI18n();
 const loadFailed=useEffectEvent(()=>t('settings.appearanceLoadFailed'));
 const [state,setState]=useState<ContentState|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const running=useRef(false),alive=useRef(true);
 const receive=(next:ContentState)=>{acceptAppearance(next.appearance);if(alive.current)setState(old=>!old||next.appearance.revision>=old.appearance.revision?next:old);};
 useEffect(()=>{alive.current=true;const refresh=()=>{void desktop<ContentState>('ContentState').then(receive).catch(()=>{if(alive.current)setError(loadFailed());});};refresh();window.addEventListener('appearance-changed',refresh);return()=>{alive.current=false;window.removeEventListener('appearance-changed',refresh);};},[]);
 const run=async(method:string,...args:unknown[])=>{if(running.current)return;running.current=true;setBusy(true);setError('');try{receive(await desktop<ContentState>(method,...args));}catch(e){if(alive.current)setError(e instanceof Error?e.message:String(e));}finally{running.current=false;if(alive.current)setBusy(false);}};
 const select=(value:Partial<Selection>)=>{if(state){const s={...state.appearance.selection,...value};void run('SelectAppearance',s.character,s.avatar);}};
 return <section><h1>{t('settings.appearance')}</h1>
  <SettingGroup title={t('settings.appearanceFigure')}>
   <SettingRow label={t('settings.characterAndCostume')} htmlFor="appearance-character"><select id="appearance-character" disabled={!state||busy} value={state?.appearance.selection.character??'builtin:caelis'} onChange={e=>select({character:e.target.value})}>{state?.characters.map(c=><option key={c.id} value={c.id}>{c.name}{c.pack?` — ${c.group}`:''}</option>)}</select></SettingRow>
   <SettingRow label={t('settings.chatAvatar')} htmlFor="appearance-avatar"><select id="appearance-avatar" disabled={!state||busy} value={state?.appearance.selection.avatar??'follow'} onChange={e=>select({avatar:e.target.value})}>{state?.avatars.map(a=><option key={a.id} value={a.id}>{a.name}{a.pack?` — ${a.group}`:''}</option>)}</select></SettingRow>
  </SettingGroup>
  <SettingGroup title={t('settings.contentPacks')}><SettingRow label={t('settings.importContentPack')} description={t('settings.importContentPackDescription')}><button disabled={busy||!state} onClick={()=>void run('ImportContent')}>{busy?t('common.loading'):t('settings.importButton')}</button></SettingRow>
   {state?.packs.map(p=><SettingRow key={p.id} label={`${p.name} · ${p.version}`} description={t('settings.importedPackDetails',{author:p.author,license:p.license})}><button disabled={busy||p.active} title={p.active?t('settings.packInUseHint'):''} onClick={()=>void run('RemoveContent',p.id)}>{p.active?t('settings.packActive'):t('settings.packRemove')}</button></SettingRow>)}
  </SettingGroup>
  {(error||state?.notice)&&<p role={error?'alert':'status'} className={error?'inline-error':'settings-note'}>{error||state?.notice}</p>}
 </section>;
}
