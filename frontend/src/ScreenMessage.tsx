import {useEffect,useRef,useState} from 'react';
import {backend,desktop} from './desktop';
import type {ScreenImage,ScreenPresentation} from './backend/contract';
import {useI18n} from './i18n';
import './screen-message.css';

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
function Thumbnail({image,label,onClick,context=false}:{image:ScreenImage;label:string;onClick:()=>void;context?:boolean}) {
 const {t}=useI18n();const {url,failed,fail}=useImage(image.id,true);
 return <button className={context?'screen-context':'screen-selection'} onClick={onClick} aria-label={label} title={label}>
  {url&&!failed?<img src={url} alt="" width={image.width} height={image.height} onError={fail}/>:<span className="screen-image-placeholder">{failed?t('chat.screenImageUnavailable'):t('common.loading')}</span>}
  {context&&<span>{label}</span>}
 </button>;
}
function ImageViewer({image,close,report}:{image:ScreenImage;close:()=>void;report:(message:string)=>void}) {
 const {t}=useI18n(),dialog=useRef<HTMLDialogElement>(null),{url,failed,fail}=useImage(image.id,false);
 const label=image.role==='context'?t('chat.screenContext'):t('chat.screenSelection');
 useEffect(()=>{dialog.current?.showModal();return()=>{dialog.current?.close()}},[]);
 return <dialog className="screen-viewer" ref={dialog} aria-label={label} onCancel={close} onClick={e=>{if(e.target===e.currentTarget)close()}}
  onKeyDown={e=>{if(e.key==='Escape'||(e.metaKey&&e.key==='w')){e.stopPropagation();e.preventDefault();close()}}}>
  <div className="screen-viewer-toolbar"><span>{label}</span><button aria-label={t('chat.screenCopy')} title={t('chat.screenCopy')} disabled={!url||failed}
   onClick={()=>void desktop('CopyScreenImage',image.id).catch(()=>report(t('chat.screenCopyFailed')))}><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"/></svg></button>
   <button aria-label={t('common.close')} title={t('common.close')} onClick={close}><img className="symbol" src="/icons/xmark.png" alt=""/></button></div>
  {url&&!failed?<img className="screen-full-image" src={url} alt={label} onError={fail}/>:<p role="status">{failed?t('chat.screenImageUnavailable'):t('common.loading')}</p>}
 </dialog>;
}
export function ScreenMessage({value,note,report}:{value:ScreenPresentation;note:string;report:(message:string)=>void}) {
 const {t}=useI18n(),[open,setOpen]=useState<ScreenImage|null>(null);
 const selection=value.images?.find(i=>i.role==='selection'),context=value.images?.find(i=>i.role==='context');
 return <div className="screen-message">
  {selection?<div className="screen-image-card"><Thumbnail image={selection} label={t('chat.screenSelection')} onClick={()=>setOpen(selection)}/>
   <div className="screen-source">{value.application||t('chat.screenSelection')}</div></div>:<div className="screen-missing">{t('chat.screenImageUnavailable')}</div>}
  {context&&<Thumbnail image={context} label={t('chat.screenContext')} context onClick={()=>setOpen(context)}/>}
  {note&&<p className="screen-note">{note}</p>}
  {open&&<ImageViewer image={open} close={()=>setOpen(null)} report={report}/>}
 </div>;
}
