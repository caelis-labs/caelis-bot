import '../style.css';
import './settings.css';
import {createRoot} from 'react-dom/client';
import {useEffect} from 'react';
import {I18nProvider, type LanguageBridge} from '../i18n';
import {TelegramSettings} from '../TelegramSettings';
import {BotSetup} from '../BotSetup';
import {ScreenInputSettings} from '../ScreenInputSettings';
import {useI18n} from '../i18n';
import {settingsSections} from '../settings-navigation';

if(!import.meta.env.DEV)throw new Error('Development fixture only');
document.body.dataset.surface='settings';
const query=new URLSearchParams(location.search);
const locale=query.get('lang')==='zh-CN'?'zh-CN':'en';
const bridge:LanguageBridge={read:async()=>({preference:locale,locale,revision:1}),save:async()=>({preference:locale,locale,revision:2}),subscribe:()=>()=>{}};
function Preview(){const {t}=useI18n();const page=query.get('page')??'telegram';
 useEffect(()=>{const state=query.get('state');if(page!=='telegram')return;
  const timer=setTimeout(()=>{
   if(state==='existing')document.querySelector<HTMLButtonElement>('.telegram-route button:nth-child(2)')?.click();
   if(state==='forget')document.querySelector<HTMLButtonElement>('.telegram-manage .text-action')?.click();
   if(state==='connecting'){
    const input=document.querySelector<HTMLInputElement>('#telegram-token');if(!input)return;
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')?.set?.call(input,'fixture-token');input.dispatchEvent(new Event('input',{bubbles:true}));
    setTimeout(()=>document.querySelector<HTMLButtonElement>('.telegram-step button.primary')?.click(),80);
   }
  },250);return()=>clearTimeout(timer);
 },[page]);
 if(page==='setup')return <main className="settings-window setup-window"><div className="setup-page"><BotSetup onDone={()=>{}}/></div></main>;
 return <main className="settings-window"><aside><nav aria-label={t('settings.navLabel')}>{settingsSections.map(id=><button key={id} aria-current={id===(page==='extras'?'extras':'chatConnections')?'page':undefined}>{t(`settings.${id}`)}</button>)}</nav><small>Caelis Bot<br/>Fixture</small></aside><div className="settings-content"><div className="settings-page"><h1>{t(page==='extras'?'settings.extras':'settings.chatConnections')}</h1>{page==='extras'?<ScreenInputSettings/>:<TelegramSettings/>}</div></div></main>;
}
createRoot(document.getElementById('root')!).render(<I18nProvider bridge={bridge}><Preview/></I18nProvider>);
