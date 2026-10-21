import {useEffect,useState} from 'react';
import {CopyIcon} from '@phosphor-icons/react/dist/csr/Copy';
import {CheckIcon} from '../../SettingsIcons';
import {desktop} from '../../desktop';
import {useI18n} from '../../i18n';

// Only the user-facing challenge is copied, never the provider's device secret.
export function DeviceCode({code,host=desktop}:{code:string;host?:typeof desktop}) {
 const {t}=useI18n(),[copied,setCopied]=useState(false),[failed,setFailed]=useState(false);
 useEffect(()=>{setCopied(false);setFailed(false)},[code]);
 const copy=async()=>{try{await host('CopyText',code);setCopied(true);setFailed(false)}catch{setFailed(true)}};
 return <div className="runtime-device-code"><span>{t('connections.deviceCode')}</span>
  <div className="codex-user-code"><code>{code}</code><button type="button" aria-label={t(copied?'chat.feedbackCopied':'runtime.deviceCopyCode')} title={t(copied?'chat.feedbackCopied':'runtime.deviceCopyCode')} onClick={()=>void copy()}>{copied?<CheckIcon size={18}/>:<CopyIcon size={18}/>}</button></div>
  {failed&&<small role="status">{t('runtime.deviceCopyFailed')}</small>}
 </div>;
}
