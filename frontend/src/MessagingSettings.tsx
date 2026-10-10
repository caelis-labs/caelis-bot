import {useEffect,useState} from 'react';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import type {MessageKey} from './i18n/catalogs';
import {telegramPhase,type TelegramStatus} from './telegram-state';
import type {WeixinStatus} from './WeixinSettings';

// A channel row is an entry point, not a second account or a configuration form.
export function MessagingSettings({openTelegram,openWeixin,active=true,focusVersion=0}:{openTelegram:()=>void;openWeixin:()=>void;active?:boolean;focusVersion?:number}){
 const {t}=useI18n();
 const [status,setStatus]=useState<TelegramStatus|null>(null),[error,setError]=useState(false);
 const [weixin,setWeixin]=useState<WeixinStatus|null>(null);
 useEffect(()=>{if(!active)return;let live=true;const read=()=>{void desktop<TelegramStatus>('TelegramStatus').then(value=>{if(live){setStatus(value);setError(false)}}).catch(()=>{if(live)setError(true)})};read();const timer=window.setInterval(read,2500);return()=>{live=false;clearInterval(timer)}},[active,focusVersion]);
 useEffect(()=>{if(!active)return;let live=true;const read=()=>{void desktop<WeixinStatus>('WeixinStatus').then(value=>{if(live)setWeixin(value)}).catch(()=>{})};read();const timer=window.setInterval(read,2500);return()=>{live=false;clearInterval(timer)}},[active,focusVersion]);
 const phase=telegramPhase(status,false,error?'unavailable':status?.issue??'');
 return <section className="messaging-settings"><h1>{t('settings.chatConnections')}</h1><p className="settings-page-intro">{t('settings.messagingIntro')}</p>
  <button type="button" className="messaging-channel" onClick={openTelegram}><span className="messaging-channel-copy"><strong>Telegram</strong><span>{t('settings.telegramSubtitle')}</span></span><span className={`telegram-state telegram-state-${phase}`}>{t(`settings.telegramState_${phase}` as MessageKey)}</span><span className="messaging-chevron" aria-hidden="true">›</span></button>
  <button type="button" className="messaging-channel" onClick={openWeixin}><span className="messaging-channel-copy"><strong>{t('settings.weixinTitle')}</strong><span>{t('settings.weixinSubtitle')}</span></span><span className={`telegram-state telegram-state-${weixin?.phase==='connected'?'connected':weixin?.phase==='paused'?'paused':'unconfigured'}`}>{t(weixin?.phase==='connected'?'settings.weixinState_connected':weixin?.phase==='paused'?'settings.weixinState_paused':'settings.weixinState_unconfigured')}</span><span className="messaging-chevron" aria-hidden="true">›</span></button>
 </section>;
}
