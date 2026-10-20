import {settingsHasModal} from '../../context-navigation';
import {SetupBack} from '../../SetupBack';
import {useEffect,useRef,type ReactNode} from 'react';
import {useI18n} from '../../i18n';
export function SettingsPage({title,description,busy=false,onClose,children,className='',hideHeader=false}:{hideHeader?:boolean;className?:string;title:ReactNode;description?:string;busy?:boolean;onClose:()=>void;children:ReactNode}){
 const {t}=useI18n(), element=useRef<HTMLElement>(null);
 useEffect(()=>{
  const key=(event:KeyboardEvent)=>{
   if(event.key!=='Escape'||event.defaultPrevented||!element.current?.getClientRects().length||settingsHasModal())return;
   event.preventDefault();event.stopPropagation();if(!busy)onClose();
  };
  window.addEventListener('keydown',key,true);return()=>window.removeEventListener('keydown',key,true);
 },[busy,onClose]);
 useEffect(()=>{if(!busy)return;const guard=(e:Event)=>e.preventDefault();window.addEventListener('settings-navigate',guard);return()=>window.removeEventListener('settings-navigate',guard)},[busy]);
 return <section ref={element} className={`connection-detail ${className}`}>{!hideHeader&&<header className="connection-detail-header">{className.includes('onboarding-connection')?<SetupBack disabled={busy} onClick={onClose}/>:<button className="text-action" disabled={busy} onClick={onClose}>{t('settings.setupBack')}</button>}<h2>{title}</h2></header>}{description&&<p className="settings-note">{description}</p>}<div className="connection-detail-body">{children}</div></section>
}
