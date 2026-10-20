import {useState} from 'react';
import {BotAvatar} from './BotAvatar';
import {useI18n} from './i18n';
import {XIcon} from './SettingsIcons';

// Ephemeral presentation only: never enters the conversation store, never
// acknowledges a receipt. Dismissing leaves the operation's authority intact.
export function ChatFeedback({message,onRetry,onDismiss}:{message:string;onRetry?:()=>void;onDismiss?:()=>void}) {
 const {t}=useI18n();
 const [dismissed,setDismissed]=useState('');
 if(!message||dismissed===message)return null;
 return <article className="message-row assistant chat-feedback" data-transient-feedback>
  <BotAvatar/>
  <div className="message assistant">
   <p role="status">{message}</p>
   <div className="chat-feedback-actions">
    {onRetry&&<button type="button" className="text-action" onClick={onRetry}>{t('settings.runtimeTryAgain')}</button>}
    <button type="button" className="text-action" aria-label={t('common.close')} onClick={()=>{setDismissed(message);onDismiss?.();}}><XIcon size={14} aria-hidden="true"/></button>
   </div>
  </div>
 </article>;
}
