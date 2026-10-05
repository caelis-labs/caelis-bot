import {useEffect,useRef,useState} from 'react';
import {backend} from './desktop';
import type {MediaImage,MediaPresentation} from './backend/contract';
import {useI18n} from './i18n';
import './media-message.css';

function Image({value}:{value:MediaImage}) {
 const {t}=useI18n(),[url,setURL]=useState(''),[failed,setFailed]=useState(false),[open,setOpen]=useState(false),[full,setFull]=useState(''),[fullFailed,setFullFailed]=useState(false),dialog=useRef<HTMLDialogElement>(null);
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
 return <div className="media-image">
  <button className="media-thumbnail" type="button" disabled={!url||failed} onClick={()=>setOpen(true)} aria-label={t('chat.mediaOpen',{name:value.name})}>
   {url&&!failed?<img src={url} alt={value.name} onError={()=>setFailed(true)}/>:<span role="status">{failed?t('chat.mediaUnavailable'):t('common.loading')}</span>}
  </button>
  <div className="media-name" title={value.name}>{value.name}</div>
  <dialog className="screen-viewer" ref={dialog} aria-label={value.name} onCancel={()=>setOpen(false)} onClick={e=>{if(e.target===e.currentTarget)setOpen(false)}}>
   <div className="screen-viewer-toolbar"><span>{value.name}</span><button type="button" onClick={()=>setOpen(false)} aria-label={t('common.close')}>×</button></div>
   {full&&!fullFailed?<img className="screen-full-image" src={full} alt={value.name} onError={()=>setFullFailed(true)}/>:<p role="status">{fullFailed?t('chat.mediaUnavailable'):t('common.loading')}</p>}
  </dialog>
 </div>;
}
export function MediaMessage({value,note}:{value:MediaPresentation;note:string}) {
 return <div className="media-message">
  {note&&<p className="media-caption">{note}</p>}
  <div className="media-grid">{value.images.map(image=><Image key={image.id} value={image}/>)}</div>
 </div>;
}
