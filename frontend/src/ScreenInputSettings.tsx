import {useEffect,useState} from 'react';
import {desktop} from './desktop';
import {SettingGroup,SettingRow} from './SettingsUI';
import {useI18n} from './i18n';
type Preferences={includeBackground:boolean;notice:string};
export function ScreenInputSettings() {
 const {t}=useI18n(),[value,setValue]=useState<Preferences|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(false);
 useEffect(()=>{let live=true;void desktop<Preferences>('CapturePreferences').then(v=>{if(live){setValue(v);setError(!!v.notice)}}).catch(()=>{if(live)setError(true)});return()=>{live=false}},[]);
 const save=async(includeBackground:boolean)=>{
  setBusy(true);try{setValue(await desktop<Preferences>('SaveCapturePreferences',{includeBackground,notice:''}));setError(false)}catch{setError(true)}finally{setBusy(false)}
 };
 return <SettingGroup title={t('settings.screenInput')}>
  <SettingRow label={t('settings.screenContext')} description={t('settings.screenContextDescription')} htmlFor="screen-context">
   <input id="screen-context" className="settings-switch" type="checkbox" role="switch" checked={value?.includeBackground??false} disabled={!value||busy} onChange={e=>void save(e.target.checked)}/>
  </SettingRow>
  {error&&<p role="alert" className="settings-note">{t('settings.screenPreferencesFailed')}</p>}
 </SettingGroup>;
}
