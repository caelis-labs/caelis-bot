import { useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import type { Reference } from './backend/contract';
import { desktop } from './desktop';
import { attachmentMenuLayout, type MenuLayout } from './attachment-menu-layout';
import { useI18n } from './i18n';

// Preserve open/close ordering across quick-menu mounts. Native activation IDs
// also fence requests from an editor that has already been hidden/reopened.
let nativeLayouts:Promise<unknown>=Promise.resolve();
function setNativeMenu(height:number,activation:number) {
 nativeLayouts=nativeLayouts.catch(()=>{}).then(()=>desktop('SetPanelMenu',height,activation));
 return nativeLayouts;
}

export function AttachmentMenu({trigger,composer,quick,activation,references,selected,onClose,onPick,onSelect,onError}:{
 trigger:RefObject<HTMLButtonElement|null>;composer:RefObject<HTMLDivElement|null>;
 quick:boolean;activation:number;references:Reference[];selected:string[];
 onClose:()=>void;onPick:()=>void;onSelect:(id:string)=>void;onError:()=>void;
}) {
 const {t} = useI18n();
 const menu=useRef<HTMLElement>(null),[layout,setLayout]=useState<MenuLayout|null>(null);
 const callbacks=useRef({onClose,onError});callbacks.current={onClose,onError};
 useLayoutEffect(()=>{
  const node=menu.current!;
  const desired=Math.min(324,node.scrollHeight+2);
  const position=()=>{
   if(quick)return;
   const rect=composer.current?.getBoundingClientRect();
   if(rect)setLayout(attachmentMenuLayout(rect,{width:innerWidth,height:innerHeight},desired));
  };
  const nativeLayout=(e:Event)=>{
   const detail=(e as CustomEvent<MenuLayout&{activation:number}>).detail;
   if(detail.activation===activation)setLayout(detail);
  };
  const outside=(e:PointerEvent)=>{
   if(!node.contains(e.target as Node)&&!trigger.current?.contains(e.target as Node))callbacks.current.onClose();
  };
  const key=(e:KeyboardEvent)=>{
   if(e.isComposing)return;
   if(e.key==='Escape'){
    e.preventDefault();e.stopImmediatePropagation();callbacks.current.onClose();trigger.current?.focus();return;
   }
   if(e.key==='Tab'){callbacks.current.onClose();return;}
   if(!['ArrowDown','ArrowUp','Home','End'].includes(e.key))return;
   const buttons=Array.from(node.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'));
   if(!buttons.length)return;
   e.preventDefault();e.stopImmediatePropagation();
   const current=buttons.indexOf(document.activeElement as HTMLButtonElement);
   const index=e.key==='Home'?0:e.key==='End'?buttons.length-1:e.key==='ArrowDown'?(current+1)%buttons.length:(current<=0?buttons.length:current)-1;
   buttons[index].focus();
  };
  const blur=()=>callbacks.current.onClose();
  window.addEventListener('panel-menu-layout',nativeLayout);
  window.addEventListener('resize',position);
  window.addEventListener('pointerdown',outside,true);
  window.addEventListener('keydown',key,true);
  window.addEventListener('blur',blur);
  const observer=new ResizeObserver(position);if(composer.current)observer.observe(composer.current);
  position();
  let disposed=false;
  if(quick)void setNativeMenu(desired,activation).catch(()=>{if(!disposed){callbacks.current.onClose();callbacks.current.onError();}});
  return()=>{
   disposed=true;observer.disconnect();
   window.removeEventListener('panel-menu-layout',nativeLayout);
   window.removeEventListener('resize',position);
   window.removeEventListener('pointerdown',outside,true);
   window.removeEventListener('keydown',key,true);
   window.removeEventListener('blur',blur);
   if(quick)void setNativeMenu(0,activation).catch(()=>{});
  };
 },[quick,activation,references.length,trigger,composer]);
 return createPortal(<section ref={menu} id="attachment-menu" className="attachment-menu" role="menu" aria-label={t('chat.attachmentMenuAriaLabel')}
  style={layout?{left:layout.left,top:layout.top,width:layout.width,maxHeight:layout.height}:{visibility:'hidden'}}>
  <button role="menuitem" className="action-row attachment-file" onClick={onPick}><img className="symbol" src="/icons/paperclip.png" alt=""/>{t('chat.addFile')}</button>
  {!!references.length&&<><p className="menu-caption">{t('chat.pluginsAndSkills')}</p>{references.map(r=><button role="menuitem" key={r.id} className="action-row" disabled={selected.includes(r.id)} title={r.description} onClick={()=>onSelect(r.id)}><span>{r.name}<small>{r.description}</small></span></button>)}</>}
 </section>,document.body);
}
