import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { PaperclipIcon } from '@phosphor-icons/react/dist/csr/Paperclip';
import { PuzzlePieceIcon } from '@phosphor-icons/react/dist/csr/PuzzlePiece';
import { desktop, backend } from './desktop';
import { attachmentMenuDescription, botMenuPlugins, nativeMenuPlugins, type MenuPlugin } from './attachment-menu-model';
import { attachmentMenuLayout, type MenuLayout } from './attachment-menu-layout';
import { useI18n } from './i18n';

type BotCatalog={items:Array<{id:string;title:string;description:string;installed:boolean;enabled:boolean;status:string}>;syncState?:string};
type NativePlugin={id:string;name:string;description:string;source:string};

export function AttachmentMenu({trigger,selected,onClose,onPick,onSelect}: {
 trigger:RefObject<HTMLButtonElement|null>;selected:string[];
 onClose:()=>void;onPick:()=>void;onSelect:(plugin:MenuPlugin)=>void;
}) {
 const {t}=useI18n();
 const menu=useRef<HTMLElement>(null),[layout,setLayout]=useState<MenuLayout|null>(null);
 const [bot,setBot]=useState<MenuPlugin[]>([]),[native,setNative]=useState<MenuPlugin[]>([]);
 const close=useRef(onClose);close.current=onClose;
 useEffect(()=>{
  let alive=true;
  void desktop<BotCatalog>('Plugins').then(value=>{if(alive&&value.syncState!=='pending')setBot(botMenuPlugins(value.items));}).catch(()=>{});
  void backend<NativePlugin[]>('NativePlugins').then(value=>{if(alive)setNative(nativeMenuPlugins(value));}).catch(()=>{});
  return()=>{alive=false;};
 },[]);
 useLayoutEffect(()=>{
  const node=menu.current!;
  const position=()=>{
   const rect=trigger.current?.closest('.capsule')?.getBoundingClientRect();
   if(rect)setLayout(attachmentMenuLayout(rect,{width:innerWidth,height:innerHeight},Math.min(360,node.scrollHeight+2),rect.width));
  };
  const outside=(event:PointerEvent)=>{if(!node.contains(event.target as Node)&&!trigger.current?.contains(event.target as Node))close.current();};
  const key=(event:KeyboardEvent)=>{
   if(event.isComposing)return;
   if(event.key==='Escape'){event.preventDefault();event.stopImmediatePropagation();close.current();trigger.current?.focus();return;}
   if(event.key==='Tab'){close.current();return;}
   if(!['ArrowDown','ArrowUp','Home','End'].includes(event.key))return;
   const buttons=Array.from(node.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'));
   if(!buttons.length)return;
   event.preventDefault();event.stopImmediatePropagation();
   const current=buttons.indexOf(document.activeElement as HTMLButtonElement);
   const index=event.key==='Home'?0:event.key==='End'?buttons.length-1:event.key==='ArrowDown'?(current+1)%buttons.length:(current<=0?buttons.length:current)-1;
   buttons[index].focus();
  };
  const blur=()=>close.current(),taskExpanded=()=>close.current();
  window.addEventListener('resize',position);
  window.addEventListener('pointerdown',outside,true);
  window.addEventListener('keydown',key,true);
  window.addEventListener('blur',blur);
  window.addEventListener('task-dock-expanded',taskExpanded);
  const observer=new ResizeObserver(position);observer.observe(node);position();
  return()=>{
   observer.disconnect();window.removeEventListener('resize',position);
   window.removeEventListener('pointerdown',outside,true);window.removeEventListener('keydown',key,true);
   window.removeEventListener('blur',blur);window.removeEventListener('task-dock-expanded',taskExpanded);
  };
 },[trigger,bot.length,native.length]);
 const row=(plugin:MenuPlugin)=><button role="menuitem" key={plugin.id} className="attachment-menu-row" disabled={selected.includes(plugin.id)}
  title={plugin.description||plugin.name} onClick={()=>onSelect(plugin)}>
  <PuzzlePieceIcon size={17} aria-hidden="true"/>
  <span className="attachment-menu-copy"><strong>{plugin.name}</strong>{plugin.description&&<small>{attachmentMenuDescription(plugin.description)}</small>}</span>
 </button>;
 return createPortal(<section ref={menu} id="attachment-menu" className="attachment-menu" role="menu" aria-label={t('chat.attachmentMenuAriaLabel')}
  style={layout?{left:layout.left,top:layout.top,width:layout.width,maxHeight:layout.height}:{visibility:'hidden'}}>
  <button role="menuitem" className="attachment-menu-row attachment-file" onClick={onPick}><PaperclipIcon size={17} aria-hidden="true"/><span className="attachment-menu-copy"><strong>{t('chat.addFile')}</strong></span></button>
  {!!bot.length&&<div className="attachment-menu-group"><p className="menu-caption">{t('chat.plugins')} · Bot</p>{bot.map(row)}</div>}
  {!!native.length&&<div className="attachment-menu-group"><p className="menu-caption">{t('chat.plugins')} · Codex</p>{native.map(row)}</div>}
 </section>,document.body);
}
