import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { desktop } from '../desktop';
import { initialLanguage, newerLanguage, translator } from './core.ts';
import type { LanguagePreference, LanguageState } from './core.ts';

export type LanguageBridge = {
 read:()=>Promise<LanguageState>;
 save:(preference:LanguagePreference)=>Promise<LanguageState>;
 subscribe:(receive:(state:LanguageState)=>void)=>()=>void;
};
const nativeBridge:LanguageBridge={
 read:()=>desktop<LanguageState>('LanguagePreferences'),
 save:preference=>desktop<LanguageState>('SaveLanguage',preference),
 subscribe:receive=>{
  const listener=(event:Event)=>receive((event as CustomEvent<LanguageState>).detail);
  window.addEventListener('language-changed',listener);
  return()=>window.removeEventListener('language-changed',listener);
 },
};
const defaultContext={...initialLanguage,...translator('en'),ready:false,failed:false,
 setLanguage:async(_preference:LanguagePreference):Promise<void>=>{throw new Error('Language provider unavailable');}};
const Context=createContext(defaultContext);

// Mount once per surface, outside conversation components; switching languages
// rerenders labels without remounting drafts, replaying work or reloading a page.
export function I18nProvider({children,bridge=nativeBridge}:{children:ReactNode;bridge?:LanguageBridge}) {
 const [state,setState]=useState(initialLanguage),[ready,setReady]=useState(false),[failed,setFailed]=useState(false);
 useEffect(()=>{
  let active=true,received=false;
  const accept=(next:LanguageState)=>{if(active){received=true;setState(current=>newerLanguage(current,next));setReady(true);setFailed(false);}};
  const unsubscribe=bridge.subscribe(accept);
  void bridge.read().then(accept).catch(()=>{if(active&&!received){setReady(true);setFailed(true);}});
  return()=>{active=false;unsubscribe();};
 },[bridge]);
 useEffect(()=>{document.documentElement.lang=state.locale;},[state.locale]);
 const value=useMemo(()=>({...state,...translator(state.locale),ready,failed,
  setLanguage:async(preference:LanguagePreference)=>{const next=await bridge.save(preference);setState(current=>newerLanguage(current,next));setFailed(false);},
 }),[state,bridge,ready,failed]);
 return <Context.Provider value={value}>{ready?children:null}</Context.Provider>;
}
export function useI18n(){return useContext(Context);}
export type { LanguagePreference, LanguageState, Locale } from './core.ts';
