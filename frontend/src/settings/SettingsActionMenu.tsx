import type {ReactNode} from 'react';
import {SettingsDialog} from './runtime/SettingsDialog';
export function SettingsActionMenu({anchor,title,items,onClose}:{anchor:HTMLElement;title:string;items:Array<{label:ReactNode;action:()=>void;disabled?:boolean}>;onClose:()=>void}){
 return <SettingsDialog title={title} anchor={anchor} alignTo={anchor} className="settings-action-menu" onClose={onClose}>
  <div role="menu" aria-label={title} onKeyDown={event=>{
   const nodes=[...event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')],index=nodes.indexOf(document.activeElement as HTMLButtonElement);
   if(event.key==='ArrowDown'||event.key==='ArrowUp'){event.preventDefault();nodes[(index+(event.key==='ArrowDown'?1:nodes.length-1))%nodes.length]?.focus()}
  }}>{items.map((item,index)=><button key={index} type="button" role="menuitem" disabled={item.disabled} onClick={()=>{onClose();item.action()}}>{item.label}</button>)}</div>
 </SettingsDialog>
}
