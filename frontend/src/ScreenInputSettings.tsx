import {useEffect,useState} from 'react';
import {desktop} from './desktop';
import {SettingGroup,SettingRow} from './SettingsUI';
import {useI18n} from './i18n';
import {ShortcutSettings} from './ShortcutSettings';
type Preferences={enabled:boolean;includeBackground:boolean;notice:string};
export function ScreenInputSettings() {
 const {t}=useI18n(),[value,setValue]=useState<Preferences|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(false);
 useEffect(()=>{let live=true;void desktop<Preferences>('CapturePreferences').then(v=>{if(live){setValue(v);setError(!!v.notice)}}).catch(()=>{if(live)setError(true)});return()=>{live=false}},[]);
 const save=async(method:'SetCaptureEnabled'|'SaveCapturePreferences',next:boolean)=>{
  setBusy(true);try{setValue(await (method==='SetCaptureEnabled'?desktop<Preferences>(method,next):desktop<Preferences>(method,{includeBackground:next,notice:''})));setError(false)}catch{setError(true)}finally{setBusy(false)}
 };
 return <><p className="settings-page-intro">{t('settings.extrasIntro')}</p><SettingGroup title={t('settings.extrasCaptureTitle')}>
  <SettingRow label={t('settings.extrasCaptureToggle')} description={t('settings.extrasCaptureDescription')} htmlFor="capture-enabled">
   <input id="capture-enabled" className="settings-switch" type="checkbox" role="switch" checked={value?.enabled??false} disabled={!value||busy} onChange={e=>void save('SetCaptureEnabled',e.target.checked)}/>
  </SettingRow>
  {value?.enabled&&<>
  <SettingRow label={t('settings.screenContext')} description={t('settings.screenContextDescription')} htmlFor="screen-context">
   <input id="screen-context" className="settings-switch" type="checkbox" role="switch" checked={value?.includeBackground??false} disabled={busy} onChange={e=>void save('SaveCapturePreferences',e.target.checked)}/>
  </SettingRow>
  <ShortcutSettings capture/><ShortcutSettings paste/>
  </>}
  {error&&<p role="alert" className="settings-note">{t('settings.screenPreferencesFailed')}</p>}
 </SettingGroup></>;
}
