import { useEffect, useRef, useState } from 'react';
import { backend, desktop, type Placement } from './desktop';
import { getReviewLabel, Prompt, useConversation } from './Panel';
import { useI18n } from './i18n';
import { approvalTitle } from './approval-presentation';

export function Bubble() {
 const {t,locale} = useI18n();
 const [visible,setVisible]=useState(false),[expanded,setExpanded]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState(''),[index,setIndex]=useState(0);
 const surface=useRef<HTMLDivElement>(null);
 const {snapshot,refresh}=useConversation(visible,true);
 useEffect(()=>{
  const changed=(e:Event)=>setVisible((e as CustomEvent<boolean>).detail), expand=()=>setExpanded(true), collapse=()=>setExpanded(false);
  window.addEventListener('pet-visibility',changed);window.addEventListener('bubble-expand',expand);window.addEventListener('bubble-collapse',collapse);
  const key=(e:KeyboardEvent)=>{if(!e.isComposing&&e.key==='Escape')void desktop('CollapseBubble');};window.addEventListener('keydown',key);
  void desktop<Placement>('Placement').then(p=>setVisible(p.visible));
  return()=>{window.removeEventListener('pet-visibility',changed);window.removeEventListener('bubble-expand',expand);window.removeEventListener('bubble-collapse',collapse);window.removeEventListener('keydown',key);};
 },[]);
 const prompts=snapshot?.approvals.filter(p=>p.status!=='resolved')??[];
 const prompt=prompts[Math.min(index,Math.max(0,prompts.length-1))];
 const output=snapshot?.items.filter(i=>i.kind==='assistant').at(-1);
 const working=!!snapshot?.canInterrupt;
 const review=snapshot?.reviews?.filter(r=>r.status!=='approved').at(-1);
 const reviewText=review&&(review.status!=='inProgress'||working)?getReviewLabel(review.status, t):'';
 const attention=!!prompt||snapshot?.connection==='login'||snapshot?.connection==='offline'||snapshot?.phase==='unknown';
 const terminal=snapshot?.phase==='interrupted'?t('chat.statusInterrupted'):snapshot?.phase==='failed'?t('chat.terminalFailed'):snapshot?.phase==='completed'?t('chat.statusCompleted'):'';
 const content=error||(prompt&&approvalTitle(prompt,locale))||snapshot?.message||((working||!output?.text)&&reviewText)||output?.text||reviewText||(working?t('chat.bubbleThinking'):terminal);
 const wanted=(!snapshot?.quiet||!!error||attention)&&!!content&&(working||attention||!snapshot?.previewDismissed);
 useEffect(()=>{void desktop('SetBubbleVisible',wanted);},[wanted]);
 useEffect(()=>{if(!prompt&&expanded)void desktop('CollapseBubble');},[prompt?.id,expanded]);
 useEffect(()=>{const resize=new ResizeObserver(()=>{if(surface.current)void desktop('SetBubbleHeight',Math.max(68,Math.min(480,Math.ceil(surface.current.getBoundingClientRect().height))));});resize.observe(surface.current!);return()=>resize.disconnect();},[]);
 const open=()=>void desktop(prompt?'OpenApproval':'OpenHistory');
 const action=async(method:string,...args:unknown[])=>{setBusy(true);setError('');try{await backend(method,...args);await refresh();}catch(e){setError(e instanceof Error?e.message:t('chat.actionFailed'));}finally{setBusy(false);}};
 const actionText=prompt?t('chat.bubbleReviewAndDecide'):t('chat.bubbleOpenChat');
 return <div ref={surface} className="bubble-shell"><main className={`message-bubble ${expanded&&prompt?'expanded':''}`} aria-label={t('chat.bubbleAriaLabel')}>
  <div className="bubble-summary">
   <button className="bubble-message" onClick={open} aria-label={t('chat.bubbleSummaryAria',{content,action:actionText})}>
    <span className="bubble-copy" role="status">{content}</span>
   </button>
   <div className="bubble-actions">
    {working&&<button className="bubble-action" disabled={busy} onClick={()=>void action('Interrupt')} aria-label={t('chat.stopWork')}><span className="stop-symbol"/></button>}
    {!working&&!attention&&snapshot?.previewKey&&<button className="bubble-action" disabled={busy} onClick={()=>void action('DismissPreview',snapshot.previewKey)} aria-label={t('chat.dismissPreview')}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="m4 10 4 4 8-9"/></svg></button>}
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
