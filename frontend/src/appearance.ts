import { useEffect, useSyncExternalStore } from 'react';
import { desktop } from './desktop';

export type Selection={character:string;avatar:string};
export type Appearance={revision:number;selection:Selection;model:string;avatar:string;basic:boolean;key:string};
export type ContentState={appearance:Appearance;characters:Choice[];avatars:Choice[];packs:{id:string;name:string;author:string;version:string;license:string;active:boolean}[];notice:string};
type Choice={id:string;name:string;pack:string;group:string};
export const builtinAppearance:Appearance={revision:0,selection:{character:'builtin:caelis',avatar:'follow'},model:'',avatar:'',basic:false,key:'builtin:caelis'};
let current=builtinAppearance,initial:Promise<void>|undefined;
const listeners=new Set<()=>void>();
export function acceptAppearance(value:Appearance){if(value.revision<current.revision)return;current=value;listeners.forEach(notify=>notify());}
window.addEventListener('appearance-changed',event=>acceptAppearance((event as CustomEvent<Appearance>).detail));
const subscribe=(notify:()=>void)=>{listeners.add(notify);return()=>{listeners.delete(notify);};};
export function initializeAppearance(){return initial??=desktop<Appearance>('Appearance').then(acceptAppearance).catch(()=>{initial=undefined;});}
export function observeAppearance(notify:(value:Appearance)=>void){const changed=()=>notify(current);const remove=subscribe(changed);void initializeAppearance().then(changed);return remove;}
export function useAppearance(){const value=useSyncExternalStore(subscribe,()=>current);useEffect(()=>{void initializeAppearance();},[]);return value;}
