import '../style.css';
import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { I18nProvider, useI18n } from './index';
import type { LanguageBridge, LanguageState } from './index';
import { LanguageSetting } from './LanguageSetting';
import { SettingGroup, SettingRow } from '../SettingsUI';

if(!import.meta.env.DEV)throw new Error('Language preview is development-only');
document.body.dataset.surface='settings';
const subscribers=new Set<(state:LanguageState)=>void>();
let state:LanguageState={preference:'system',locale:'en',revision:1};
const bridge:LanguageBridge={
 read:async()=>state,
 save:async preference=>{
  state={preference,locale:preference==='zh-CN'?'zh-CN':'en',revision:state.revision+1};
  subscribers.forEach(receive=>receive(state));return state;
 },
 subscribe:receive=>{subscribers.add(receive);return()=>{subscribers.delete(receive);};},
};
function Preview(){
 const {t,number,date}=useI18n();const [draft,setDraft]=useState('Draft stays / 草稿保留');
 const sections=['general','appearance','runtime','execution','storage','diagnostics','updates'] as const;
 return <main className="settings-window"><aside><nav aria-label={t('settings.navLabel')}>{sections.map(id=><button key={id} aria-current={id==='general'?'page':undefined}>{t(`settings.${id}`)}</button>)}</nav><small>Caelis Bot<br/>i18n preview</small></aside><div className="settings-content"><div className="settings-page"><h1>{t('settings.general')}</h1><LanguageSetting/>
  <SettingGroup title={t('settings.pet')}><SettingRow label={t('settings.petSize')}>{number(1,{style:'percent'})}</SettingRow></SettingGroup>
  <SettingGroup title={t('settings.notifications')}><SettingRow label={t('settings.systemNotifications')} description={t('settings.notificationEnabled')}><button>{t('settings.notificationTest')}</button><button>{t('settings.openSystemSettings')}</button></SettingRow></SettingGroup>
  <label htmlFor="sample-draft">Draft / 草稿</label><input id="sample-draft" value={draft} onChange={event=>setDraft(event.target.value)}/>
  <p>{date(Date.UTC(2026,8,24,16,30),{dateStyle:'medium',timeStyle:'short',timeZone:'Asia/Shanghai'})}</p>
  <I18nProvider bridge={bridge}><SecondSurface/></I18nProvider>
 </div></div></main>;
}
function SecondSurface(){const {t}=useI18n();return <p data-testid="second-surface">{t('native.settingsTitle')}</p>;}
createRoot(document.getElementById('root')!).render(<I18nProvider bridge={bridge}><Preview/></I18nProvider>);
