import {useEffect, useRef, useState} from 'react';
import {BookOpenIcon} from '@phosphor-icons/react/dist/csr/BookOpen';
import {PlugsConnectedIcon} from '@phosphor-icons/react/dist/csr/PlugsConnected';
import {PuzzlePieceIcon} from '@phosphor-icons/react/dist/csr/PuzzlePiece';
import {CaretLeftIcon} from '@phosphor-icons/react/dist/csr/CaretLeft';
import {CaretRightIcon} from '@phosphor-icons/react/dist/csr/CaretRight';
import {backend, desktop} from './desktop';
import {useI18n} from './i18n';
import './settings/plugins.css';

type Issue={component:string;name:string;message:string};
type Contribution={id:string;name:string;description?:string;nameZh?:string;descriptionZh?:string};
type Plugin={id:string;title:string;version:string;description:string;publisher?:string;publisherUrl?:string;sourceUrl?:string;bundled?:boolean;skills?:Contribution[];mcpServers?:Contribution[];installed:boolean;enabled:boolean;status:string;issues?:Issue[]};
type Snapshot={revision:number;items:Plugin[]};
type Action='install'|'enable'|'disable'|'uninstall';
type View='browse'|'installed';

function statusLabel(status:string,t:ReturnType<typeof useI18n>['t']){
 switch(status){case 'ready':return t('settings.pluginStatus_ready');case 'disabled':return t('settings.pluginStatus_disabled');case 'failed':return t('settings.pluginStatus_failed');case 'update_available':return t('settings.pluginStatus_update_available');default:return t('settings.pluginStatus_available')}
}
function PluginIcon({item}:{item:Plugin}){
 const Icon=item.skills?.length&&!item.mcpServers?.length?BookOpenIcon:item.mcpServers?.length&&!item.skills?.length?PlugsConnectedIcon:PuzzlePieceIcon;
 return <span className="plugin-icon" aria-hidden="true"><Icon size={23} weight="regular"/></span>;
}

