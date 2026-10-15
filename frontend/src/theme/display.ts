import { useSyncExternalStore } from 'react';
import { desktop } from '../desktop';
export type DisplayState={mode:'system'|'light'|'dark';theme:'default';revision:number};
let current:DisplayState={mode:'system',theme:'default',revision:0};
const listeners=new Set<()=>void>();
const media=matchMedia('(prefers-color-scheme: dark)');
function apply(){const mode=current.mode==='system'?(media.matches?'dark':'light'):current.mode;document.documentElement.dataset.mode=mode;document.documentElement.style.colorScheme=mode;}
export function acceptDisplay(v:DisplayState){if(v.revision<current.revision)return;current=v;apply();listeners.forEach(fn=>fn());}
window.addEventListener('display-changed',event=>acceptDisplay((event as CustomEvent<DisplayState>).detail));
media.addEventListener('change',apply);
apply();
void desktop<DisplayState>('DisplayPreferences').then(acceptDisplay).catch(()=>{});
export function useDisplay(){return useSyncExternalStore(fn=>{listeners.add(fn);return()=>{listeners.delete(fn);};},()=>current);}
