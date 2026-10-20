import { useEffect } from 'react';
import { History } from './Panel';
import { desktop } from './desktop';
import { useI18n } from './i18n';
import { GearSixIcon } from '@phosphor-icons/react/dist/csr/GearSix';
import {nativeContextChrome,settingsHasModal} from './context-navigation';
export function ContextWindow() {
 const {t}=useI18n();
 const open=()=>{if(!settingsHasModal())void desktop('OpenSettings')};
 useEffect(()=>{window.addEventListener('context-navigation',open);return()=>window.removeEventListener('context-navigation',open)},[]);
 return <div className="context-window" data-view="chat" data-native-chrome={nativeContextChrome()}><div className="context-view chat-view">{!nativeContextChrome()&&<button className="context-navigation" aria-label={t('native.settings')} onClick={open}><GearSixIcon aria-hidden="true" size={18}/></button>}<History/></div></div>;
}