export function PluginSettings(){
 const {locale,t}=useI18n();
 const [snapshot,setSnapshot]=useState<Snapshot|null>(null),[selected,setSelected]=useState<string|null>(null),[view,setView]=useState<View>('browse');
 const [busy,setBusy]=useState<{id:string;action:Action}|null>(null),[error,setError]=useState(''),[confirmRemove,setConfirmRemove]=useState(false);
 const backRef=useRef<HTMLButtonElement>(null),titleRef=useRef<HTMLHeadingElement>(null),keyboardNav=useRef(false);
 const load=async()=>{try{setSnapshot(await desktop<Snapshot>('Plugins'));setError('')}catch{setError(t('settings.pluginsLoadFailed'))}};
 useEffect(()=>{void load()},[t]);
 useEffect(()=>{if(keyboardNav.current){if(selected)backRef.current?.focus();else titleRef.current?.focus()}},[selected]);
 useEffect(()=>{const onKey=(event:KeyboardEvent)=>{if(event.key==='Escape'&&selected){event.stopImmediatePropagation();event.preventDefault();keyboardNav.current=true;setSelected(null);setConfirmRemove(false)}};window.addEventListener('keydown',onKey,true);return()=>window.removeEventListener('keydown',onKey,true)},[selected]);
 const change=async(id:string,action:Action)=>{if(busy)return;setBusy({id,action});setError('');try{const next=await desktop<Snapshot>('PluginAction',id,action);setSnapshot(next);setConfirmRemove(false);if(action==='uninstall'){keyboardNav.current=false;setSelected(null);setView('browse')}}catch{setError(t('settings.pluginsChangeFailed'));try{setSnapshot(await desktop<Snapshot>('Plugins'))}catch{ /* Keep the last confirmed snapshot. */ }}finally{setBusy(null)}};
 const openLink=async(url:string)=>{try{await backend('OpenMessageLink',url)}catch{setError(t('settings.pluginOpenLinkFailed'))}};
 const items=snapshot?.items||[];
 const active=items.find(item=>item.id===selected);
 const installedCount=items.filter(item=>item.installed).length;
 const visible=view==='browse'?items:items.filter(item=>item.installed);
 const goBack=(keyboard:boolean)=>{keyboardNav.current=keyboard;setSelected(null);setConfirmRemove(false);setError('')};
 const open=(item:Plugin,keyboard:boolean)=>{keyboardNav.current=keyboard;setSelected(item.id);setConfirmRemove(false);setError('')};
 const mainAction=(item:Plugin):Action=>!item.installed||item.status==='update_available'?'install':item.enabled?'disable':'enable';
 const actionLabel=(action:Action)=>({install:t('settings.pluginInstall'),enable:t('settings.pluginEnable'),disable:t('settings.pluginDisable'),uninstall:t('settings.pluginRemove')})[action];
 const busyLabel=(action:Action)=>({install:t('settings.pluginBusy_install'),enable:t('settings.pluginBusy_enable'),disable:t('settings.pluginBusy_disable'),uninstall:t('settings.pluginBusy_uninstall')})[action];
 const actionButton=(item:Plugin,compact=false)=>{const action=mainAction(item),working=busy?.id===item.id&&busy.action===action;return <button type="button" className={compact?'plugin-row-action':'plugin-primary primary'} disabled={!!busy} aria-busy={working} onClick={()=>void change(item.id,action)}>{working?busyLabel(action):item.status==='update_available'&&action==='install'?t('settings.pluginUpdate'):actionLabel(action)}</button>};
 const contribution=(item:Contribution)=>{const zh=locale==='zh-CN';return {name:(zh&&item.nameZh)||item.name,description:(zh&&item.descriptionZh)||item.description}};
 const section=(label:string,entries:Contribution[])=>entries.length>0&&<section className="plugin-contributions" aria-label={label}><h2>{label}</h2><div className="plugin-contribution-list">{entries.map(entry=>{const copy=contribution(entry);return <div className="plugin-contribution" key={entry.id}><strong>{copy.name}</strong>{copy.description&&<p>{copy.description}</p>}</div>})}</div></section>;
 const issueLabels=(item:Plugin)=>[...new Set((item.issues||[]).map(issue=>{
  const skill=issue.component==='skill'&&item.skills?.find(entry=>entry.id===issue.name);
  if(skill)return t('settings.pluginIssueSkill',{name:contribution(skill).name});
  const server=issue.component==='server'&&item.mcpServers?.find(entry=>entry.id===issue.name);
  if(server)return t('settings.pluginIssueServer',{name:contribution(server).name});
  return '';
 }).filter(Boolean))];
 const activeIssueLabels=active?issueLabels(active):[];
 return <section className="plugin-settings">
  {active?<>
   <button ref={backRef} type="button" className="plugin-back" onClick={event=>goBack(event.detail===0)}><CaretLeftIcon size={15}/>{t('settings.pluginsBack')}</button>
   <header className="plugin-detail-head"><PluginIcon item={active}/><div className="plugin-detail-identity"><h1>{active.title}</h1>{active.publisher&&<p>{active.publisherUrl?<button type="button" className="plugin-link" onClick={()=>void openLink(active.publisherUrl!)}>{active.publisher}</button>:active.publisher}</p>}</div>{actionButton(active)}</header>
   <p className="plugin-description">{active.description}</p>
   <div className="plugin-facts"><span>{t('settings.pluginVersion')} {active.version}</span>{(active.bundled||active.sourceUrl)&&<span>{active.bundled?t('settings.pluginBundledSource'):<button type="button" className="plugin-link" onClick={()=>void openLink(active.sourceUrl!)}>{t('settings.pluginSourceLink')}</button>}</span>}<span className={active.status==='failed'?'plugin-status-failed':''}>{statusLabel(active.status,t)}</span></div>
   {section(t('settings.pluginSkills'),active.skills||[])}
   {section(t('settings.pluginMCPServers'),active.mcpServers||[])}
   {!!active.issues?.length&&<div className="plugin-issues" role="status"><span>{activeIssueLabels.length?activeIssueLabels.join(' · '):t('settings.pluginIssue')}</span><button type="button" onClick={()=>void load()} disabled={!!busy}>{t('settings.pluginRefresh')}</button></div>}
   {active.installed&&<div className="plugin-bottom-action">{confirmRemove?<><span>{t('settings.pluginConfirmRemove')}</span><button type="button" disabled={!!busy} onClick={()=>void change(active.id,'uninstall')}>{busy?.action==='uninstall'?t('settings.pluginBusy_uninstall'):t('settings.pluginRemove')}</button><button type="button" onClick={()=>setConfirmRemove(false)} disabled={!!busy}>{t('settings.pluginCancelRemove')}</button></>:<button type="button" className="plugin-link" disabled={!!busy} onClick={()=>setConfirmRemove(true)}>{t('settings.pluginRemove')}</button>}</div>}
  </>:<>
   <h1 ref={titleRef} tabIndex={-1}>{t('settings.plugins')}</h1>
   <div className="plugin-tabs" role="group" aria-label={t('settings.pluginViews')}><button type="button" aria-pressed={view==='browse'} onClick={()=>setView('browse')}>{t('settings.pluginsBrowse')}</button><button type="button" aria-pressed={view==='installed'} onClick={()=>setView('installed')}>{t('settings.pluginsInstalled')}{installedCount>0&&<span className="plugin-count">{installedCount}</span>}</button></div>
   {visible.length?<div className="plugin-list">{visible.map(item=><div className="plugin-row" key={item.id}><button type="button" className="plugin-row-open" onClick={event=>open(item,event.detail===0)} aria-label={t('settings.pluginOpenDetails',{name:item.title})}><PluginIcon item={item}/><span className="plugin-row-main"><strong>{item.title}</strong><small>{item.description}</small></span><CaretRightIcon size={16} className="plugin-chevron"/></button>{!item.installed?actionButton(item,true):<span className="plugin-row-status">{statusLabel(item.status,t)}</span>}</div>)}</div>:<p className="plugin-empty">{view==='installed'?t('settings.pluginsNoneInstalled'):t('settings.pluginsNoneAvailable')}</p>}
  </>}
  {error&&<div className="plugin-error" role="alert"><span>{error}</span><button type="button" onClick={()=>void load()} disabled={!!busy}>{t('settings.pluginRetry')}</button></div>}
 </section>;
}
