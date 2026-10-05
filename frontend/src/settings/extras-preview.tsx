import '../style.css';
import './settings.css';
import {createRoot} from 'react-dom/client';
import {I18nProvider, type LanguageBridge} from '../i18n';
import {Settings} from '../Settings';

if(!import.meta.env.DEV)throw new Error('Development fixture only');
document.body.dataset.surface='settings';
const query=new URLSearchParams(location.search);
const locale=query.get('lang')==='zh-CN'?'zh-CN':'en';
const bridge:LanguageBridge={read:async()=>({preference:locale,locale,revision:1}),save:async()=>({preference:locale,locale,revision:2}),subscribe:()=>()=>{}};
createRoot(document.getElementById('root')!).render(<I18nProvider bridge={bridge}><Settings/></I18nProvider>);
