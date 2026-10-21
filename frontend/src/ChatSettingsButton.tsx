import {useState} from 'react';
import {GearSixIcon} from '@phosphor-icons/react/dist/csr/GearSix';
import {desktop} from './desktop';
import {useI18n} from './i18n';

// Presentation only: native and Web transports retain their navigation guards.
export function ChatSettingsButton(){
 const {t}=useI18n(),[opening,setOpening]=useState(false),[failed,setFailed]=useState(false);
 return <button className="chat-settings" aria-label={t('settings.windowTitle')} title={failed?t('settings.openSettingsFailed'):t('settings.windowTitle')} aria-busy={opening} onClick={()=>{if(opening)return;setOpening(true);setFailed(false);void desktop('OpenSettings').catch(()=>setFailed(true)).finally(()=>setOpening(false));}}><GearSixIcon size={20} aria-hidden="true"/></button>;
}
