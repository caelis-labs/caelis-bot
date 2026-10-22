import { Children, isValidElement, useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useI18n } from './i18n';
import { SettingsChevron } from './SettingsIcons';

type Option = {'data-badge'?:string;'data-description'?:string;title?:string;value:string;children:ReactNode;disabled?:boolean;hidden?:boolean};
// Keep the option markup at call sites; render every settings menu with the
// same accessible control, including menus inside dialogs.
export function SettingsSelect({id,value,disabled,onChange,children,placeholder,searchable=false,centered=true,compact=false,'aria-label':ariaLabel}:{placeholder?:ReactNode;compact?:boolean;centered?:boolean;searchable?:boolean;id?:string;value:string;disabled?:boolean;onChange:(event:{target:{value:string}})=>void;children:ReactNode;'aria-label'?:string}) {
 const {t}=useI18n();
 const [query,setQuery]=useState('');
 const search=useRef<HTMLInputElement>(null);
 const nativePopover=typeof HTMLElement.prototype.showPopover==='function';
 const menuID=useId(), trigger=useRef<HTMLButtonElement>(null), menu=useRef<HTMLDivElement>(null);
 const [open,setOpen]=useState(false),[position,setPosition]=useState({left:0,top:0,width:0,maxHeight:300});
 const options:Option[]=[];
 const visit=(nodes:ReactNode)=>Children.forEach(nodes,node=>{if(isValidElement<Option>(node)){if(node.type==='option')options.push(node.props);else if(node.props.children)visit(node.props.children);}});
 visit(children);
 // Empty disabled options are legacy placeholders, not menu choices. Keep
 // enabled empty values (Default / Follow conversation) and disabled real items.
 const choices=options.filter(o=>!o.hidden&&!(o.value===''&&o.disabled));
 const selected=options.find(o=>o.value===value);
 // Both surfaces size the same content columns before the menu opens. Hidden
 // copies also keep the leading edge stable when selection or search changes.
 const columnSizes=centered&&choices.map(option=><span key={option.value} className="settings-select-measure" aria-hidden="true"><span className="settings-select-option-copy">{option.children}{option['data-description']&&<small className="settings-select-detail">{option['data-description']}</small>}</span><span className="settings-select-badge">{option['data-badge']&&<small className="settings-source-badge">{option['data-badge']}</small>}</span></span>);
 const close=()=>{menu.current?.hidePopover?.();setOpen(false);trigger.current?.focus();};
 const focusSelected=()=>{if(searchable&&search.current){search.current.focus();return;}(menu.current?.querySelector('[aria-checked="true"]:not(:disabled)') as HTMLElement|null??menu.current?.querySelector<HTMLButtonElement>('button:not(:disabled)'))?.focus();};
 useEffect(()=>{
  if(!open)return;
  focusSelected();
  const dismiss=(event:PointerEvent)=>{if(!menu.current?.contains(event.target as Node)&&!trigger.current?.contains(event.target as Node)){menu.current?.hidePopover?.();setOpen(false);}};
  const reposition=()=>{menu.current?.hidePopover?.();setOpen(false);};
  document.addEventListener('pointerdown',dismiss);
  window.addEventListener('resize',reposition);
  return()=>{document.removeEventListener('pointerdown',dismiss);window.removeEventListener('resize',reposition);};
 },[open]);
 const show=()=>{
  if(open){close();return;}
  setQuery('');
  const rect=trigger.current!.getBoundingClientRect(), below=innerHeight-rect.bottom-12, above=rect.top-12;
  const height=Math.min(300,choices.length*40+12+(searchable?58:0)),up=below<height&&above>below;
  const width=compact?Math.min(360,innerWidth-16):Math.min(innerWidth-16,Math.max(rect.width,180));
  setPosition({left:Math.max(8,Math.min(rect.left,innerWidth-width-8)),top:up?Math.max(8,rect.top-height-6):rect.bottom+6,width,maxHeight:Math.max(80,up?above:below)});
  menu.current?.showPopover?.();setOpen(true);
 };
 const popup=<div id={menuID} ref={menu} popover={nativePopover?"auto":undefined} hidden={!nativePopover&&!open} role="menu" aria-label={ariaLabel} aria-labelledby={ariaLabel?undefined:id} className={`settings-select-menu${centered?' settings-select-menu-centered':''}${compact?' settings-select-menu-compact':''}`} style={compact?{...position,width:'max-content',maxWidth:position.width}:position} onToggle={()=>{if(nativePopover)setOpen(menu.current?.matches(':popover-open')??false);}} onKeyDown={event=>{
   if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close();return;}
   if(event.key==='Tab'){close();return;}
   if(event.target===search.current&&!['ArrowDown','ArrowUp','Escape','Tab'].includes(event.key))return;
   const items=[...menu.current!.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')];
   const index=items.indexOf(document.activeElement as HTMLButtonElement);
   let next=-1;
   if(event.key==='ArrowDown')next=(index+1)%items.length;
   else if(event.key==='ArrowUp')next=(index-1+items.length)%items.length;
   else if(event.key==='Home')next=0;
   else if(event.key==='End')next=items.length-1;
   else if(event.key.length===1&&!event.ctrlKey&&!event.metaKey)next=items.findIndex((item,i)=>i>index&&item.textContent?.toLocaleLowerCase().startsWith(event.key.toLocaleLowerCase()));
   if(next>=0){event.preventDefault();items[next]?.focus();}
  }}>{searchable&&<input ref={search} className="settings-select-search" aria-label={t('runtime.searchModels')} placeholder={t('runtime.searchModels')} value={query} onChange={e=>setQuery(e.target.value)}/>}<div className="settings-select-options">{columnSizes}{choices.filter(o=>!query||(String(o.children)+' '+(o.title||'')+' '+(o['data-description']||'')+' '+(o['data-badge']||'')).toLocaleLowerCase().includes(query.toLocaleLowerCase())).map(option=><button type="button" key={option.value} role="menuitemradio" aria-label={typeof option.children==='string'?[option.children,option['data-badge']].filter(Boolean).join(' · '):undefined} aria-checked={option.value===value} disabled={option.disabled} tabIndex={-1} onClick={()=>{onChange({target:{value:option.value}});close();}}><span className="settings-select-option-copy">{option.children}{option['data-description']&&<small className="settings-select-detail">{option['data-description']}</small>}</span><span className="settings-select-badge">{option['data-badge']&&<small className="settings-source-badge">{option['data-badge']}</small>}</span><span className="settings-select-check" aria-hidden="true">{option.value===value?'✓':''}</span></button>)}</div></div>;
 return <div className={`settings-select${centered?' settings-select-centered':''}`} data-select-open={open||undefined}>
  <button id={id} ref={trigger} className="settings-select-trigger" type="button" disabled={disabled} aria-label={ariaLabel} title={selected?.title} aria-haspopup="menu" aria-expanded={open} aria-controls={menuID} onClick={show} onKeyDown={event=>{if(event.key==='ArrowDown'||event.key==='ArrowUp'){event.preventDefault();show();}}}><span className="settings-select-value">{columnSizes}<span className="settings-select-option-copy">{selected?.children??(value||placeholder)}</span>{selected?.['data-badge']&&<small className="settings-source-badge">{selected['data-badge']}</small>}</span><SettingsChevron open={open}/></button>
  {/* Older WebKit has no top layer. Escape scrolling/stacking ancestors while
      staying inside the active dialog layer and its existing inert boundary. */}
  {nativePopover?popup:open&&createPortal(popup,trigger.current?.closest('.runtime-dialog-scrim')??trigger.current?.closest('.settings-window')??document.body)}
 </div>;
}
