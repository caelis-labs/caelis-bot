import {useEffect,useRef,useState} from 'react';
import {backend} from './desktop';
import type {WorkURLPage,WorkURLReceipt} from './backend/contract';
import {SettingsDialog} from './settings/runtime/SettingsDialog';
import {useI18n} from './i18n';

// This shared desktop/Web surface is user-only. No page HTML, scripts, hidden
// fields or browser credentials execute in the Bot origin. Only operation IDs
// are retained across reload; entered values never enter storage or chat.
export function ApprovalPage({id,onClose}:{id:string;onClose:()=>void}){
 const {t}=useI18n();
 const key='caelisbot.url-submit:'+id;
 const [page,setPage]=useState<WorkURLPage|null>(null),[values,setValues]=useState<Record<string,string>>({}),[receipt,setReceipt]=useState<WorkURLReceipt|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const inFlight=useRef(false),operation=useRef('');
 const retain=(value:string)=>{operation.current=value;try{if(value)sessionStorage.setItem(key,value);else sessionStorage.removeItem(key)}catch{/* Original owner also retains ID/digest; no automatic replay. */}};
 useEffect(()=>{
  let active=true;
  try{operation.current=sessionStorage.getItem(key)||''}catch{/* Safe receipt read only. */}
  void backend<WorkURLPage>('ApprovalPage',id).then(next=>{
   if(!active)return;setPage(next);setValues(Object.fromEntries((next.fields??[]).map(f=>[f.name,f.value])));
   if(next.operation){retain(next.operation.requestId);setReceipt(next.operation)}
  }).catch(cause=>{if(active)setError(t(String(cause).includes('worker_url_page_unsupported')?'chat.urlPageUnsupported':'chat.urlPageUnavailable'))});
  const original=operation.current;
  if(original)void backend<WorkURLReceipt>('ApprovalPageOperation',id,original).then(next=>{if(active&&operation.current===original)setReceipt(next)}).catch(()=>{if(active&&operation.current===original)setReceipt({requestId:original,fingerprint:'',outcome:'unknown'})});
  return()=>{active=false};
 },[id]);
 const inspect=async()=>{
  if(inFlight.current||!operation.current)return;inFlight.current=true;setBusy(true);
  try{setReceipt(await backend<WorkURLReceipt>('ApprovalPageOperation',id,operation.current));setError('')}
  catch{setError(t('chat.urlPageUnknown'))}
  finally{inFlight.current=false;setBusy(false)}
 };
 const submit=async()=>{
  if(!page||inFlight.current||receipt&&receipt.outcome!=='rejected')return;
  inFlight.current=true;setBusy(true);setError('');
  const request=crypto.randomUUID();retain(request);
  try{
   const next=await backend<WorkURLReceipt>('SubmitApprovalPage',id,request,page.revision,values);
   setReceipt(next);
   if(next.outcome==='rejected')retain('');
  }catch{
   setReceipt({requestId:request,fingerprint:'',outcome:'unknown'});setError(t('chat.urlPageUnknown'));
  }finally{setValues({});inFlight.current=false;setBusy(false)}
 };
 const pending=!!receipt&&receipt.outcome!=='rejected';
 return <SettingsDialog title={t('chat.urlPageTitle')} onClose={onClose} busy={busy} portalTarget={document.body} className="worker-url-dialog">
  {page&&<><p>{page.origin}</p>{page.title&&<h3>{page.title}</h3>}{page.text&&<p>{page.text}</p>}<p>{t('chat.urlPageConsentSeparate')}</p></>}
  {error&&<p role="status">{error}</p>}
  {receipt&&<p role="status">{t(receipt.outcome==='accepted'?'chat.urlPageSubmitted':receipt.outcome==='rejected'?'chat.urlPageRejected':'chat.urlPageUnknown')}</p>}
  {page&&!pending&&<form className="worker-url-form" onSubmit={e=>{e.preventDefault();void submit()}}>
   {(page.fields??[]).map(f=><label key={f.name}>{f.label}{f.required?' *':''}{f.type==='select'?<select name={f.name} disabled={busy} value={values[f.name]??''} onChange={e=>setValues({...values,[f.name]:e.target.value})}>{(f.options??[]).map(o=><option key={o.value} value={o.value}>{o.label}</option>)}</select>:<input name={f.name} type={f.type} required={f.required} maxLength={4096} autoComplete="off" disabled={busy} value={values[f.name]??''} onChange={e=>setValues({...values,[f.name]:e.target.value})}/>}</label>)}
   <button type="submit" disabled={busy||!page.revision}>{page.submitLabel||t('chat.urlPageSubmit')}</button>
  </form>}
  {pending&&receipt.outcome!=='accepted'&&<button type="button" disabled={busy} onClick={()=>void inspect()}>{t('chat.urlPageCheck')}</button>}
 </SettingsDialog>;
}
