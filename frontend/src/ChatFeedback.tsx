import {useState} from 'react';
import {BotAvatar} from './BotAvatar';
import {useI18n} from './i18n';
import {XIcon} from './SettingsIcons';
import {copyFeedback} from './feedback-client';
import type {FeedbackContext} from './feedback';

// Ephemeral presentation only: never enters the conversation store, never
// acknowledges a receipt. Dismissing leaves the operation's authority intact.
export function ChatFeedback({message,onRetry,onDismiss,feedback}:{message:string;onRetry?:()=>void;onDismiss?:()=>void;feedback?:FeedbackContext}) {
 const {t,locale}=useI18n();
 const [dismissed,setDismissed]=useState('');
 const [copied,setCopied]=useState(false),[copyFailed,setCopyFailed]=useState(false);
 const copy=async()=>{try{await copyFeedback({page:'history',...feedback},locale);setCopied(true);setCopyFailed(false);}catch{setCopyFailed(true);}};
 if(!message||dismissed===message)return null;
 return <article className="message-row assistant chat-feedback" data-transient-feedback>
  <BotAvatar/>
  <div className="message assistant">
   <p role="status">{message}</p>
   <div className="chat-feedback-actions">
    {onRetry&&<button type="button" className="text-action" onClick={onRetry}>{t('settings.runtimeTryAgain')}</button>}
    <button type="button" className="text-action" onClick={()=>void copy()}>{t(copied?'chat.feedbackCopied':'chat.copyFeedback')}</button>
    <button type="button" className="text-action" aria-label={t('common.close')} onClick={()=>{setDismissed(message);onDismiss?.();}}><XIcon size={14} aria-hidden="true"/></button>
   </div>
   {copyFailed&&<p role="status">{t('chat.copyFailed')}</p>}
  </div>
 </article>;
}
