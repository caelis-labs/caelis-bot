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
type Tool={name:string;title?:string;description?:string;readOnlyHint?:boolean;destructiveHint?:boolean;idempotentHint?:boolean;openWorldHint?:boolean};
type ServerDetail={state:string;tools:Tool[];error?:string;canConnect?:boolean};
type SkillDetail={body:string};
type Action='install'|'enable'|'disable'|'uninstall';
type View='browse'|'installed';

function statusLabel(status:string,t:ReturnType<typeof useI18n>['t']){
 switch(status){case 'ready':return t('settings.pluginStatus_ready');case 'disabled':return t('settings.pluginStatus_disabled');case 'failed':return t('settings.pluginStatus_failed');case 'update_available':return t('settings.pluginStatus_update_available');default:return t('settings.pluginStatus_available')}
}
function connectionLabel(state:string,t:ReturnType<typeof useI18n>['t']){
 switch(state){case 'connected':return t('settings.pluginConnection_connected');case 'disabled':return t('settings.pluginConnection_disabled');case 'not_configured':return t('settings.pluginConnection_not_configured');case 'pending':return t('settings.pluginConnection_pending');case 'authentication_required':return t('settings.pluginConnection_authentication_required');case 'failed':return t('settings.pluginConnection_failed');default:return t('settings.pluginConnection_unknown')}
}
function PluginIcon({item}:{item:Plugin}){
 const Icon=item.skills?.length&&!item.mcpServers?.length?BookOpenIcon:item.mcpServers?.length&&!item.skills?.length?PlugsConnectedIcon:PuzzlePieceIcon;
 return <span className="plugin-icon" aria-hidden="true"><Icon size={23} weight="regular"/></span>;
}

