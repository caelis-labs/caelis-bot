import {useEffect,useState} from 'react';
import {desktop} from './desktop';
import {SettingGroup,SettingRow} from './SettingsUI';
import {useI18n} from './i18n';
import {ShortcutSettings} from './ShortcutSettings';
import type {FeatureCapabilities} from './capabilities';
type Preferences={enabled:boolean;includeBackground:boolean;notice:string};
export function ScreenInputSettings() {
 const {t}=useI18n(),[value,setValue]=useState<Preferences|null>(null),[capabilities,setCapabilities]=useState<FeatureCapabilities|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(false);
 useEffect(()=>{let live=true;const refresh=()=>{void Promise.all([desktop<Preferences>('CapturePreferences'),desktop<FeatureCapabilities>('FeatureCapabilities')]).then(([v,c])=>{if(live){setValue(v);setCapabilities(c);setError(!!v.notice)}}).catch(()=>{if(live)setError(true)})};refresh();window.addEventListener('focus',refresh);return()=>{live=false;window.removeEventListener('focus',refresh)}},[]);
 const save=async(method:'SetCaptureEnabled'|'SaveCapturePreferences',next:boolean)=>{
  setBusy(true);try{setValue(await (method==='SetCaptureEnabled'?desktop<Preferences>(method,next):desktop<Preferences>(method,{includeBackground:next,notice:''})));setCapabilities(await desktop<FeatureCapabilities>('FeatureCapabilities'));setError(false)}catch{setError(true)}finally{setBusy(false)}
 };
 return <><p className="settings-page-intro">{t('settings.extrasIntro')}</p><SettingGroup title={t('settings.extrasCaptureTitle')}>
  <SettingRow label={t('settings.extrasCaptureToggle')} description={t('settings.extrasCaptureDescription')} htmlFor="capture-enabled">
   <input id="capture-enabled" className="settings-switch" type="checkbox" role="switch" checked={capabilities?.capture.enabled??value?.enabled??false} disabled={!value||!capabilities?.capture.available||busy} onChange={e=>void save('SetCaptureEnabled',e.target.checked)}/>
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
