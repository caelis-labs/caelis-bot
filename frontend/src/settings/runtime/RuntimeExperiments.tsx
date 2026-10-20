import {useEffect,useRef,useState} from 'react';
import {backend} from '../../desktop';
import type {RuntimeExperiments as View} from '../../backend/contract';
import {SettingRow} from '../../SettingsUI';
import {useI18n} from '../../i18n';

const labels={
 async_compact:'experimentalAsync',
 mcp_catalog_refresh:'experimentalRefresh',
 deferred_tool_retirement:'experimentalRetirement',
} as const;

export function RuntimeExperiments(){
 const {t}=useI18n();
 const [value,setValue]=useState<View|null>(null);
 const [busy,setBusy]=useState(false),[error,setError]=useState(false),[notice,setNotice]=useState('');
 const generation=useRef(0),working=useRef(false),polls=useRef(0);
 const adopt=(next:View,announce:boolean)=>{
  setValue(next);setError(false);
  if(announce||next.saveState==='pending')setNotice(next.saveState);
 };
 const read=async(announce=false)=>{
  if(working.current)return;
  const seq=++generation.current;
  setBusy(true);
  try{
   const next=await backend<View>('RuntimeExperiments');
   if(seq===generation.current)adopt(next,announce);
  }catch{
   if(seq===generation.current)setError(true);
  }finally{
   if(seq===generation.current)setBusy(false);
  }
 };
 useEffect(()=>{void read();return()=>{generation.current++}},[]);
 useEffect(()=>{
  if(value?.saveState!=='pending'||busy||polls.current>=3)return;
  // Read the existing save flow; never repeat the mutation after a lost reply.
  const timer=setTimeout(()=>{polls.current++;void read(true)},1200);
  return()=>clearTimeout(timer);
 },[value,busy]);
 const save=async(id:string,enabled:boolean)=>{
  if(!value||working.current||value.saveState==='pending')return;
  const seq=++generation.current;
  working.current=true;setBusy(true);setError(false);setNotice('');polls.current=0;
  try{
   const next=await backend<View>('ChangeRuntimeExperiments',{id,enabled,expectedRevision:value.revision});
   if(seq===generation.current)adopt(next,true);
  }catch{
   if(seq===generation.current){setError(true);setNotice('')}
  }finally{
   working.current=false;
   if(seq===generation.current)setBusy(false);
  }
 };
 const note=error?'experimentalFailed':
  busy?(working.current?'experimentalSaving':'experimentalLoading'):
  notice==='pending'?'experimentalPending':notice==='saved'?'experimentalSaved':
  notice==='conflicted'||notice==='superseded'?'experimentalChanged':
  notice==='rejected'?'experimentalRejected':'';
 return <section className="runtime-experiments" aria-labelledby="runtime-experiments-title" aria-busy={busy}>
  <header>
   <h3 id="runtime-experiments-title">{t('settings.experimentalTitle')}</h3>
   <p className="settings-note">{t('settings.experimentalScope')}</p>
  </header>
  {value?.supported?value.features.filter(feature=>feature.id in labels).map(feature=>
   <SettingRow key={feature.id} label={t(`settings.${labels[feature.id as keyof typeof labels]}`)} htmlFor={`experiment-${feature.id}`}>
    <input id={`experiment-${feature.id}`} className="settings-switch" role="switch" type="checkbox"
     checked={feature.enabled} disabled={busy||error||value.saveState==='pending'}
     onChange={event=>void save(feature.id,event.target.checked)}/>
   </SettingRow>
  ):value?<p className="settings-note">{t('settings.experimentalUnsupported')}</p>:null}
  {note&&<p className="settings-note" role="status">{t(`settings.${note}`)}</p>}
  {(error||value?.saveState==='pending'||notice==='conflicted'||notice==='superseded'||notice==='rejected')&&
   <button className="text-action" disabled={busy} onClick={()=>{polls.current=0;void read(true)}}>{t('runtime.recheck')}</button>}
 </section>;
}
