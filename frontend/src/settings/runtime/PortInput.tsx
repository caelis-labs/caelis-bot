import {useId,useEffect,useState} from 'react';
import {useI18n} from '../../i18n';
import {validPort} from './bot-address';
export function PortInput({value,onChange,disabled,placeholder,required=false}:{value:string;onChange:(v:string)=>void;disabled?:boolean;placeholder?:string;required?:boolean}){
 const {t}=useI18n(),id=useId(),invalid=!!value&&!validPort(value);
 return <><input inputMode="numeric" pattern="[0-9]+" value={value} placeholder={placeholder} required={required} disabled={disabled} aria-invalid={invalid||undefined} aria-describedby={invalid?id:undefined} onChange={e=>{const next=e.target.value;e.target.setCustomValidity(next&&!validPort(next)?t('settings.portRange'): '');onChange(next)}}/>{invalid&&<small id={id} className="remote-field-error">{t('settings.portRange')}</small>}</>;
}

// Keep the typed string until submission; numeric model conversion must not
// replace invalid input with NaN or silently turn zero into a default port.
export function NumericPortInput({value,defaultValue,onChange,...props}:{value?:number;defaultValue?:number;onChange:(v:number|undefined)=>void;disabled?:boolean;placeholder?:string;required?:boolean}){
 const format=(v?:number)=>v===defaultValue?'':String(v??'');
 const [text,setText]=useState(()=>format(value));
 useEffect(()=>{if(!Object.is(value,text===''?defaultValue:Number(text)))setText(format(value))},[value,defaultValue,text]);
 return <PortInput {...props} value={text} onChange={v=>{setText(v);onChange(v===''?defaultValue:Number(v))}}/>;
}
