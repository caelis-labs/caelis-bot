import {Recovery} from './SurfaceBoundary';
import {recoverRead} from './read-recovery';
import { acceptDisplay, useDisplay, type DisplayState } from './theme/display';
import { SettingsSelect } from './SettingsSelect';
import { useEffectEvent, useEffect, useRef, useState } from 'react';
import { desktop } from './desktop';
import { acceptAppearance, type ContentState, type Selection } from './appearance';
import { SettingGroup, SettingRow } from './SettingsUI';
import { useI18n } from './i18n';
import { PetSizeSettings } from './PetSizeSettings';
import { CharacterPreview } from './CharacterPreview';

export function AppearanceSettings(){
 const {t}=useI18n();
 const display=useDisplay();
 const setMode=async(mode:DisplayState['mode'])=>{setBusy(true);setError('');try{acceptDisplay(await desktop<DisplayState>('SaveDisplay',mode,'default'));}catch{setError(t('settings.displaySaveFailed'));}finally{setBusy(false);}};
 const loadFailed=useEffectEvent(()=>t('settings.appearanceLoadFailed'));
 const [state,setState]=useState<ContentState|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const running=useRef(false),alive=useRef(true);
 const receive=(next:ContentState)=>{acceptAppearance(next.appearance);if(alive.current)setState(old=>!old||next.appearance.revision>=old.appearance.revision?next:old);};
 useEffect(()=>{alive.current=true;const refresh=()=>{void recoverRead(()=>desktop<ContentState>('ContentState')).then(next=>{receive(next);if(alive.current)setError('');}).catch(()=>{if(alive.current)setError(loadFailed());});};refresh();window.addEventListener('appearance-changed',refresh);return()=>{alive.current=false;window.removeEventListener('appearance-changed',refresh);};},[]);
 const run=async(method:string,...args:unknown[])=>{if(running.current)return;running.current=true;setBusy(true);setError('');try{receive(await desktop<ContentState>(method,...args));}catch(e){if(alive.current)setError(e instanceof Error?e.message:String(e));}finally{running.current=false;if(alive.current)setBusy(false);}};
 const select=(value:Partial<Selection>)=>{if(state){const s={...state.appearance.selection,...value};void run('SelectAppearance',s.character,'follow');}};
 return <section className="appearance-settings"><h1>{t('settings.appearance')}</h1>
  {state&&<CharacterPreview appearance={state.appearance}/>}
  <SettingGroup title={t('settings.appearanceFigure')}>
   <SettingRow label={t('settings.characterAndCostume')} htmlFor="appearance-character"><SettingsSelect id="appearance-character" disabled={!state||busy} value={state?.appearance.selection.character??'builtin:caelis'} onChange={e=>select({character:e.target.value})}>{state?.characters.filter(c=>c.id!=='builtin:stick').map(c=><option key={c.id} value={c.id}>{c.id==='builtin:caelis'?t('settings.defaultTheme'):c.name}{c.pack?` — ${c.group}`:''}</option>)}</SettingsSelect></SettingRow>
  <PetSizeSettings/></SettingGroup>
  <SettingGroup title={t('settings.interfaceAppearance')}>
   <SettingRow label={t('settings.colorMode')}><div className="runtime-segments" role="group" aria-label={t('settings.colorMode')}>{(['system','light','dark'] as const).map(mode=><button key={mode} disabled={busy||!display.revision} aria-pressed={display.mode===mode} onClick={()=>void setMode(mode)}>{t(mode==='system'?'settings.system':mode==='light'?'settings.lightMode':'settings.darkMode')}</button>)}</div></SettingRow>
   <SettingRow label={t('settings.theme')} htmlFor="appearance-theme"><SettingsSelect id="appearance-theme" value="default" onChange={()=>{}}><option value="default">{t('settings.defaultTheme')}</option></SettingsSelect></SettingRow>
  </SettingGroup>
  <SettingGroup title={t('settings.contentPacks')}><SettingRow label={t('settings.importContentPack')} description={t('settings.importContentPackDescription')}><button disabled={busy||!state} onClick={()=>void run('ImportContent')}>{busy?t('common.loading'):t('settings.importButton')}</button></SettingRow>
   {state?.packs.map(p=><SettingRow key={p.id} label={`${p.name} · ${p.version}`} description={t('settings.importedPackDetails',{author:p.author,license:p.license})}><button disabled={busy||p.active} title={p.active?t('settings.packInUseHint'):''} onClick={()=>void run('RemoveContent',p.id)}>{p.active?t('settings.packActive'):t('settings.packRemove')}</button></SettingRow>)}
  </SettingGroup>
  {error&&<Recovery message={error}/>}
 </section>;
}
