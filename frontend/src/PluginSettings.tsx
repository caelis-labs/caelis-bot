import {useEffect, useState} from 'react';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import './settings/plugins.css';

type Issue={component:string;name:string;message:string};
type Plugin={id:string;title:string;version:string;description:string;source:string;installed:boolean;enabled:boolean;status:string;issues:Issue[]};
type Snapshot={revision:number;items:Plugin[]};
function statusLabel(status:string,t:ReturnType<typeof useI18n>['t']){
 switch(status){case 'ready':return t('settings.pluginStatus_ready');case 'disabled':return t('settings.pluginStatus_disabled');case 'failed':return t('settings.pluginStatus_failed');case 'update_available':return t('settings.pluginStatus_update_available');default:return t('settings.pluginStatus_available')}
}

export function PluginSettings(){
 const {t}=useI18n();
 const [snapshot,setSnapshot]=useState<Snapshot|null>(null),[selected,setSelected]=useState<string|null>(null);
 const [busy,setBusy]=useState(false),[error,setError]=useState(''),[confirmRemove,setConfirmRemove]=useState(false);
 const load=()=>desktop<Snapshot>('Plugins').then(setSnapshot).catch(()=>setError(t('settings.pluginsLoadFailed')));
 useEffect(()=>{void load()},[t]);
 useEffect(()=>{const onKey=(event:KeyboardEvent)=>{if(event.key==='Escape'&&selected){event.stopImmediatePropagation();event.preventDefault();setSelected(null);setConfirmRemove(false)}};window.addEventListener('keydown',onKey,true);return()=>window.removeEventListener('keydown',onKey,true)},[selected]);
 const change=async(id:string,action:string)=>{if(busy)return;setBusy(true);setError('');try{setSnapshot(await desktop<Snapshot>('PluginAction',id,action));setConfirmRemove(false)}catch(e){setError(e instanceof Error?e.message:t('settings.pluginsChangeFailed'));void load()}finally{setBusy(false)}};
 const items=snapshot?.items||[];
 const active=items.find(item=>item.id===selected);
 const row=(item:Plugin)=><button className="plugin-row" key={item.id} onClick={()=>{setSelected(item.id);setConfirmRemove(false);setError('')}}><span className="plugin-row-main"><strong>{item.title}</strong><small>{item.description}</small></span><span className="plugin-row-status">{statusLabel(item.status,t)}</span><span aria-hidden="true">›</span></button>;
 return <section className="plugin-settings">
  {active?<>
   <button className="plugin-back" onClick={()=>{setSelected(null);setConfirmRemove(false);setError('')}}>‹ {t('settings.pluginsBack')}</button>
   <h1>{active.title}</h1><p className="plugin-description">{active.description}</p>
   <div className="plugin-details"><div><span>{t('settings.pluginVersion')}</span><strong>{active.version}</strong></div><div><span>{t('settings.pluginSource')}</span><strong>{active.source.startsWith('bundled:')?t('settings.pluginBundledSource'):active.source}</strong></div><div><span>{t('settings.pluginState')}</span><strong>{statusLabel(active.status,t)}</strong></div></div>
   {active.issues.length>0&&<div className="plugin-issues" role="status">{active.issues.map((issue,i)=><p key={`${issue.component}-${issue.name}-${i}`}>{issue.message}</p>)}</div>}
   <div className="plugin-actions">{!active.installed?<button disabled={busy} onClick={()=>void change(active.id,'install')}>{t('settings.pluginInstall')}</button>:<><button disabled={busy} onClick={()=>void change(active.id,active.enabled?'disable':'enable')}>{t(active.enabled?'settings.pluginDisable':'settings.pluginEnable')}</button><button className="plugin-remove" disabled={busy} onClick={()=>{if(confirmRemove)void change(active.id,'uninstall');else setConfirmRemove(true)}}>{t(confirmRemove?'settings.pluginConfirmRemove':'settings.pluginRemove')}</button></>}</div>
  </>:<><h1>{t('settings.plugins')}</h1><p className="plugin-intro">{t('settings.pluginsIntro')}</p>
   <h2>{t('settings.pluginsInstalled')}</h2><div className="plugin-list">{items.filter(item=>item.installed).length?items.filter(item=>item.installed).map(row):<p className="plugin-empty">{t('settings.pluginsNoneInstalled')}</p>}</div>
   <h2>{t('settings.pluginsAvailable')}</h2><div className="plugin-list">{items.filter(item=>!item.installed).length?items.filter(item=>!item.installed).map(row):<p className="plugin-empty">{t('settings.pluginsNoneAvailable')}</p>}</div>
  </>}
  {error&&<p className="plugin-error" role="alert">{error}</p>}
 </section>;
}
