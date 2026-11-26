import {useEffect,useRef,useState} from 'react';
import {createPortal} from 'react-dom';
import {backend} from './desktop';
import type {MediaImage,MediaPresentation} from './backend/contract';
import {useI18n} from './i18n';

export function MessageImage({value,label=value.name,inline=false}:{value:MediaImage;label?:string;inline?:boolean}) {
 const {t}=useI18n(),[url,setURL]=useState(''),[failed,setFailed]=useState(false),[open,setOpen]=useState(false),[full,setFull]=useState(''),[fullFailed,setFullFailed]=useState(false),dialog=useRef<HTMLDialogElement>(null),trigger=useRef<HTMLButtonElement>(null);
 const returnFocus=useRef(false);
 const dismiss=()=>{returnFocus.current=true;setOpen(false);};
 useEffect(()=>{const close=()=>{returnFocus.current=false;setOpen(false);};window.addEventListener('history-close',close);return()=>window.removeEventListener('history-close',close);},[]);
 useEffect(()=>{
  let active=true;setURL('');setFailed(false);
  if(value.unavailable){setFailed(true);return()=>{active=false}}
  void backend<string>('MediaImage',value.id,true).then(data=>{
   if(!active)return;
   if(/^data:image\/(png|jpeg|gif|webp);base64,/.test(data))setURL(data);else setFailed(true);
  }).catch(()=>{if(active)setFailed(true)});
  return()=>{active=false};
 },[value.id,value.unavailable]);
 useEffect(()=>{
  let active=true;setFull('');setFullFailed(false);
  if(open)void backend<string>('MediaImage',value.id,false).then(data=>{
   if(!active)return;
   if(/^data:image\/(png|jpeg|gif|webp);base64,/.test(data))setFull(data);else setFullFailed(true);
  }).catch(()=>{if(active)setFullFailed(true)});
  return()=>{active=false};
 },[open,value.id]);
 useEffect(()=>{if(open){if(!dialog.current?.open)dialog.current?.showModal()}else if(dialog.current?.open)dialog.current.close()},[open]);
 return <span className={`media-image${inline?' media-inline-image':''}`}>
  <button ref={trigger} className="media-thumbnail" type="button" disabled={!url||failed} onClick={()=>setOpen(true)} aria-label={t('chat.mediaOpen',{name:label})}>
   {url&&!failed?<img src={url} alt={label} onError={()=>setFailed(true)}/>:<span role="status">{failed?t('chat.mediaUnavailable'):t('common.loading')}</span>}
  </button>
  <span className="media-name" title={label}>{label}</span>
  {createPortal(<dialog className="screen-viewer" ref={dialog} aria-label={label} onCancel={e=>{e.preventDefault();dismiss();}} onClose={()=>{if(returnFocus.current)trigger.current?.focus({preventScroll:true});returnFocus.current=false;}}
   onKeyDown={e=>{if(e.key==='Escape'){e.preventDefault();e.stopPropagation();dismiss();}}}
   onClick={e=>{if(e.target===e.currentTarget)dismiss();}}>
   <div className="screen-viewer-toolbar"><span>{label}</span><button type="button" onClick={dismiss} aria-label={t('common.close')}>×</button></div>
   {full&&!fullFailed?<img className="screen-full-image" src={full} alt={label} onError={()=>setFullFailed(true)}/>:<p role="status">{fullFailed?t('chat.mediaUnavailable'):t('common.loading')}</p>}
  </dialog>,document.body)}
 </span>;
}
export function MediaMessage({value,note}:{value:MediaPresentation;note:string}) {
 return <div className="media-message">
  {note&&<p className="media-caption">{note}</p>}
  <div className="media-grid">{value.images.map(image=><MessageImage key={image.id} value={image}/>)}</div>
 </div>;
}