export function PluginSettings(){
 const {locale,t}=useI18n();
 const [snapshot,setSnapshot]=useState<Snapshot|null>(null),[selected,setSelected]=useState<string|null>(null),[view,setView]=useState<View>('browse');
 const [busy,setBusy]=useState<{id:string;action:Action}|null>(null),[error,setError]=useState(''),[confirmRemove,setConfirmRemove]=useState(false);
 const [more,setMore]=useState(false),[detail,setDetail]=useState<{kind:'skill'|'server';entry:Contribution}|null>(null),[detailData,setDetailData]=useState<ServerDetail|SkillDetail|null>(null),[detailError,setDetailError]=useState(false),[detailBusy,setDetailBusy]=useState(false);
 const backRef=useRef<HTMLButtonElement>(null),titleRef=useRef<HTMLHeadingElement>(null),closeRef=useRef<HTMLButtonElement>(null),dialogRef=useRef<HTMLDivElement>(null),detailOrigin=useRef<HTMLButtonElement|null>(null),detailSequence=useRef(0),keyboardNav=useRef(false);
 const load=async()=>{try{setSnapshot(await desktop<Snapshot>('Plugins'));setError('')}catch{setError(t('settings.pluginsLoadFailed'))}};
 useEffect(()=>{void load()},[t]);
 useEffect(()=>{if(keyboardNav.current){if(selected)backRef.current?.focus();else titleRef.current?.focus()}},[selected]);
 useEffect(()=>{const onKey=(event:KeyboardEvent)=>{if(event.key==='Escape'&&detail){event.stopImmediatePropagation();event.preventDefault();detailSequence.current++;setDetail(null);detailOrigin.current?.focus()}else if(event.key==='Escape'&&selected){event.stopImmediatePropagation();event.preventDefault();keyboardNav.current=true;setSelected(null);setConfirmRemove(false);setMore(false)}};window.addEventListener('keydown',onKey,true);return()=>window.removeEventListener('keydown',onKey,true)},[selected,detail]);
 const change=async(id:string,action:Action)=>{if(busy)return;setBusy({id,action});setError('');try{const next=await desktop<Snapshot>('PluginAction',id,action);setSnapshot(next);setConfirmRemove(false);setMore(false);setDetail(null);if(action==='uninstall'){keyboardNav.current=false;setSelected(null);setView('browse')}}catch{setError(t('settings.pluginsChangeFailed'));try{setSnapshot(await desktop<Snapshot>('Plugins'))}catch{ /* Keep the last confirmed snapshot. */ }}finally{setBusy(null)}};
 const openLink=async(url:string)=>{try{await backend('OpenMessageLink',url)}catch{setError(t('settings.pluginOpenLinkFailed'))}};
 const items=snapshot?.items||[];
 const active=items.find(item=>item.id===selected);
 const installedCount=items.filter(item=>item.installed).length;
 const visible=view==='browse'?items:items.filter(item=>item.installed);
 const goBack=(keyboard:boolean)=>{keyboardNav.current=keyboard;setSelected(null);setConfirmRemove(false);setMore(false);setDetail(null);setError('')};
 const open=(item:Plugin,keyboard:boolean)=>{keyboardNav.current=keyboard;setSelected(item.id);setConfirmRemove(false);setMore(false);setDetail(null);setError('')};
 const mainAction=(item:Plugin):Action=>!item.installed||item.status==='update_available'?'install':item.enabled?'disable':'enable';
 const actionLabel=(action:Action)=>({install:t('settings.pluginInstall'),enable:t('settings.pluginEnable'),disable:t('settings.pluginDisable'),uninstall:t('settings.pluginRemove')})[action];
 const busyLabel=(action:Action)=>({install:t('settings.pluginBusy_install'),enable:t('settings.pluginBusy_enable'),disable:t('settings.pluginBusy_disable'),uninstall:t('settings.pluginBusy_uninstall')})[action];
 const actionButton=(item:Plugin,compact=false)=>{const action=mainAction(item),working=busy?.id===item.id&&busy.action===action;return <button type="button" className={compact?'plugin-row-action':item.installed?'plugin-secondary':'plugin-primary primary'} disabled={!!busy} aria-busy={working} onClick={()=>void change(item.id,action)}>{working?busyLabel(action):item.status==='update_available'&&action==='install'?t('settings.pluginUpdate'):actionLabel(action)}</button>};
 const contribution=(item:Contribution)=>{const zh=locale==='zh-CN';return {name:(zh&&item.nameZh)||item.name,description:(zh&&item.descriptionZh)||item.description}};
 const showDetail=(kind:'skill'|'server',entry:Contribution,event:React.MouseEvent<HTMLButtonElement>)=>{detailSequence.current++;detailOrigin.current=event.currentTarget;setDetail({kind,entry});setDetailData(null);setDetailError(false)};
 const section=(label:string,kind:'skill'|'server',entries:Contribution[])=>entries.length>0&&<section className="plugin-contributions" aria-label={label}><h2>{label}</h2><div className="plugin-contribution-list">{entries.map(entry=>{const copy=contribution(entry);return <button type="button" className="plugin-contribution" key={entry.id} onClick={event=>showDetail(kind,entry,event)}><span><strong>{copy.name}</strong>{copy.description&&<small>{copy.description}</small>}</span><CaretRightIcon size={15} aria-hidden="true"/></button>})}</div></section>;
 const fetchDetail=async(kind:'skill'|'server',entry:Contribution,refresh=false)=>{if(!active)return;const sequence=++detailSequence.current;setDetailBusy(true);setDetailError(false);try{const data=kind==='skill'?await desktop<SkillDetail>('PluginSkillDetail',active.id,entry.id):await desktop<ServerDetail>('PluginServerDetail',active.id,entry.id,refresh);if(sequence===detailSequence.current)setDetailData(data)}catch{if(sequence===detailSequence.current)setDetailError(true)}finally{if(sequence===detailSequence.current)setDetailBusy(false)}};
 useEffect(()=>{if(!detail||!active)return;void fetchDetail(detail.kind,detail.entry);closeRef.current?.focus()},[detail?.kind,detail?.entry.id,active?.id,active?.version,snapshot?.revision]);
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
   <header className="plugin-detail-head"><PluginIcon item={active}/><div className="plugin-detail-identity"><h1>{active.title}</h1>{active.publisher&&<p>{active.publisherUrl?<button type="button" className="plugin-link" onClick={()=>void openLink(active.publisherUrl!)}>{active.publisher}</button>:active.publisher}</p>}</div><div className="plugin-head-actions">{actionButton(active)}{active.installed&&<div className="plugin-more-wrap"><button type="button" className="plugin-more" aria-label={t('settings.pluginMore')} aria-expanded={more} onClick={()=>{setMore(!more);setConfirmRemove(false)}} disabled={!!busy}>···</button>{more&&<div className="plugin-more-menu">{confirmRemove?<><span>{t('settings.pluginConfirmRemove')}</span><button type="button" disabled={!!busy} onClick={()=>void change(active.id,'uninstall')}>{busy?.action==='uninstall'?busyLabel('uninstall'):actionLabel('uninstall')}</button><button type="button" disabled={!!busy} onClick={()=>setConfirmRemove(false)}>{t('settings.pluginCancelRemove')}</button></>:<button type="button" disabled={!!busy} onClick={()=>setConfirmRemove(true)}>{actionLabel('uninstall')}</button>}</div>}</div>}</div></header>
   <p className="plugin-description">{active.description}</p>
   <div className="plugin-facts"><span>{t('settings.pluginVersion')} {active.version}</span>{(active.bundled||active.sourceUrl)&&<span>{active.bundled?t('settings.pluginBundledSource'):<button type="button" className="plugin-link" onClick={()=>void openLink(active.sourceUrl!)}>{t('settings.pluginSourceLink')}</button>}</span>}<span className={active.status==='failed'?'plugin-status-failed':''}>{statusLabel(active.status,t)}</span></div>
   {section(t('settings.pluginSkills'),'skill',active.skills||[])}
   {section(t('settings.pluginMCPServers'),'server',active.mcpServers||[])}
   {!!active.issues?.length&&<div className="plugin-issues" role="status"><span>{activeIssueLabels.length?activeIssueLabels.join(' · '):t('settings.pluginIssue')}</span><button type="button" onClick={()=>void load()} disabled={!!busy}>{t('settings.pluginRefresh')}</button></div>}
  </>:<>
   <h1 ref={titleRef} tabIndex={-1}>{t('settings.plugins')}</h1>
   <div className="plugin-tabs" role="group" aria-label={t('settings.pluginViews')}><button type="button" aria-pressed={view==='browse'} onClick={()=>setView('browse')}>{t('settings.pluginsBrowse')}</button><button type="button" aria-pressed={view==='installed'} onClick={()=>setView('installed')}>{t('settings.pluginsInstalled')}{installedCount>0&&<span className="plugin-count">{installedCount}</span>}</button></div>
   {visible.length?<div className="plugin-list">{visible.map(item=><div className="plugin-row" key={item.id}><button type="button" className="plugin-row-open" onClick={event=>open(item,event.detail===0)} aria-label={`${t('settings.pluginOpenDetails',{name:item.title})} ${item.installed?statusLabel(item.status,t):''}`}><PluginIcon item={item}/><span className="plugin-row-main"><strong>{item.title}</strong><small>{item.description}</small></span>{item.installed&&<span className="plugin-row-status">{statusLabel(item.status,t)}</span>}<CaretRightIcon size={16} className="plugin-chevron"/></button>{!item.installed&&actionButton(item,true)}</div>)}</div>:<p className="plugin-empty">{view==='installed'?t('settings.pluginsNoneInstalled'):t('settings.pluginsNoneAvailable')}</p>}
  </>}
  {error&&<div className="plugin-error" role="alert"><span>{error}</span><button type="button" onClick={()=>void load()} disabled={!!busy}>{t('settings.pluginRetry')}</button></div>}
  {detail&&<div className="plugin-dialog-backdrop" onMouseDown={event=>{if(event.target===event.currentTarget){detailSequence.current++;setDetail(null);detailOrigin.current?.focus()}}}><div ref={dialogRef} className="plugin-dialog" role="dialog" aria-modal="true" aria-labelledby="plugin-detail-title" onKeyDown={event=>{if(event.key!=='Tab')return;const elements=[...dialogRef.current?.querySelectorAll<HTMLElement>('button,summary,[tabindex]:not([tabindex="-1"])')||[]].filter(element=>!element.hasAttribute('disabled'));if(!elements.length)return;const first=elements[0],last=elements[elements.length-1];if(event.shiftKey&&document.activeElement===first){event.preventDefault();last.focus()}else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus()}}}><header><div><small>{detail.kind==='skill'?t('settings.pluginSkills'):t('settings.pluginMCPServers')}</small><h2 id="plugin-detail-title">{contribution(detail.entry).name}</h2></div><button ref={closeRef} type="button" aria-label={t('settings.pluginCloseDetail')} onClick={()=>{detailSequence.current++;setDetail(null);detailOrigin.current?.focus()}}>×</button></header>{contribution(detail.entry).description&&<p>{contribution(detail.entry).description}</p>}{detailBusy&&<p role="status">{t('settings.pluginDetailLoading')}</p>}{detailError&&<p role="alert">{t('settings.pluginDetailFailed')} <button type="button" onClick={()=>void fetchDetail(detail.kind,detail.entry,true)}>{t('settings.pluginRetry')}</button></p>}{detail.kind==='skill'&&detailData&&'body'in detailData&&detailData.body&&<div className="plugin-skill-body">{detailData.body}</div>}{detail.kind==='server'&&detailData&&'state'in detailData&&<><div className="plugin-connection-state">{t('settings.pluginConnection')}: {connectionLabel(detailData.state,t)}</div>{detailData.error&&<p role="status">{detailData.error}</p>}{detailData.tools.length>0&&<div className="plugin-tool-list">{detailData.tools.map(tool=><details key={tool.name}><summary>{tool.title||tool.name}</summary><div><strong>{tool.name}</strong>{tool.description&&<p>{tool.description}</p>}{([[t('settings.pluginHintReadOnly'),tool.readOnlyHint],[t('settings.pluginHintDestructive'),tool.destructiveHint],[t('settings.pluginHintIdempotent'),tool.idempotentHint],[t('settings.pluginHintOpenWorld'),tool.openWorldHint]] as const).filter(([,value])=>value!==undefined).map(([label,value])=><small key={label}>{label}: {value?t('settings.pluginHintYes'):t('settings.pluginHintNo')}</small>)}</div></details>)}</div>}{active?.enabled&&<button type="button" className="plugin-refresh-tools" disabled={detailBusy} onClick={()=>void fetchDetail('server',detail.entry,true)}>{t('settings.pluginRefreshTools')}</button>}</>}</div></div>}
 </section>;
}
