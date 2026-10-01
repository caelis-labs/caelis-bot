import { useLayoutEffect, useId, useRef, type ReactNode } from 'react';
import { useI18n } from '../../i18n';

const modals:{element:HTMLElement;refresh:()=>void}[]=[];
const backgrounds=new WeakMap<HTMLElement,{owners:number;inert:string|null;hidden:string|null}>();
function blockBackground(element:HTMLElement){
 const existing=backgrounds.get(element);
 if(existing){existing.owners++;return;}
 backgrounds.set(element,{owners:1,inert:element.getAttribute('inert'),hidden:element.getAttribute('aria-hidden')});
 element.setAttribute('inert','');element.setAttribute('aria-hidden','true');
}
function restoreBackground(element:HTMLElement){
 const original=backgrounds.get(element);
 if(!original||--original.owners)return;
 for(const [name,value] of [['inert',original.inert],['aria-hidden',original.hidden]] as const){
  if(value===null)element.removeAttribute(name);else element.setAttribute(name,value);
 }
 backgrounds.delete(element);
}
const activeModal=()=>modals.reduce<(typeof modals)[number]|undefined>((top,modal)=>top&&modal.element.contains(top.element)?top:modal,undefined);
const topModal=()=>activeModal()?.element;
function canRestoreFocus(element:HTMLElement|null|undefined){
 if(!element?.isConnected||element.matches(':disabled,[aria-disabled="true"]')||element.closest('[inert],[hidden],[aria-hidden="true"]'))return false;
 if(!element.matches('button,input,select,textarea,summary,a[href],[tabindex],[contenteditable="true"]'))return false;
 for(let parent:HTMLElement|null=element;parent;parent=parent.parentElement){
  const style=window.getComputedStyle(parent);
  if(style.display==='none'||style.visibility==='hidden'||style.visibility==='collapse')return false;
  if(parent.tagName==='DETAILS'&&!parent.hasAttribute('open')&&!parent.querySelector(':scope > summary')?.contains(element))return false;
 }
 return true;
}

export function SettingsDialog({ title, description, busy = false, onClose, children, className = '',returnFocus,fallbackFocus }: { returnFocus?:HTMLElement|null; fallbackFocus?:HTMLElement|null; title: string; description?: string; busy?: boolean; onClose: () => void; children: ReactNode; className?: string }) {
 const { t } = useI18n();
 const titleID = useId(), ref = useRef<HTMLElement>(null),current=useRef({busy,onClose});current.current={busy,onClose};
 useLayoutEffect(() => {
  const dialog=ref.current!,scrim=dialog.parentElement!;
  const prior = document.activeElement as HTMLElement | null;
  const blocked=new Set<HTMLElement>();
  const refresh=()=>{
   if(topModal()!==dialog)return;
   // Only siblings of the modal's ancestor path are inert. The modal and its
   // ancestors stay reachable, including when it is rendered inside a page.
   for(let branch:HTMLElement|null=scrim;branch?.parentElement;branch=branch.parentElement){
    for(const sibling of branch.parentElement.children){
     if(sibling===branch||!(sibling instanceof HTMLElement)||blocked.has(sibling))continue;
     blockBackground(sibling);blocked.add(sibling);
    }
    if(branch.parentElement===document.body)break;
   }
  };
  const modal={element:dialog,refresh};modals.push(modal);
  dialog.focus({preventScroll:true});refresh();
  const observer=new window.MutationObserver(refresh);observer.observe(document.body,{childList:true,subtree:true});
  const controls=()=>{const nodes=[...dialog.querySelectorAll<HTMLElement>('button,input,select,textarea,summary,a[href],[tabindex],[contenteditable="true"]')].filter(node=>{
   if(node.tabIndex<0||node.matches(':disabled')||node.closest('[hidden],[inert]')||!node.getClientRects().length||window.getComputedStyle(node).visibility==='hidden')return false;
   for(let parent=node.parentElement;parent&&parent!==dialog;parent=parent.parentElement){
    if(parent.tagName==='DETAILS'&&!parent.hasAttribute('open')&&!parent.querySelector(':scope > summary')?.contains(node))return false;
   }
   return true;
  });
  // Native radio groups are one Tab stop; arrow keys still select options.
  return nodes.filter(node=>{
   if(!node.matches('input[type="radio"][name]')||!node.getAttribute('name'))return true;
   const radio=node as HTMLInputElement,group=nodes.filter(candidate=>candidate.matches('input[type="radio"]')&&(candidate as HTMLInputElement).name===radio.name&&(candidate as HTMLInputElement).form===radio.form);
   return node===(group.find(candidate=>(candidate as HTMLInputElement).checked)??group[0]);
  });};
  const key=(event:KeyboardEvent)=>{
   if(topModal()!==dialog||event.isComposing||!['Tab','Escape'].includes(event.key))return;
   event.preventDefault();event.stopImmediatePropagation();
   if(event.key==='Escape'){if(!current.current.busy&&!event.repeat)current.current.onClose();return;}
   const nodes=controls(),index=nodes.indexOf(document.activeElement as HTMLElement);
   const next=event.shiftKey?(index<=0?nodes.length-1:index-1):(index<0||index===nodes.length-1?0:index+1);
   (nodes[next]??dialog).focus({preventScroll:true});
  };
  const focus=(event:FocusEvent)=>{if(topModal()===dialog&&!dialog.contains(event.target as Node))dialog.focus({preventScroll:true});};
  const guard = (event: Event) => event.preventDefault();
  window.addEventListener('keydown',key,true);document.addEventListener('focusin',focus,true);
  window.addEventListener('settings-navigate', guard);
  return () => {
   const wasTop=topModal()===dialog;
   observer.disconnect();window.removeEventListener('keydown',key,true);document.removeEventListener('focusin',focus,true);window.removeEventListener('settings-navigate',guard);
   modals.splice(modals.indexOf(modal),1);blocked.forEach(restoreBackground);activeModal()?.refresh();
   if(wasTop){const target=[returnFocus,fallbackFocus,prior,topModal()].find(canRestoreFocus);target?.focus({preventScroll:true});}
  };
 }, []);
 return <div className="setup-scrim runtime-dialog-scrim" onPointerDown={event => { if (event.target === event.currentTarget && !busy) onClose(); }}>
  <section ref={ref} tabIndex={-1} className={`setup-dialog runtime-dialog ${className}`} role="dialog" aria-modal="true" aria-labelledby={titleID}>
   <div className="runtime-dialog-heading"><h2 id={titleID}>{title}</h2><button className="runtime-close" aria-label={t('runtime.closeDialog')} disabled={busy} onClick={onClose}><img src="/icons/xmark.png" alt=""/></button></div>
   {description && <p className="runtime-dialog-description">{description}</p>}
   {children}
  </section>
 </div>;
}
