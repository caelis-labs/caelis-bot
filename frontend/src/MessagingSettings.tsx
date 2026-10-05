import {useEffect,useState} from 'react';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import type {MessageKey} from './i18n/catalogs';
import {telegramPhase,type TelegramStatus} from './telegram-state';

// A channel row is an entry point, not a second account or a configuration form.
export function MessagingSettings({openTelegram}:{openTelegram:()=>void}){
 const {t}=useI18n();
 const [status,setStatus]=useState<TelegramStatus|null>(null),[error,setError]=useState(false);
 useEffect(()=>{let live=true;const read=()=>{void desktop<TelegramStatus>('TelegramStatus').then(value=>{if(live){setStatus(value);setError(false)}}).catch(()=>{if(live)setError(true)})};read();const timer=window.setInterval(read,2500);return()=>{live=false;clearInterval(timer)}},[]);
 const phase=telegramPhase(status,false,error?'unavailable':status?.issue??'');
 return <section className="messaging-settings"><h1>{t('settings.chatConnections')}</h1><p className="settings-page-intro">{t('settings.messagingIntro')}</p>
  <button type="button" className="messaging-channel" onClick={openTelegram}><span className="messaging-channel-copy"><strong>Telegram</strong><span>{t('settings.telegramSubtitle')}</span></span><span className={`telegram-state telegram-state-${phase}`}>{t(`settings.telegramState_${phase}` as MessageKey)}</span><span className="messaging-chevron" aria-hidden="true">›</span></button>
 </section>;
}
