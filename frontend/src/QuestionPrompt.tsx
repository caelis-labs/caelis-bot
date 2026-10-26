import {useId,useRef,useState,useEffect} from 'react';
import type {QuestionCard} from './backend/contract';
import {SetupButton} from './SetupLoading';
import {backend} from './desktop';
import {useI18n} from './i18n';
import {questionAnswered,questionStatusKey,questionCopy,optionWords,displayUnits} from './question-presentation';

export function QuestionPrompt({card,refresh,onAnswered}:{card:QuestionCard;refresh:()=>void;onAnswered:(card:QuestionCard)=>void}) {
 const {t,locale}=useI18n();
 const id=useId();
 const [value,setValue]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const [settled,setSettled]=useState<{requestID:string;state:string}>();
 const inFlight=useRef(false);
 const state=card.state||(settled&&card.rejectedReplyId!==settled.requestID?settled.state:''),done=!!state;
 useEffect(()=>{if(settled&&card.rejectedReplyId===settled.requestID)setSettled(undefined);},[card.rejectedReplyId,settled]);
 const copy=questionCopy(card.title,card.context);
 const act=async(kind:'answer'|'dismiss',answer='')=>{
  if(inFlight.current||busy||done||(kind==='answer'&&!answer.trim()))return;
  inFlight.current=true;setBusy(true);setError('');setSettled(undefined);
  const requestID=crypto.randomUUID();
  try {
   const feedback=kind==='answer'
    ?await backend<string>('AnswerQuestion',card.shortId,card.binding,answer,requestID)
    :await backend<string>('DismissQuestion',card.shortId,card.binding,requestID);
   if(kind==='answer'&&(feedback==='回答已提交，等待后续回复。'||feedback==='回答已提交，不会重复发送。')){setSettled({requestID,state:'answered'});onAnswered(card);}
   else if(kind==='dismiss'&&feedback==='问题已关闭，未提供答案。')setSettled({requestID,state:'dismissed'});
   else if(feedback.includes('待核对')||feedback.includes('不会自动重发')||feedback.includes('投递后'))setSettled({requestID,state:'unknown'});
   else setError(t('chat.replyRejected'));
   refresh();
  } catch {setSettled({requestID,state:'unknown'});}
  finally {inFlight.current=false;setBusy(false);}
 };
 if(questionAnswered(state))return null;
 if(done)return <p className="question-result" role="status"><span>{card.title}</span><small>{t(questionStatusKey(state))}</small></p>;
 return <section data-question-pending className="question-prompt" aria-labelledby={id+'-title'} aria-busy={busy} onKeyDown={event=>{
  // macOS WebKit may skip buttons in native Tab navigation. Keep the card's
  // visible controls in order, without trapping focus at either boundary.
  if(event.key!=='Tab')return;
  const controls=Array.from(event.currentTarget.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled)'));
  const index=controls.indexOf(document.activeElement as HTMLElement),next=index+(event.shiftKey?-1:1);
  if(index>=0&&next>=0&&next<controls.length){event.preventDefault();controls[next].focus();}
 }}>
  <div className="question-topline"><div className="question-context"><span className="question-source" title={card.taskTitle}>{card.taskTitle||t('chat.questionConversation')}</span>{!!card.expiresAt&&<><span aria-hidden="true"> · </span><time dateTime={new Date(card.expiresAt*1000).toISOString()}>{t('chat.questionUntil',{time:new Intl.DateTimeFormat(locale,{month:'numeric',day:'numeric',hour:'2-digit',minute:'2-digit'}).format(card.expiresAt*1000)})}</time></>}</div><button type="button" className="question-dismiss" disabled={busy} aria-label={t('chat.questionDismissLabel')} onClick={()=>void act('dismiss')}>{t('chat.questionDismiss')}</button></div>
  <h3 id={id+'-title'} className={"question-prompt-title"+(displayUnits(copy.title)>80?" question-long-title":"")}>{copy.title}</h3>
  {copy.context&&<p className="question-background">{copy.context}</p>}
  {!!card.options?.length&&<div className="question-prompt-options" aria-label={card.title} onKeyDown={event=>{
   if(!['ArrowDown','ArrowUp','ArrowRight','ArrowLeft','Home','End'].includes(event.key))return;
   const buttons=Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'));
   const current=buttons.indexOf(document.activeElement as HTMLButtonElement);if(current<0||!buttons.length)return;
   event.preventDefault();buttons[event.key==='Home'?0:event.key==='End'?buttons.length-1:(current+(['ArrowDown','ArrowRight'].includes(event.key)?1:-1)+buttons.length)%buttons.length]?.focus();
  }}>{card.options.map((option,index)=><button type="button" key={index} aria-label={option} disabled={busy} className={displayUnits(option)>28?"question-long-option":undefined} onClick={()=>void act('answer',String(index+1))}>{optionWords(option,locale).map((word,n)=><span key={n}>{word}</span>)}</button>)}</div>}
  <form className="question-prompt-entry" onSubmit={e=>{e.preventDefault();void act('answer',value);}}>
   <input id={id+'-answer'} value={value} disabled={busy} aria-label={t('chat.questionYourAnswer')} placeholder={t('chat.questionPlaceholder')} onChange={e=>setValue(e.target.value)} onKeyDown={e=>{if(e.key==='Enter'&&(e.nativeEvent.isComposing||e.nativeEvent.keyCode===229))e.preventDefault();}}/>
   <SetupButton type="submit" busy={busy} disabled={!value.trim()}>{t('chat.questionSend')}</SetupButton>
  </form>
  {busy&&<p className="question-feedback" role="status">{t('chat.decisionSubmitting')}</p>}
  {error&&<p className="question-feedback error" role="alert">{error}</p>}
 </section>;
}
