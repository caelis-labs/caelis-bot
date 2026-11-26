import {useEffect,useRef,useState} from 'react';
import { CopyIcon } from '@phosphor-icons/react/dist/csr/Copy';
import {backend,desktop} from './desktop';
import type {ScreenImage,ScreenPresentation} from './backend/contract';
import {useI18n} from './i18n';
import { XIcon } from './SettingsIcons';

function useImage(id:string,thumbnail:boolean) {
 const [url,setURL]=useState(''),[failed,setFailed]=useState(false);
 useEffect(()=>{
  let live=true;setURL('');setFailed(false);
  void backend<string>('ScreenImage',id,thumbnail).then(value=>{
   if(!live)return;
   if(/^data:image\/(png|jpeg);base64,/.test(value))setURL(value);else setFailed(true);
  }).catch(()=>{if(live)setFailed(true)});
  return()=>{live=false};
 },[id,thumbnail]);
 return {url,failed,fail:()=>setFailed(true)};
}
function Thumbnail({image,label,onClick}:{image:ScreenImage;label:string;onClick:()=>void}) {
 const {t}=useI18n();const {url,failed,fail}=useImage(image.id,true);
 return <button className="screen-selection" onClick={onClick} aria-label={label} title={label}>
  {url&&!failed?<img src={url} alt="" width={image.width} height={image.height} onError={fail}/>:<span className="screen-image-placeholder">{failed?t('chat.screenImageUnavailable'):t('common.loading')}</span>}
 </button>;
}
function ImageViewer({image,close,report}:{image:ScreenImage;close:()=>void;report:(message:string)=>void}) {
 const {t}=useI18n(),dialog=useRef<HTMLDialogElement>(null),{url,failed,fail}=useImage(image.id,false);
 const label=t('chat.screenSelection');
 useEffect(()=>{dialog.current?.showModal();return()=>{dialog.current?.close()}},[]);
 return <dialog className="screen-viewer" ref={dialog} aria-label={label} onCancel={close} onClick={e=>{if(e.target===e.currentTarget)close()}}
  onKeyDown={e=>{if(e.key==='Escape'||(e.metaKey&&e.key==='w')){e.stopPropagation();e.preventDefault();close()}}}>
  <div className="screen-viewer-toolbar"><span>{label}</span><button aria-label={t('chat.screenCopy')} title={t('chat.screenCopy')} disabled={!url||failed}
   onClick={()=>void desktop('CopyScreenImage',image.id).catch(()=>report(t('chat.screenCopyFailed')))}><CopyIcon size={17} aria-hidden="true"/></button>
   <button aria-label={t('common.close')} title={t('common.close')} onClick={close}><XIcon size={17} aria-hidden="true"/></button></div>
  {url&&!failed?<img className="screen-full-image" src={url} alt={label} onError={fail}/>:<p role="status">{failed?t('chat.screenImageUnavailable'):t('common.loading')}</p>}
 </dialog>;
}
export function ScreenMessage({value,note,report}:{value:ScreenPresentation;note:string;report:(message:string)=>void}) {
 const {t}=useI18n(),[open,setOpen]=useState<ScreenImage|null>(null);
 useEffect(()=>{const close=()=>setOpen(null);window.addEventListener('history-close',close);return()=>window.removeEventListener('history-close',close);},[]);
 const selection=value.images?.find(i=>i.role==='selection');
 return <div className="screen-message">
  {selection?<div className="screen-image-card"><Thumbnail image={selection} label={t('chat.screenSelection')} onClick={()=>setOpen(selection)}/>
   <div className="screen-source">{value.application||t('chat.screenSelection')}</div></div>:<div className="screen-missing">{t('chat.screenImageUnavailable')}</div>}
  {note&&<p className="screen-note">{note}</p>}
  {open&&<ImageViewer image={open} close={()=>setOpen(null)} report={report}/>}
 </div>;
}
