import '../application.css';
import {createRoot} from 'react-dom/client';
import {I18nProvider,type LanguageBridge} from '../i18n';
import {Settings} from '../Settings';
import {ContextWindow} from '../ContextWindow';
import {SurfaceBoundary} from '../SurfaceBoundary';
import {useEffect,useState} from 'react';
if(!import.meta.env.DEV)throw Error('Development fixture only');
const params=new URLSearchParams(location.search),chat=params.get('surface')==='chat',locale=params.get('lang')==='en'?'en':'zh-CN';
document.body.dataset.surface=chat?'context':'settings';
const bridge:LanguageBridge={read:async()=>({preference:locale,locale,revision:1}),save:async()=>({preference:locale,locale,revision:2}),subscribe:()=>()=>{}};
let failRendering=false;
function FixtureSurface(){
 const [failed,setFailed]=useState(failRendering);
 useEffect(()=>{const fail=()=>{failRendering=true;setFailed(true)};window.addEventListener('fixture-render-failure',fail);return()=>window.removeEventListener('fixture-render-failure',fail)},[]);
 if(failed)throw Error('Synthetic renderer failure');
 return chat?<ContextWindow/>:<Settings/>;
}
createRoot(document.getElementById('root')!).render(<I18nProvider bridge={bridge}><SurfaceBoundary><FixtureSurface/></SurfaceBoundary></I18nProvider>);
