import { useEffect, useState } from 'react';
import { desktop } from './desktop';
import { History } from './Panel';
import { Settings } from './Settings';
import { useI18n } from './i18n';
import { GearSixIcon } from '@phosphor-icons/react/dist/csr/GearSix';

export function ContextWindow() {
 const {t}=useI18n();
 const [settings,setSettings]=useState(false);
 useEffect(()=>{
  let events=0,live=true;
  const openSettings=()=>{events++;setSettings(true);};
  const openHistory=()=>{events++;setSettings(false);};
  void desktop<boolean>('SettingsVisible').then(value=>{if(live&&!events)setSettings(value);}).catch(()=>{});
  window.addEventListener('settings-open',openSettings);
  window.addEventListener('history-open',openHistory);
  window.addEventListener('history-visible',openHistory);
  return()=>{live=false;window.removeEventListener('settings-open',openSettings);window.removeEventListener('history-open',openHistory);window.removeEventListener('history-visible',openHistory);};
 },[]);
 return <div className="context-window" data-view={settings?'settings':'chat'}>
  <div className="context-view chat-view" hidden={settings} inert={settings}><header className="context-chat-toolbar"><button aria-label={t('native.settings')} title={t('native.settings')} onClick={()=>void desktop('OpenSettings')}><GearSixIcon aria-hidden="true" size={18}/></button></header><History/></div>
  <div className="context-view settings-view" hidden={!settings} inert={!settings}><Settings/></div>
 </div>;
}
