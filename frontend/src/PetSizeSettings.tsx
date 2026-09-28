import {useEffect,useRef,useState} from 'react';
import {desktop,type Placement} from './desktop';
import {useI18n} from './i18n';
import {SettingRow} from './SettingsUI';

export function PetSizeSettings() {
 const {t,number}=useI18n();
 const [scale,setScale]=useState<number|null>(null),[error,setError]=useState<'settings.sizeLoadFailed'|'settings.sizeSaveFailed'|''>('');
 const pending=useRef<number|null>(null),pumping=useRef(false),save=useRef(false),dragging=useRef(false);
 useEffect(()=>{
  void desktop<Placement>('Placement').then(p=>setScale(p.scale)).catch(()=>setError('settings.sizeLoadFailed'));

 },[]);
 // At most one geometry request is in flight. Coalesce motion, then save the
 // final value on release; late bridge responses cannot rewind the thumb.
 const flush=async()=>{
  if(pumping.current)return;
  pumping.current=true;
  try {
   while(pending.current!==null||save.current){
    if(pending.current!==null){const value=pending.current;pending.current=null;await desktop('PreviewScale',value);}
    else {save.current=false;await desktop('CommitScale');}
   }
  }catch{pending.current=null;save.current=false;setError('settings.sizeSaveFailed');}
  finally{pumping.current=false;}
 };
 const commit=()=>{dragging.current=false;save.current=true;void flush();};

 return <>
   <SettingRow label={t('settings.petSize')} htmlFor="pet-size"><div className="size-control">
    <input id="pet-size" type="range" min="0.65" max="1.6" step="any" disabled={scale===null} value={scale??1} aria-valuetext={scale===null?'':number(scale,{style:'percent',maximumFractionDigits:0})} onPointerDown={()=>{dragging.current=true;}} onChange={event=>{const value=event.target.valueAsNumber;setScale(value);pending.current=value;save.current=!dragging.current;setError('');void flush();}} onPointerUp={commit} onPointerCancel={commit} onBlur={commit}/>
    <output htmlFor="pet-size">{scale===null?'—':number(scale,{style:'percent',maximumFractionDigits:0})}</output>
   </div></SettingRow>


  {error&&<p role="alert" className="inline-error">{t(error)}</p>}
 </>;
}
