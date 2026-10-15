import { useEffect, useState } from 'react';
import { TerminalIcon } from '@phosphor-icons/react/dist/csr/Terminal';
import { PlugsConnectedIcon } from '@phosphor-icons/react/dist/csr/PlugsConnected';
import { SettingsChevron } from '../../SettingsIcons';
import { useI18n } from '../../i18n';
import type { ConnectChoice, ConnectionKind, RuntimeSettingsClient } from './types';

type Provider = ConnectChoice & { kind: ConnectionKind };
export const providerNames: Record<string,string> = { codex:'Codex',grok:'Grok',deepseek:'DeepSeek',xiaomi:'Xiaomi MiMo',magpie:'Magpie',ollama:'Ollama' };
const hints = {codex:'runtime.providerHintCodex',grok:'runtime.providerHintGrok',deepseek:'runtime.providerHintDeepseek',xiaomi:'runtime.providerHintXiaomi',magpie:'runtime.providerHintMagpie',ollama:'runtime.providerHintOllama',custom:'runtime.providerHintCustom'} as const;
const logos: Record<string,string> = { deepseek:'deepseek.ico',ollama:'ollama.png',xiaomi:'xiaomi.ico',grok:'grok.svg',magpie:'magpie.png' };
export function ProviderLogo({id}:{id:string}) {
 return <span className="onboarding-provider-logo" aria-hidden="true">{logos[id]?<img src={`/icons/provider-logos/${logos[id]}`} alt=""/>:id==='codex'?<TerminalIcon size={24}/>:<PlugsConnectedIcon size={24}/>}</span>;
}
export function OnboardingProviders({client,onSelect,disabled}:{client:RuntimeSettingsClient;onSelect:(kind:ConnectionKind,id:string)=>void;disabled:boolean}) {
 const {t}=useI18n();
 const [providers,setProviders]=useState<Provider[]>([]),[loading,setLoading]=useState(true),[error,setError]=useState(''),[retry,setRetry]=useState(0);
 useEffect(()=>{
  let live=true;setLoading(true);setError('');
  void Promise.all((['account','api-key'] as const).map(async kind=>({kind,catalog:await client.catalog(kind)}))).then(results=>{
   if(!live)return;
   setProviders(results.flatMap(({kind,catalog})=>catalog.choices.map(c=>({...c,kind}))));
   setError(results.map(r=>r.catalog.unavailable).filter(Boolean).join(' '));
  }).catch(e=>{if(live)setError(e instanceof Error?e.message:t('runtime.actionFailed'));}).finally(()=>{if(live)setLoading(false)});
  return()=>{live=false};
 },[client,retry]);
 return <div className="onboarding-provider-list">
  {loading&&<p role="status">{t('common.loading')}</p>}
  {providers.map(p=><button key={`${p.kind}:${p.id}`} disabled={disabled||loading} onClick={()=>onSelect(p.kind,p.id)}>
   <ProviderLogo id={p.id}/>
   <span className="onboarding-provider-copy"><strong>{providerNames[p.id]|| (p.id==='custom'?t('runtime.providerCustom'):p.name)}</strong><small>{p.id in hints?t(hints[p.id as keyof typeof hints]):p.description}</small></span><SettingsChevron/>
  </button>)}
  {error&&<p className="inline-error" role="alert">{error}</p>}
  {!loading&&!providers.length&&<button onClick={()=>setRetry(n=>n+1)}>{t('runtime.recheck')}</button>}
 </div>;
}
