import {createContext,useContext,useLayoutEffect,useRef,type Dispatch,type SetStateAction,type ReactNode} from 'react';
import {createPortal} from 'react-dom';
import {settingsHasModal} from '../context-navigation';
export type SettingsCrumb={label:string;open?:()=>void};
// Subpages project their existing route into the one shell header. No second router.
export const SettingsToolbarContext=createContext<Dispatch<SetStateAction<SettingsCrumb[]>>|null>(null);
export function useSettingsTrail(items:SettingsCrumb[],active=true){
 const publish=useContext(SettingsToolbarContext),latest=useRef(items);latest.current=items;
 const key=JSON.stringify(items.map(item=>[item.label,!!item.open]));
 useLayoutEffect(()=>{
  if(!publish||!active)return;
  publish(latest.current.map((item,index)=>({label:item.label,open:item.open?()=>latest.current[index]?.open?.():undefined})));
  return()=>publish([]);
 },[publish,active,key]);
 return !!publish;
}
export function SettingsBreadcrumbs({items,label}:{items:SettingsCrumb[];label:string}){
 return <nav className="settings-breadcrumbs" aria-label={label}><ol>{items.map((item,index)=><li key={index}>{index>0&&<span aria-hidden="true">/</span>}{index<items.length-1&&item.open?<button onClick={()=>{if(!settingsHasModal()&&window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))item.open?.();}}>{item.label}</button>:<span aria-current={index===items.length-1?'page':undefined}>{item.label}</span>}</li>)}</ol></nav>;
}

export const SettingsActionsContext=createContext<HTMLElement|null>(null);
export function SettingsPageActions({children}:{children:ReactNode}){const host=useContext(SettingsActionsContext);return host?createPortal(children,host):<div className="settings-page-actions">{children}</div>}
