import { useEffect, useReducer, useRef, useState } from 'react';
import { backend, desktop, type Placement } from './desktop';
import { getReviewLabel, Prompt, useConversation } from './Panel';
import { useI18n } from './i18n';
import { approvalTitle } from './approval-presentation';
import { bubblePresentation, reduceBubbleNotice } from './bubble-notice';
import { MessageContent } from './MessageContent';
import { chatActivity } from './chat-presentation';
import { activityLabel } from './activity-presentation';

export function Bubble() {
 const {t,locale} = useI18n();
 const [visible,setVisible]=useState(false),[expanded,setExpanded]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState(''),[index,setIndex]=useState(0);
 const surface=useRef<HTMLDivElement>(null);
 const [reading,setReading]=useState(false);
 const leaveTimer=useRef(0);
 const nativeHover=useRef<boolean|null>(null);
 const read=()=>{window.clearTimeout(leaveTimer.current);setReading(true);};
 const unread=()=>{window.clearTimeout(leaveTimer.current);leaveTimer.current=window.setTimeout(()=>setReading(false),180);};
 useEffect(()=>()=>window.clearTimeout(leaveTimer.current),[]);
 const [notice,dispatchNotice]=useReducer(reduceBubbleNotice,null);
 const noticeID=useRef(0);
 const {snapshot,liveReplies,refresh}=useConversation(visible,true);
 useEffect(()=>{
  const receive=(event:Event)=>{
   const detail=(event as CustomEvent<{message:string;pending:boolean}>).detail;
   if(!detail||typeof detail.message!=='string'||typeof detail.pending!=='boolean')return;
   if(!detail.message){dispatchNotice({type:'clearPending'});return;}
   dispatchNotice({type:'show',notice:{id:++noticeID.current,...detail}});
  };
  window.addEventListener('terminal-notice',receive);
  return()=>window.removeEventListener('terminal-notice',receive);
 },[]);
 useEffect(()=>{
  if(!notice||notice.pending)return;
  const timer=window.setTimeout(()=>dispatchNotice({type:'dismiss',id:notice.id}),4000);
  return()=>window.clearTimeout(timer);
 },[notice]);
 useEffect(()=>{
  const changed=(e:Event)=>{setVisible((e as CustomEvent<boolean>).detail);setReading(false);}, expand=()=>setExpanded(true), collapse=()=>{setExpanded(false);setReading(false);};
  const hidden=()=>{window.clearTimeout(leaveTimer.current);if(nativeHover.current!==null)nativeHover.current=false;setReading(false);};
  const hover=(event:Event)=>{
   nativeHover.current=(event as CustomEvent<boolean>).detail;
   if(nativeHover.current)read();else unread();
  };
  window.addEventListener('bubble-hover',hover);
  window.addEventListener('bubble-hidden',hidden);
  window.addEventListener('pet-visibility',changed);window.addEventListener('bubble-expand',expand);window.addEventListener('bubble-collapse',collapse);
  const key=(e:KeyboardEvent)=>{if(!e.isComposing&&e.key==='Escape'){setReading(false);void desktop('CollapseBubble');}};window.addEventListener('keydown',key);
  void desktop<Placement>('Placement').then(p=>setVisible(p.visible));
  return()=>{window.removeEventListener('bubble-hover',hover);window.removeEventListener('bubble-hidden',hidden);window.removeEventListener('pet-visibility',changed);window.removeEventListener('bubble-expand',expand);window.removeEventListener('bubble-collapse',collapse);window.removeEventListener('keydown',key);};
 },[]);
 const prompts=snapshot?.approvals.filter(p=>p.status!=='resolved')??[];
 const prompt=prompts[Math.min(index,Math.max(0,prompts.length-1))];
 const output=snapshot?.items.filter(i=>i.kind==='assistant').at(-1);
 const working=!!snapshot?.canInterrupt;
 const activity=chatActivity(snapshot);
 const dreaming=activity==='dreaming';
 const progress=dreaming?t('chat.dreaming'):activity==='tool'&&snapshot?.activity?activityLabel(snapshot.activity,t):activity==='stopping'?t('chat.stopping'):activity==='reviewing'?t('chat.reviewing'):activity==='thinking'?t('chat.bubbleThinking'):'';
 const review=snapshot?.reviews?.filter(r=>r.owner!=='task'&&r.status!=='approved').at(-1);
 const reviewText=review&&(review.status!=='inProgress'||working)?getReviewLabel(review.status, t):'';
 const attention=!!prompt||snapshot?.connection==='login'||snapshot?.connection==='offline'||snapshot?.phase==='unknown';
 const terminal=snapshot?.phase==='interrupted'?t('chat.statusInterrupted'):snapshot?.phase==='failed'?t('chat.terminalFailed'):snapshot?.phase==='completed'?t('chat.statusCompleted'):'';
 const conversationContent=error||(prompt&&approvalTitle(prompt,locale))||snapshot?.message||((working||!output?.text)&&reviewText)||(dreaming?progress:'')||output?.text||reviewText||progress||(working?t('chat.bubbleThinking'):terminal);
 const conversationWanted=(!snapshot?.quiet||dreaming||!!error||attention)&&!!conversationContent&&(working||attention||!snapshot?.previewDismissed);
 const {content,wanted,showNotice}=bubblePresentation(conversationContent,conversationWanted,attention||!!error,notice);
 const markdown=!showNotice&&!error&&!prompt&&!snapshot?.message&&!reviewText&&!!output?.text&&content===output.text;
 const progressText=!showNotice&&!attention&&!error&&content!==progress?progress:'';
 const readingMessage=reading&&!prompt;
 useEffect(()=>{void desktop('SetBubbleVisible',wanted);},[wanted]);
 useEffect(()=>{if(!prompt&&expanded)void desktop('CollapseBubble');},[prompt?.id,expanded]);
 useEffect(()=>{const resize=new ResizeObserver(()=>{if(surface.current)void desktop('SetBubbleHeight',Math.max(68,Math.min(480,Math.ceil(surface.current.getBoundingClientRect().height))));});resize.observe(surface.current!);return()=>resize.disconnect();},[]);
 const open=()=>void desktop(showNotice?'ToggleTaskDock':prompt?'OpenApproval':'OpenHistory');
 const action=async(method:string,...args:unknown[])=>{setBusy(true);setError('');try{await backend(method,...args);await refresh();}catch(e){setError(e instanceof Error?e.message:t('chat.actionFailed'));}finally{setBusy(false);}};
 const actionText=showNotice?t('chat.bubbleOpenTasks'):prompt?t('chat.bubbleReviewAndDecide'):t('chat.bubbleOpenChat');
 return <div ref={surface} className="bubble-shell" onMouseEnter={()=>{if(nativeHover.current===null)read();}} onMouseLeave={()=>{if(nativeHover.current===null)unread();}} onFocusCapture={read} onBlurCapture={e=>{if(!nativeHover.current&&!e.currentTarget.contains(e.relatedTarget))unread();}}><main className={`message-bubble ${expanded&&prompt?'expanded':''} ${readingMessage?'reading':''}`} aria-label={t('chat.bubbleAriaLabel')} title={dreaming?t('chat.dreamHint'):undefined}>
  <div className="bubble-summary">
   <div className="bubble-message" onClick={e=>{
    if((e.target as Element).closest('button,a,input,select,textarea')||window.getSelection()?.toString())return;
    open();
   }}>
    {!!progressText&&<div className="bubble-progress" role="status"><span className="activity-spinner" aria-hidden="true"/><span>{progressText}</span></div>}
    <div className="bubble-copy" tabIndex={readingMessage?0:undefined} role="region" aria-label={t('chat.bubbleAriaLabel')}>
     {/* Temporary prompts keep the reply mounted so its reveal progress survives. */}
     {output&&<div hidden={!markdown}><MessageContent key={output.id} text={output.text} report={setError} animate={visible&&liveReplies.has(output.id)}/></div>}
     {!markdown&&<span className={content===progress?'bubble-standalone-progress':undefined}>{content===progress&&!!progress&&!dreaming&&<span className="activity-spinner" aria-hidden="true"/>}{content}</span>}
    </div>
   </div>
   <div className="bubble-actions">
    {!showNotice&&working&&<button className="bubble-action" disabled={busy} onClick={()=>void action('Interrupt')} aria-label={t('chat.stopWork')}><span className="stop-symbol"/></button>}
    {(showNotice||(!working&&!attention&&snapshot?.previewKey))&&<button className="bubble-action" disabled={!showNotice&&busy} onClick={()=>showNotice&&notice?dispatchNotice({type:'dismiss',id:notice.id}):void action('DismissPreview',snapshot!.previewKey)} aria-label={t('chat.dismissPreview')}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="m4 10 4 4 8-9"/></svg></button>}
    <button className="bubble-action" onClick={open} aria-label={actionText}><svg viewBox="0 0 20 20" aria-hidden="true"><path d={prompt?'M4 10h12m-5-5 5 5-5 5':'M5 15 15 5M6 5h9v9'}/></svg></button>
   </div>
  </div>
  {expanded&&prompt&&<div className="bubble-decision">
   {prompts.length>1&&<div className="event-pagination"><span>{Math.min(index+1,prompts.length)} / {prompts.length}</span><button className="text-action" onClick={()=>setIndex((index+1)%prompts.length)}>{t('chat.nextApproval')}</button></div>}
   <Prompt key={prompt.id} value={prompt} refresh={()=>void refresh()}/>
   <button className="text-action" onClick={()=>void desktop('OpenHistory')}>{t('chat.viewInChat')}</button>
  </div>}
 </main></div>;
}
