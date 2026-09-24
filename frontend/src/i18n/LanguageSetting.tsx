import { useState } from 'react';
import { SettingGroup, SettingRow } from '../SettingsUI';
import { useI18n } from './index';
import type { LanguagePreference } from './core.ts';

export function LanguageSetting(){
 const {t,preference,setLanguage,failed}=useI18n();
 const [saving,setSaving]=useState(false),[saveFailed,setSaveFailed]=useState(false);
 const change=async(value:LanguagePreference)=>{
  setSaving(true);setSaveFailed(false);
  try{await setLanguage(value);}catch{setSaveFailed(true);}finally{setSaving(false);}
 };
 return <SettingGroup><SettingRow label={t('settings.language')} htmlFor="interface-language">
  <select id="interface-language" value={preference} disabled={saving} onChange={event=>void change(event.target.value as LanguagePreference)}>
   <option value="system">{t('settings.system')}</option><option value="en">English</option><option value="zh-CN">简体中文</option>
  </select>
 </SettingRow>{(saveFailed||failed)&&<p role="alert" className="inline-error">{t(saveFailed?'settings.languageSaveFailed':'settings.languageLoadFailed')}</p>}</SettingGroup>;
}
