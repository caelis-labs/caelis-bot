import { useLayoutEffect, useMemo, useRef, useState, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { PaperclipIcon } from '@phosphor-icons/react/dist/csr/Paperclip';
import { PuzzlePieceIcon } from '@phosphor-icons/react/dist/csr/PuzzlePiece';
import { SparkleIcon } from '@phosphor-icons/react/dist/csr/Sparkle';
import type { Reference } from './backend/contract';
import { attachmentMenuDescription, attachmentMenuReferences, attachmentReferenceKey } from './attachment-menu-model';
import { attachmentMenuLayout, type MenuLayout } from './attachment-menu-layout';
import { useI18n } from './i18n';

export function AttachmentMenu({trigger,references,selected,onClose,onPick,onSelect}:{
 trigger:RefObject<HTMLButtonElement|null>;references:Reference[];selected:string[];
 onClose:()=>void;onPick:()=>void;onSelect:(id:string)=>void;
}) {
 const {t}=useI18n();
 const menu=useRef<HTMLElement>(null),[layout,setLayout]=useState<MenuLayout|null>(null);
 const close=useRef(onClose);close.current=onClose;
 const groups=useMemo(()=>attachmentMenuReferences(references),[references]);
 const selectedKeys=new Set(references.filter(r=>selected.includes(r.id)).map(attachmentReferenceKey));
 useLayoutEffect(()=>{
  const node=menu.current!;
  const position=()=>{
   const rect=trigger.current?.getBoundingClientRect();
   if(rect)setLayout(attachmentMenuLayout(rect,{width:innerWidth,height:innerHeight},Math.min(360,node.scrollHeight+2)));
  };
  const outside=(event:PointerEvent)=>{
   if(!node.contains(event.target as Node)&&!trigger.current?.contains(event.target as Node))close.current();
  };
  const key=(event:KeyboardEvent)=>{
   if(event.isComposing)return;
   if(event.key==='Escape'){
    event.preventDefault();event.stopImmediatePropagation();close.current();trigger.current?.focus();return;
   }
   if(event.key==='Tab'){close.current();return;}
   if(!['ArrowDown','ArrowUp','Home','End'].includes(event.key))return;
   const buttons=Array.from(node.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'));
   if(!buttons.length)return;
   event.preventDefault();event.stopImmediatePropagation();
   const current=buttons.indexOf(document.activeElement as HTMLButtonElement);
   const index=event.key==='Home'?0:event.key==='End'?buttons.length-1:event.key==='ArrowDown'?(current+1)%buttons.length:(current<=0?buttons.length:current)-1;
   buttons[index].focus();
  };
  const blur=()=>close.current();
  const taskExpanded=()=>close.current();
  window.addEventListener('resize',position);
  window.addEventListener('pointerdown',outside,true);
  window.addEventListener('keydown',key,true);
  window.addEventListener('blur',blur);
  window.addEventListener('task-dock-expanded',taskExpanded);
  const observer=new ResizeObserver(position);observer.observe(node);
  position();
  return()=>{
   observer.disconnect();
   window.removeEventListener('resize',position);
   window.removeEventListener('pointerdown',outside,true);
   window.removeEventListener('keydown',key,true);
   window.removeEventListener('blur',blur);
   window.removeEventListener('task-dock-expanded',taskExpanded);
  };
 },[trigger]);
 const row=(reference:Reference)=>{
  const plugin=reference.kind==='plugin';
  return <button role="menuitem" key={reference.id} className="attachment-menu-row" disabled={selectedKeys.has(attachmentReferenceKey(reference))}
   title={reference.description||reference.name} onClick={()=>onSelect(reference.id)}>
   {plugin?<PuzzlePieceIcon size={17} aria-hidden="true"/>:<SparkleIcon size={17} aria-hidden="true"/>}
   <span className="attachment-menu-copy"><strong>{reference.name}</strong>{reference.description&&<small>{attachmentMenuDescription(reference.description)}</small>}</span>
  </button>;
 };
 return createPortal(<section ref={menu} id="attachment-menu" className="attachment-menu" role="menu" aria-label={t('chat.attachmentMenuAriaLabel')}
  style={layout?{left:layout.left,top:layout.top,width:layout.width,maxHeight:layout.height}:{visibility:'hidden'}}>
  <button role="menuitem" className="attachment-menu-row attachment-file" onClick={onPick}><PaperclipIcon size={17} aria-hidden="true"/><span className="attachment-menu-copy"><strong>{t('chat.addFile')}</strong></span></button>
  {!!groups.skills.length&&<div className="attachment-menu-group"><p className="menu-caption">{t('chat.skills')}</p>{groups.skills.map(row)}</div>}
  {!!groups.plugins.length&&<div className="attachment-menu-group"><p className="menu-caption">{t('chat.plugins')}</p>{groups.plugins.map(row)}</div>}
 </section>,document.body);
}
