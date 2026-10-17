import { useEffect, useState } from 'react';
import { TerminalIcon } from '@phosphor-icons/react/dist/csr/Terminal';
import { PlugsConnectedIcon } from '@phosphor-icons/react/dist/csr/PlugsConnected';
import { SettingsChevron } from '../../SettingsIcons';
import { useI18n } from '../../i18n';
import { presentationWarning } from '../../presentation-warning';
import { SetupLoading } from '../../SetupLoading';
import { SetupBack } from '../../SetupBack';
import type { ConnectChoice, ConnectionKind, RuntimeSettingsClient } from './types';

type Provider = ConnectChoice & { kind: ConnectionKind };
export const providerNames: Record<string,string> = { codex:'Codex',grok:'Grok',deepseek:'DeepSeek',xiaomi:'Xiaomi MiMo',magpie:'Magpie',ollama:'Ollama' };
const hints = {codex:'runtime.providerHintCodex',grok:'runtime.providerHintGrok',deepseek:'runtime.providerHintDeepseek',xiaomi:'runtime.providerHintXiaomi',magpie:'runtime.providerHintMagpie',ollama:'runtime.providerHintOllama',custom:'runtime.providerHintCustom'} as const;
const logos: Record<string,string> = { deepseek:'deepseek.ico',ollama:'ollama.png',xiaomi:'xiaomi.ico',grok:'grok.svg',magpie:'magpie.png' };
export function ProviderLogo({id}:{id:string}) {
 return <span className="onboarding-provider-logo" aria-hidden="true">{logos[id]?<img src={`/icons/provider-logos/${logos[id]}`} alt=""/>:id==='codex'?<TerminalIcon size={24}/>:<PlugsConnectedIcon size={24}/>}</span>;
}
export function OnboardingProviders({client,onSelect,disabled,onBack}:{client:RuntimeSettingsClient;onSelect:(kind:ConnectionKind,id:string)=>void;disabled:boolean;onBack?:()=>void}) {
 const {t}=useI18n();
 const [providers,setProviders]=useState<Provider[]>([]),[loading,setLoading]=useState(true),[retry,setRetry]=useState(0);
 useEffect(()=>{
  let live=true;setLoading(true);
  // Catalogs are safe reads. Retry each independently once, retaining the other
  // catalog if one provider family is unavailable. Never retry a connection.
  void Promise.allSettled((['account','api-key'] as const).map(async kind=>{
   try{return {kind,catalog:await client.catalog(kind)};}
   catch{if(!live)throw new Error('cancelled');return {kind,catalog:await client.catalog(kind)};}
  })).then(results=>{
   if(!live)return;
   if(results.some(r=>r.status==='rejected'||r.value.catalog.unavailable))presentationWarning('background_read_failed');
   setProviders(results.flatMap(r=>r.status==='fulfilled'?r.value.catalog.choices.map(c=>({...c,kind:r.value.kind})):[]));
  }).finally(()=>{if(live)setLoading(false)});
  return()=>{live=false};
 },[client,retry]);
 if(loading)return <SetupLoading/>;
 return <section className="runtime-onboarding">
  {onBack&&<><SetupBack disabled={disabled} onClick={onBack}/><header><h1>{t('settings.setupConnectModel')}</h1><p className="setup-lead">{t('settings.setupConnectModelHint')}</p></header></>}
  <div className="onboarding-provider-list">
  {providers.map(p=><button key={`${p.kind}:${p.id}`} disabled={disabled||loading} onClick={()=>onSelect(p.kind,p.id)}>
   <ProviderLogo id={p.id}/>
   <span className="onboarding-provider-copy"><strong>{providerNames[p.id]|| (p.id==='custom'?t('runtime.providerCustom'):p.name)}</strong><small>{p.id in hints?t(hints[p.id as keyof typeof hints]):p.description}</small></span><SettingsChevron/>
  </button>)}
  {!loading&&!providers.length&&<button onClick={()=>setRetry(n=>n+1)}>{t('runtime.recheck')}</button>}
 </div></section>;
}
