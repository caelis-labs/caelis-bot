import {isWebSurface} from './transport';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { backend, desktop } from './desktop';
import type { Artifact,TaskFileCopy } from './backend/contract';
import { useI18n } from './i18n';
import { PaperclipIcon } from '@phosphor-icons/react/dist/csr/Paperclip';

const textFile = (name: string) => /\.(txt|md|markdown|json|jsonl|sql|log|csv|tsv|ya?ml|xml|toml|ini|sh|py|go|swift|js|ts|tsx|css|html)$/i.test(name);
const fileLabel = (value: string) => {
 const name = value.replaceAll('\\','/').split('/').at(-1) || 'artifact';
 return name.length > 255 || /[\u0000-\u001f\u007f]/.test(name) ? 'artifact' : name;
};

export function ArtifactLink({ file, report, attachment = false, label }: { file: Artifact; report: (message: string) => void; attachment?: boolean; label?: ReactNode }) {
 const { t } = useI18n();
 const trigger = useRef<HTMLButtonElement>(null);
 const menu = useRef<HTMLDivElement>(null);
 const [position,setPosition] = useState<{x:number;y:number}|null>(null);
 const openMenu = (x:number, y:number) => setPosition({x:Math.max(8,Math.min(x,innerWidth-210)),y:Math.max(8,Math.min(y,innerHeight-235))});
 const [copyInfo,setCopyInfo]=useState<TaskFileCopy|null>(null),[fetching,setFetching]=useState(false);
 const name = fileLabel(file.name);
 const dot = name.lastIndexOf('.');
 const extension = dot > 0 && name.length - dot <= 12 ? name.slice(dot) : '';
 const attachmentLabel = <><PaperclipIcon size={14} aria-hidden="true"/><span className="sent-file-name">{name.slice(0, name.length - extension.length)}</span>{extension && <span className="sent-file-extension">{extension}</span>}</>;
 const inline = label !== undefined;
 const isText = textFile(name);
 const failure = () => report(t('chat.artifactUnavailable'));
 const action = (method:string,...args:unknown[]) => {
  setPosition(null);
  if(file.id.startsWith('task-file:'))void backend<TaskFileCopy>('WorkerFileCopy',file.id,false).then(setCopyInfo).catch(()=>{});
  void (async()=>{
   await desktop(method,file.id,...args);
  })().catch(failure);
 };
 const refresh=async()=>{
  if(fetching)return;setFetching(true);setPosition(null);
  try{setCopyInfo(await backend<TaskFileCopy>('WorkerFileCopy',file.id,true));}catch{failure();}finally{setFetching(false);}
 };
 useEffect(() => {
  if(!position)return;
  const outside=(event:PointerEvent)=>{if(!menu.current?.contains(event.target as Node)&&event.target!==trigger.current)setPosition(null);};
  const escape=(event:KeyboardEvent)=>{if(event.key==='Escape'){event.preventDefault();event.stopImmediatePropagation();setPosition(null);trigger.current?.focus();}};
  window.addEventListener('pointerdown',outside,true);window.addEventListener('keydown',escape,true);
  return()=>{window.removeEventListener('pointerdown',outside,true);window.removeEventListener('keydown',escape,true);};
 },[position]);
 if(inline && !file.id)return <span className="message-link-unavailable">{label} <small>({t('chat.artifactUnavailable')})</small></span>;
 if(attachment && !file.id)return <span className="sent-file" title={name}>{attachmentLabel}</span>;
 return <>
  <button ref={trigger} type="button" role={inline?'link':undefined} className={inline?'message-link':attachment?'sent-file':'artifact-link'} title={inline||attachment?name:undefined} aria-label={attachment?name:undefined} onClick={()=>action(isText?'ShowArtifactPreview':'OpenArtifact')} onContextMenu={event=>{
   event.preventDefault();event.stopPropagation();openMenu(event.clientX,event.clientY);
  }} onKeyDown={event=>{if(event.key==='ContextMenu'||event.shiftKey&&event.key==='F10'){event.preventDefault();const box=event.currentTarget.getBoundingClientRect();openMenu(box.left,box.bottom+4);}}}>
   {inline?label:attachment?attachmentLabel:<><span aria-hidden="true">↗</span>{name}</>}
  </button>
  {copyInfo&&<small className="settings-note" role="status">{t(copyInfo.cached?'chat.workerFileCached':'chat.workerFileFetched',{time:new Date(copyInfo.fetchedAt).toLocaleString()})}</small>}
  {position&&createPortal(<div ref={menu} className="artifact-context-menu" role="menu" style={{left:position.x,top:position.y}}>
   <button role="menuitem" onClick={()=>action('OpenArtifact')}>{t('chat.openArtifact')}</button>
   <button role="menuitem" onClick={()=>action('SaveArtifactAs',name)}>{t('chat.saveArtifactAs')}</button>
   {file.id.startsWith('task-file:')&&<button role="menuitem" disabled={fetching} onClick={()=>void refresh()}>{t('chat.workerFileRefresh')}</button>}
   {!isWebSurface()&&<button role="menuitem" onClick={()=>action('CopyArtifactPath')}>{t('chat.copyArtifactPath')}</button>}
   {isText&&<button role="menuitem" onClick={()=>action('CopyArtifactContent')}>{t('chat.copyArtifactContent')}</button>}
   {!isWebSurface()&&<button role="menuitem" onClick={()=>{setPosition(null);void backend('RevealArtifact',file.id).catch(failure);}}>{t('chat.revealInFinder')}</button>}
  </div>,document.body)}
 </>;
}
