import {isWebSurface} from './transport';
import {QuestionPrompt} from './QuestionPrompt';
import {questionIdentity,visibleQuestionMessages} from './question-presentation';
import {Recovery} from './SurfaceBoundary';
import {ChatFeedback} from './ChatFeedback';
import { surfaceReady, surfaceMounted } from './surface-ready';
import { useConnectionNotice } from './use-connection-notice';
import { memo, useCallback, useEffectEvent, useEffect, useLayoutEffect, useMemo, useRef, useState, type ClipboardEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { backend, desktop, type DraftFile, type PasteResult } from './desktop';
import type { Approval, ChatUpdate, Decision, Draft, Item, QuotedMessage, Receipt, Snapshot, Submission, QuestionCard } from './backend/contract';
import { handleComposerKey } from './composer-keyboard';
import { MessageContent } from './MessageContent';
import { ArtifactLink } from './ArtifactLink';
import { chatTimeline, chatTimeLabel, type ChatTimeAnchor } from './chat-time';
import { canSubmit, chatActivity, composerAction, incompleteAssistant, liveReplyIDs, withOutgoing } from './chat-presentation';
import { WorkingMessage } from './WorkingMessage';
import { BotAvatar } from './BotAvatar';
import { useAvatarPresentation } from './use-avatar-presentation';
import { animatedReplyID, type PortraitClip } from './avatar-presentation';
import { SettingsDialog } from './settings/runtime/SettingsDialog';
import { AttachmentMenu } from './AttachmentMenu';
import { botMenuPlugins, nativeMenuPlugins } from './attachment-menu-model';
import { ScreenMessage } from './ScreenMessage';
import { MediaMessage } from './MediaMessage';
import { ChatScroll } from './chat-scroll';
import { ConversationOrder, FileObservationOrder, SubmissionProgress, DraftQueue, acceptedDraftSettled, visibleDraftFiles, readDraftAndFiles, retryRead, sameComposerSnapshot } from './chat-observation';
import { useI18n } from './i18n';
import { approvalChoice, approvalResult, approvalText, approvalTitle } from './approval-presentation';
import { CheckIcon, XIcon } from './SettingsIcons';
import { CaretDownIcon } from '@phosphor-icons/react/dist/csr/CaretDown';
import { CaretLeftIcon } from '@phosphor-icons/react/dist/csr/CaretLeft';
import { CaretRightIcon } from '@phosphor-icons/react/dist/csr/CaretRight';
import { CaretUpIcon } from '@phosphor-icons/react/dist/csr/CaretUp';
import type { MessageKey } from './i18n/catalogs';

function Icon({ name }: { name: string }) { return <img className="symbol" src={`/icons/${name}.png`} alt="" />; }

function DraftThumbnail({file,returnFocus}:{file:DraftFile;returnFocus:HTMLTextAreaElement|null}) {
 const {t}=useI18n();
 const [url,setURL]=useState(''),[open,setOpen]=useState(false),[full,setFull]=useState(''),[failed,setFailed]=useState(false),[actualSize,setActualSize]=useState(false);
 useEffect(()=>{const close=()=>setOpen(false);window.addEventListener('history-close',close);return()=>window.removeEventListener('history-close',close);},[]);
 useEffect(()=>{
  let active=true;setURL('');
  if(!file.image||file.unavailable)return;
  void desktop<string>('DraftImage',file.id).then(value=>{if(active&&/^data:image\/(png|jpeg|webp);base64,/.test(value))setURL(value)}).catch(()=>{});
  return()=>{active=false};
 },[file.id,file.image,file.unavailable]);
 useEffect(()=>{
  let current=true;setFull('');setFailed(false);setActualSize(false);
  if(open)void desktop<string>('DraftImagePreview',file.id).then(value=>{
   if(!current)return;
   if(/^data:image\/(png|jpeg|gif|webp);base64,/.test(value))setFull(value);else setFailed(true);
  }).catch(()=>{if(current)setFailed(true)});
  return()=>{current=false};
 },[open,file.id]);
 const copy=<>{url?<img className="draft-thumbnail" src={url} alt=""/>:<Icon name="paperclip"/>}<span className="attachment-details"><strong>{file.name}</strong><small>{file.unavailable?t('chat.mediaUnavailable'):file.type}</small></span></>;
 return <>{url?<button className="attachment-open" type="button" aria-label={t('chat.mediaOpen',{name:file.name})} onClick={()=>setOpen(true)}>{copy}</button>:<span className="attachment-open">{copy}</span>}
  {open&&<SettingsDialog portalTarget={document.body} returnFocus={returnFocus} title={file.name} className="draft-image-viewer" onClose={()=>setOpen(false)}><div className="draft-image-toolbar">{full&&!failed&&<button type="button" aria-pressed={actualSize} onClick={()=>setActualSize(value=>!value)}>{t(actualSize?'chat.imageFit':'chat.imageActualSize')}</button>}</div>{full&&!failed?<div className={`draft-image-canvas${actualSize?' actual-size':''}`}><img src={full} alt={file.name} onError={()=>setFailed(true)}/></div>:<p role="status">{t(failed?'chat.mediaUnavailable':'common.loading')}</p>}</SettingsDialog>}
 </>;
}

// Arrow keys move focus only. No option is selected or dispatched by navigation.
function decisionKeys(event:React.KeyboardEvent<HTMLElement>) {
 if(event.nativeEvent.isComposing||!['ArrowDown','ArrowUp','ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;
 const buttons=[...event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')];
 const current=buttons.indexOf(document.activeElement as HTMLButtonElement);
 if(current<0||!buttons.length)return;
 event.preventDefault();
 const next=event.key==='Home'?0:event.key==='End'?buttons.length-1:['ArrowDown','ArrowRight'].includes(event.key)?(current+1)%buttons.length:(current-1+buttons.length)%buttons.length;
 buttons[next].focus();
}

export function getItemStatusLabel(status: string, t: (key: MessageKey) => string): string {
 switch (status) {
  case 'working': return t('chat.statusWorking');
  case 'sending': return t('chat.statusSending');
  case 'attention': return t('chat.statusAttention');
  case 'interrupting': return t('chat.statusInterrupting');
  case 'completed': return t('chat.statusCompleted');
  case 'interrupted': return t('chat.statusInterrupted');
  case 'failed': return t('chat.statusFailed');
  case 'unknown': return t('chat.statusUnknown');
  case 'unconfirmed': return t('chat.statusUnconfirmed');
  case 'inProgress': return t('chat.statusInProgress');
  case 'incomplete': return t('chat.statusIncomplete');
  case 'declined': return t('chat.statusDeclined');
  case 'rejected': return t('chat.statusDeclined');
  default: return '';
 }
}

export function Prompt({ value, refresh }: { value: Approval; refresh: () => void }) {
  const {t,locale} = useI18n();
  const title = approvalTitle(value,locale);
  const resultLabel=approvalResult(value,locale);
  const [answers, setAnswers] = useState<Record<string,string[]>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const enabled = value.status === 'pending' && !busy;
  const decide = async (choice: string) => {
    setBusy(true); setError('');
    try { await backend('Decide', { id: value.id, choice, answers } satisfies Decision); }
    catch (e) { setError(e instanceof Error ? e.message : t('chat.approvalFailed')); }
    finally { setBusy(false); refresh(); }
  };
  const button = (c: typeof value.choices[number]) => <button key={c.id} className={c.scope === 'once' || c.scope === 'allow_once' || c.id === 'allow' || c.id === 'answer' || c.id === 'accept' ? 'primary-decision' : ''} disabled={!enabled} onClick={() => void decide(c.id)}>{approvalChoice(c,locale)}</button>;
  return <section className="approval" aria-label={title}>
    <p className="event-eyebrow">{t('chat.approvalEyebrow')}</p>
    <h3>{title}</h3>
    {value.taskTitle && <p>{t('chat.approvalTask',{name:value.taskTitle})}</p>}
    {value.noticeKey && <p>{approvalText(locale,value.noticeKey,'')}</p>}
    {value.description && <p>{value.description}</p>}
    {value.action && value.action !== title && <pre className="approval-action">{value.action}</pre>}
    {value.target && <p className="approval-target"><span>{t('chat.approvalTarget')}</span>{value.target}</p>}
    {value.details && <details open={!value.action}><summary>{t('chat.approvalDetails')}</summary><pre>{value.details}</pre></details>}
    {value.sections?.map((section,index)=><details key={index} open={!value.action}><summary>{approvalText(locale,section.titleKey,t('chat.approvalDetails'))}</summary><pre>{section.text}</pre></details>)}
    {value.url && <button className="text-action" disabled={!enabled} onClick={() => void backend('OpenApprovalURL',value.id).catch(() => setError(t('chat.approvalLinkExpired')))}>{t('chat.approvalOpenUrl')}</button>}
    {(value.questions ?? []).map(q => {
      const options = q.options.length>0?q.options:q.type==='boolean'?[{id:'true',label:t('chat.yes')},{id:'false',label:t('chat.no')}]:[];
      const current = answers[q.id]??[];
      const freeText = q.type==='text' || options.length===0;
      const custom = current.find(answer=>!options.some(option=>option.id===answer))??'';
      return <div className="question" key={q.id}><label htmlFor={freeText?`question-${q.id}`:undefined}>{q.title}{q.required && <span aria-label={t('chat.required')}> *</span>}</label>
        {options.length>0&&<div className="question-options">{options.map(o => <button type="button" key={o.id} disabled={!enabled} aria-pressed={current.includes(o.id)} onClick={() => setAnswers({ ...answers, [q.id]: q.multiple ? current.includes(o.id)?current.filter(id=>id!==o.id):[...current,o.id] : [o.id] })}>{o.label}</button>)}</div>}
        {freeText&&<input id={`question-${q.id}`} type={q.secret ? 'password' : q.type === 'number' || q.type === 'integer' ? 'number' : 'text'} disabled={!enabled} value={custom} onChange={e => { const selected=q.multiple?current.filter(answer=>options.some(option=>option.id===answer)):[]; setAnswers({ ...answers, [q.id]:e.target.value?[...selected,e.target.value]:selected }); }} />}
      </div>;
    })}
    {error && <Recovery message={error} onRetry={()=>{setError('');void refresh();}}/>}
    {value.status === 'pending' ? <>
      <div className="approval-choices" onKeyDown={decisionKeys}>{value.choices.map(c => c.scope === 'rule' || c.scope === 'conversation' ?
        <div key={c.id} className="permission-option"><p>{c.scope === 'rule' ? t('chat.approvalRuleScope') : t('chat.approvalConversationScope')}</p>{c.details && <pre>{c.details}</pre>}{button(c)}</div> : button(c))}</div>
    </> : <p className="quiet approval-result" role="status" aria-label={resultLabel}>{value.status==='resolved'&&value.resolution?.outcome==='allowed'?<CheckIcon aria-hidden="true" size={16}/>:value.status==='resolved'&&(value.resolution?.outcome==='declined'||value.resolution?.outcome==='cancelled')?<XIcon aria-hidden="true" size={16}/>:null}<span>{resultLabel}</span></p>}
  </section>;
}
type MessageProps={ item: Item; report: (message: string) => void; onContextMenu:(item:Item,x:number,y:number)=>void; onQuotePreview:(quote:QuotedMessage)=>void; refresh:()=>void; onQuestionAnswered:(card:QuestionCard)=>void; animate?:boolean; reveal?:boolean; incomplete?:boolean; clip?:PortraitClip };
const Message=memo(function Message({ item, report, onContextMenu, onQuotePreview, refresh, onQuestionAnswered, animate=false, reveal=false, incomplete=false, clip }: MessageProps) {
  const {t} = useI18n();
  const statusLabel = getItemStatusLabel(item.status, t);
  const delivery=item.kind==='user'&&['unknown','rejected'].includes(item.status)?statusLabel:'';
  const asyncCard=item.kind==='assistant'&&/^\[Q[0-9A-Z]+\] /.test(item.text);
  return <article data-message-id={item.id} className={`message-row ${item.kind}${item.questionCards?.length?' question-message':''}`}>
   {(item.kind==='assistant'||item.kind==='controlNotice'||item.kind==='tool')&&<BotAvatar animate={animate} clip={clip}/>}
   <div className={`message ${item.kind==='controlNotice'||item.kind==='tool'?'assistant controlNotice':item.kind}`} onContextMenu={e=>{if(!item.text)return;e.preventDefault();onContextMenu(item,e.clientX,e.clientY);}}>
    {item.quoted?.text&&<button type="button" className="sent-quote" onClick={()=>onQuotePreview(item.quoted!)}><span>{item.quoted.text}</span></button>}
    {item.task?.id ? <button type="button" className="task-message-action" onClick={()=>void desktop('OpenTaskTerminal',item.task!.id).catch(()=>report(t('chat.taskUnavailable')))}>{item.text}</button> : item.kind==='user'&&item.screen ? <ScreenMessage value={item.screen} note={item.text} report={report}/> : item.kind==='user'&&item.media ? <MediaMessage value={item.media} note={item.media.caption??''}/> : item.kind === 'activity' ? <details><summary>{item.text}<span>{statusLabel}</span></summary>{item.details && <pre>{item.details}</pre>}</details> : item.questionCards?.length ? <div className="question-list">{item.questionCards.map(card=><QuestionPrompt key={card.shortId+card.binding} card={card} refresh={refresh} onAnswered={onQuestionAnswered}/>)}</div> : asyncCard ? <p className="question-card-text">{item.text}</p> : item.kind === 'assistant' ? <MessageContent key={item.id} text={item.text} report={report} animate={reveal&&!incomplete}/> : <p>{item.text}</p>}
    {!!item.artifacts?.length&&<div className="artifacts">{item.artifacts.map(file => <ArtifactLink file={file} report={report} key={file.id}/>)}</div>}
    {incomplete&&<small className="assistant-status" role="status">{t('chat.statusIncomplete')}</small>}
    {delivery&&<small className="message-delivery pending" role="status">{delivery}</small>}
   </div>
  </article>;
},(before,after)=>before.report===after.report&&before.onContextMenu===after.onContextMenu&&before.onQuotePreview===after.onQuotePreview&&before.refresh===after.refresh&&before.onQuestionAnswered===after.onQuestionAnswered&&before.animate===after.animate&&before.reveal===after.reveal&&before.incomplete===after.incomplete&&
 (before.animate?before.clip===after.clip:true)&&before.item.id===after.item.id&&before.item.kind===after.item.kind&&
 before.item.text===after.item.text&&before.item.status===after.item.status&&before.item.seenAt===after.item.seenAt&&
 JSON.stringify(before.item.artifacts)===JSON.stringify(after.item.artifacts)&&
 JSON.stringify(before.item.screen)===JSON.stringify(after.item.screen)&&
 JSON.stringify(before.item.media)===JSON.stringify(after.item.media)&&
 JSON.stringify(before.item.quoted)===JSON.stringify(after.item.quoted)&&
 JSON.stringify(before.item.questionCards)===JSON.stringify(after.item.questionCards)&&
 JSON.stringify(before.item.task)===JSON.stringify(after.item.task));

export function useConversation(active: boolean, pet=false, chat=false) {
 const [conversation,setConversation]=useState<{snapshot:Snapshot|null;liveReplies:Set<string>}>({snapshot:null,liveReplies:new Set()});
 const observation=useRef(conversation);
 const order=useRef(new ConversationOrder()),botStatus=useRef(''),observing=useRef(active);observing.current=active;
 const refresh=useCallback(async()=>{
  if(!observing.current)return;
  const ticket=order.current.request(),expectedRevision=order.current.revision;
  let next:Snapshot|null;
  if(chat){const update=await backend<ChatUpdate>('ChatSnapshot',expectedRevision,botStatus.current);next=update.changed?update.snapshot:null;}
  else next=await backend<Snapshot>(pet?'PetSnapshot':'Snapshot');
  if(!next){order.current.accept(ticket,expectedRevision);return;}
  if(observing.current&&order.current.accept(ticket,next.revision)){
   botStatus.current=next.botStatus;
   observation.current={snapshot:next,liveReplies:liveReplyIDs(observation.current.snapshot,next,observation.current.liveReplies,pet)};
   setConversation(observation.current);
  }
 },[pet,chat]);
 useEffect(()=>{
  order.current.reset();
  if(!active)return;
  observation.current={snapshot:null,liveReplies:new Set()};
  botStatus.current='';
  let stopped=false,timer=0;
  const poll=async()=>{try{await refresh();}catch{/* Preserve confirmed state across a failed observation. */}finally{if(!stopped)timer=window.setTimeout(()=>void poll(),450);}};
  void poll();return()=>{stopped=true;clearTimeout(timer);order.current.reset();};
 },[active,pet,chat,refresh]);
 return {...conversation,refresh};
}

function MessageMenu({item,x,y,onQuote,onClose,report}:{item:Item;x:number;y:number;onQuote:(item:Item)=>void;onClose:()=>void;report:(message:string)=>void}) {
 const {t}=useI18n();
 const node=useRef<HTMLDivElement>(null);
 useEffect(()=>{
  const outside=(event:PointerEvent)=>{if(!node.current?.contains(event.target as Node))onClose();};
  const key=(event:KeyboardEvent)=>{if(event.key==='Escape'){event.preventDefault();event.stopImmediatePropagation();onClose();}};
  window.addEventListener('pointerdown',outside,true);window.addEventListener('keydown',key,true);
  window.addEventListener('scroll',onClose,true);window.addEventListener('resize',onClose);window.addEventListener('blur',onClose);
  return()=>{window.removeEventListener('pointerdown',outside,true);window.removeEventListener('keydown',key,true);window.removeEventListener('scroll',onClose,true);window.removeEventListener('resize',onClose);window.removeEventListener('blur',onClose);};
 },[onClose]);
 const copy=async()=>{onClose();try{await desktop('CopyText',item.text);}catch{report(t('chat.copyFailed'));}};
 return createPortal(<div ref={node} className="message-menu" role="menu" aria-label={t('chat.messageActions')}
  style={{left:Math.max(8,Math.min(x,innerWidth-176)),top:Math.max(8,Math.min(y,innerHeight-94))}}>
  <button role="menuitem" onClick={()=>{onQuote(item);onClose();}}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 9 4 14l5 5M4 14h10a6 6 0 0 1 6 6"/></svg><span>{t('chat.quoteMessage')}</span></button>
  <button role="menuitem" onClick={()=>void copy()}><svg viewBox="0 0 24 24" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/></svg><span>{t('chat.copyAction')}</span></button>
 </div>,document.body);
}

// The chat window owns the one draft editor. Writes serialize with the backend
// revision fence, and UI-initiated close waits for pending writes.
type ComposerProps={snapshot:Snapshot|null;active?:boolean;focusRevision?:number;refresh:()=>Promise<void>;onOutgoing?:(item:Item)=>void;quoteRequest?:{item:Item}|null;onQuoteConsumed?:()=>void;feedbackTarget:HTMLElement|null};
let flushVisibleComposer=async()=>{};
const Composer=memo(function Composer({snapshot,active=true,focusRevision=0,refresh,onOutgoing,quoteRequest,onQuoteConsumed,feedbackTarget}:ComposerProps) {
 const {t,locale} = useI18n();
 const draftLoadFailed=useEffectEvent(()=>t('chat.draftLoadFailed'));
 const input=useRef<HTMLTextAreaElement>(null),send=useRef<HTMLButtonElement>(null),add=useRef<HTMLButtonElement>(null);
 // WebKit treats inline Writing Suggestions separately from spell checking.
 // The chat editor does not offer these suggestions; suppress them at the DOM
 // input itself while leaving native IME composition and selection intact.
 useLayoutEffect(()=>{input.current?.setAttribute('writingsuggestions','false');},[]);
 // Let WebKit own the live textarea value and selection. React only needs to
 // know when the send affordance crosses the empty/nonempty boundary.
 const draft=useRef(''),hasTextRef=useRef(false);
 const quotedRef=useRef<QuotedMessage|null>(null);
 const [quoted,setQuoted]=useState<QuotedMessage|null>(null);
 const [hasText,setHasText]=useState(false),[refs,setRefs]=useState<string[]>([]),[files,setFiles]=useState<DraftFile[]>([]);
 const [pluginLabels,setPluginLabels]=useState<Record<string,string>>({});
 const [busy,setBusy]=useState(false),[expanded,setExpanded]=useState(false),[error,setError]=useState(''),[loaded,setLoaded]=useState(false);
 const [sendBlocked,setSendBlocked]=useState(false),[cleanupPending,setCleanupPending]=useState(false),[syncReadFailed,setSyncReadFailed]=useState(false),[filesLoaded,setFilesLoaded]=useState(false);
 const [dragging,setDragging]=useState(false),[feedback,setFeedback]=useState<'duplicate'|''>('');
 const visible=useRef(active);visible.current=active;
 useEffect(()=>{setExpanded(false);},[active,focusRevision]);
 useEffect(()=>{
  if(!refs.some(id=>id.startsWith('bot-plugin:')||id.startsWith('codex-plugin:')))return;
  let alive=true;
  void desktop<{items:Parameters<typeof botMenuPlugins>[0]}>('Plugins').then(value=>{
   if(alive)setPluginLabels(previous=>Object.fromEntries([...Object.entries(previous),...botMenuPlugins(value.items,locale).map(item=>[item.id,item.name])]));
  }).catch(()=>{});
  void backend<Array<{id:string;name:string;description:string;source:string}>>('NativePlugins').then(value=>{
   if(alive)setPluginLabels(previous=>Object.fromEntries([...Object.entries(previous),...nativeMenuPlugins(value).map(item=>[item.id,item.name])]));
  }).catch(()=>{});
  return()=>{alive=false;};
 },[refs,locale]);
 const pending=useRef<{request:Submission;files:DraftFile[];outgoing:Item;progress:SubmissionProgress;generation:number;draftRevision:number;editRevision:number}|null>(null);
 const lifetime=useRef(0),working=useRef(false);
 const fileOrder=useRef(new FileObservationOrder()),editGeneration=useRef(0),unsavedAfterAccepted=useRef(false),stopFileRead=useRef<()=>void>(()=>{});
 const saved=useRef<Draft>({revision:0,text:'',referenceIds:[],notice:''});
 const presentFiles=(value:DraftFile[])=>{
  const visible=visibleDraftFiles(value,saved.current),attempt=pending.current;
  return attempt?.progress.outcome==='sending'?visible.filter(file=>!attempt.request.fileIds.includes(file.id)):visible;
 };
 const writes=useRef(new DraftQueue()), conflicted=useRef(false),composing=useRef(false);
 useEffect(()=>{
  const flush=async()=>{
   if(composing.current||unsavedAfterAccepted.current)throw new Error('draft is not ready to close');
   await writes.current.flush();
   if(conflicted.current)throw new Error('draft save conflict');
  };
  flushVisibleComposer=flush;
  return()=>{if(flushVisibleComposer===flush)flushVisibleComposer=async()=>{};};
 },[]);
 const flushDraft=useEffectEvent(async()=>{
  try{await writes.current.flush();}
  catch{setError(t('chat.draftSaveFailed'));}
 });
 useEffect(()=>{
  // Native close hides reusable WebViews. Flush on the host event and focus
  // loss, independently of React's activation cleanup.
  const flush=()=>{void flushDraft();};
  window.addEventListener('history-close',flush);
  window.addEventListener('blur',flush);
  window.addEventListener('pagehide',flush);
  return()=>{window.removeEventListener('history-close',flush);window.removeEventListener('blur',flush);window.removeEventListener('pagehide',flush);};
 },[]);
 useEffect(()=>{
  lifetime.current++;working.current=false;pending.current=null;setBusy(false);
  setLoaded(false);setFilesLoaded(false);setSendBlocked(false);setCleanupPending(false);setSyncReadFailed(false);fileOrder.current.reset();
  const readFiles=()=>{stopFileRead.current();const ticket=fileOrder.current.request();setFilesLoaded(false);stopFileRead.current=retryRead(()=>desktop<DraftFile[]>('DraftFiles'),value=>{if(!fileOrder.current.accept(ticket))return;const visible=presentFiles(value);setFiles(visible);setFilesLoaded(true);setError(previous=>previous===t('chat.fileSelectionReadFailed')?(saved.current.cleanupPending?t('chat.acceptedCleanupPending'):saved.current.rejectedCleanupPending?t('chat.rejectedCleanupPending'):saved.current.pendingSend?t('chat.originalAttachmentPending'):saved.current.notice):previous);if(!visible.length)setFeedback('');},()=>setError(previous=>previous||t('chat.fileSelectionReadFailed')));};
  const stopDraft=retryRead(async()=>{
   await writes.current.flush();
   return backend<Draft>('Draft');
  },d=>{saved.current=d;conflicted.current=false;unsavedAfterAccepted.current=false;setDraftValue(d.text);setRefs(d.referenceIds??[]);quotedRef.current=d.quoted??null;setQuoted(quotedRef.current);setFiles(old=>visibleDraftFiles(old,d));setError(d.cleanupPending?t('chat.acceptedCleanupPending'):d.rejectedCleanupPending?t('chat.rejectedCleanupPending'):d.pendingSend?t('chat.originalAttachmentPending'):d.notice);setSendBlocked(!!(d.pendingSend||d.rejectedCleanupPending));setCleanupPending(!!(d.cleanupPending||d.rejectedCleanupPending));setLoaded(true);readFiles();if(visible.current)input.current?.focus();},()=>setError(draftLoadFailed()));
  const changed=(event:Event)=>{
   const detail=(event as CustomEvent<string|{error:string;added:number}>).detail;
   readFiles();setDragging(false);
   const failure=typeof detail==='string'?detail:detail?.error??'';
   setError(previous=>previous===t('chat.sentDraftSyncFailed')?previous:failure||(saved.current.cleanupPending?t('chat.acceptedCleanupPending'):saved.current.rejectedCleanupPending?t('chat.rejectedCleanupPending'):saved.current.pendingSend?t('chat.originalAttachmentPending'):saved.current.notice));setFeedback('');
  };
  window.addEventListener('files-changed',changed);
  return()=>{void flushDraft();lifetime.current++;stopDraft();stopFileRead.current();fileOrder.current.reset();window.removeEventListener('files-changed',changed);};
 },[]);
 useEffect(()=>{if(active&&loaded&&!busy)input.current?.focus({preventScroll:true});},[active,loaded,busy,focusRevision]);
 const sizeEditor=()=>{
  // Recent WebKit sizes the field in CSS. Older WebKit still needs a measured
  // fallback. The content scroller bounds the combined text and attachments.
  if(CSS.supports('field-sizing','content'))return;
  const editor=input.current;if(!editor)return;
  editor.style.height='0px';
  const height=Math.max(27,editor.scrollHeight);
  editor.style.height=`${height}px`;
 };
 const setDraftValue=(text:string,composingNow=false)=>{
  draft.current=text;
  if(input.current&&input.current.value!==text)input.current.value=text;
  sizeEditor();
  if(composingNow)return;
  const nonempty=!!text.trim();
  if(hasTextRef.current!==nonempty){hasTextRef.current=nonempty;setHasText(nonempty);}
 };
 const save=(text:string,referenceIds:string[],nextQuote=quotedRef.current)=>{
  editGeneration.current++;
  setDraftValue(text,composing.current);if(refs!==referenceIds)setRefs(referenceIds);
  if(quotedRef.current!==nextQuote){quotedRef.current=nextQuote;setQuoted(nextQuote);}
  if(syncReadFailed){unsavedAfterAccepted.current=true;setError(t('chat.sentDraftSyncFailed'));return;}
  if(composing.current)return;
  void writes.current.enqueue(async()=>{
   if(conflicted.current)throw new Error('draft_write_unresolved');
   try{saved.current=await backend<Draft>('SaveDraft',{revision:saved.current.revision,text,referenceIds,quoted:nextQuote});}
   catch(e){conflicted.current=true;setError(e instanceof Error?e.message:t('chat.draftSaveFailed'));throw e;}
  });
 };
 useEffect(()=>{
  if(!loaded||!quoteRequest)return;
  const item=quoteRequest.item;
  save(draft.current,refs,{localId:item.id,role:item.kind==='user'?'user':'assistant',text:item.text});
  onQuoteConsumed?.();input.current?.focus();
 },[loaded,quoteRequest]);
 const pick=async()=>{
  setExpanded(false);setBusy(true);setError('');setFeedback('');
  const ticket=fileOrder.current.request(),generation=lifetime.current;
  try{
   const selected=visibleDraftFiles(await desktop<DraftFile[]>('PickFiles'),saved.current);
   if(generation!==lifetime.current)return;
   if(fileOrder.current.accept(ticket)){setFiles(selected);setFilesLoaded(true);}
   setExpanded(false);
  }catch(e){if(generation===lifetime.current)setError(e instanceof Error?e.message:t('chat.pickFilesFailed'));}
  finally{if(generation===lifetime.current){setBusy(false);input.current?.focus();}}
 };
 const paste=async(e:ClipboardEvent<HTMLTextAreaElement>)=>{
  const types=Array.from(e.clipboardData.types);
  if(!e.clipboardData.files.length&&!types.some(type=>type==='Files'||type==='text/uri-list'||type==='public.file-url'||type.startsWith('image/')))return;
  e.preventDefault();
  const text=e.clipboardData.getData('text/plain');
  const start=e.currentTarget.selectionStart,end=e.currentTarget.selectionEnd,generation=lifetime.current;
  const fileTicket=fileOrder.current.request();
  setBusy(true);setError('');setFeedback('');
  try{
   const result=await desktop<PasteResult>('PasteAttachments');
   if(generation!==lifetime.current)return;
   if(result.handled){const before=files.length;const visible=visibleDraftFiles(result.files,saved.current);if(fileOrder.current.accept(fileTicket)){setFiles(visible);setFilesLoaded(true);setFeedback(visible.length>before?'':'duplicate');}}
   else if(text){save(draft.current.slice(0,start)+text+draft.current.slice(end),refs);requestAnimationFrame(()=>input.current?.setSelectionRange(start+text.length,start+text.length));}
   else setError(t('chat.clipboardNoFiles'));
  }catch(err){if(generation===lifetime.current)setError(err instanceof Error?err.message:t('chat.pasteFailed'));}
  finally{if(generation===lifetime.current){setBusy(false);input.current?.focus();}}
 };
 const accepted=async(attempt:NonNullable<typeof pending.current>)=>{
  attempt.progress.observe('accepted');
  setFeedback('');
  onOutgoing?.({...attempt.outgoing,status:'accepted'});
  try{await attempt.progress.synchronize(async()=>{
    stopFileRead.current();
    const fileTicket=fileOrder.current.request(),editTicket=editGeneration.current;
    const [next,nextFiles]=await readDraftAndFiles(()=>backend<Draft>('Draft'),()=>desktop<DraftFile[]>('DraftFiles'));
    if(lifetime.current!==attempt.generation||pending.current!==attempt)return;
    if(!acceptedDraftSettled(attempt.request,attempt.draftRevision,next))throw new Error('accepted draft still settling');
    saved.current=next;
    if(editTicket===editGeneration.current){setDraftValue(next.text);setRefs(next.referenceIds??[]);quotedRef.current=next.quoted??null;setQuoted(quotedRef.current);}
    const filesCurrent=fileOrder.current.accept(fileTicket),visible=visibleDraftFiles(nextFiles,next);
    if(filesCurrent){setFiles(visible);setFilesLoaded(true);setFeedback('');}
    setError(next.cleanupPending?t('chat.acceptedCleanupPending'):next.rejectedCleanupPending?t('chat.rejectedCleanupPending'):next.pendingSend?t('chat.originalAttachmentPending'):next.notice);setSendBlocked(!!(next.pendingSend||next.rejectedCleanupPending));setCleanupPending(!!(next.cleanupPending||next.rejectedCleanupPending));setSyncReadFailed(false);setLoaded(true);
  });}catch{
   if(lifetime.current===attempt.generation&&pending.current===attempt){
    if(draft.current===attempt.request.text)setDraftValue('');
    if(quotedRef.current===attempt.request.quoted){quotedRef.current=null;setQuoted(null);}
    setRefs(current=>current.length===attempt.request.referenceIds.length&&current.every((id,index)=>id===attempt.request.referenceIds[index])?[]:current);
    setFiles(current=>current.filter(file=>!attempt.request.fileIds.includes(file.id)));
    setFeedback('');setLoaded(true);setSendBlocked(true);setSyncReadFailed(true);setError(t('chat.sentDraftSyncFailed'));
   }
  }
 };
 const restoreSubmittedDraft=(attempt:NonNullable<typeof pending.current>)=>{
  // The original durable draft is untouched until native acceptance. Restore
  // its local presentation on refusal/uncertainty, never over a newer edit.
  if(pending.current!==attempt||lifetime.current!==attempt.generation||editGeneration.current!==attempt.editRevision)return;
  setDraftValue(attempt.request.text);setRefs(attempt.request.referenceIds);
  quotedRef.current=attempt.request.quoted??null;setQuoted(quotedRef.current);
  setFiles(current=>[...attempt.files,...current.filter(file=>!attempt.request.fileIds.includes(file.id))]);
 };
 const retryLocalCleanup=async()=>{
  stopFileRead.current();
  const generation=lifetime.current,fileTicket=fileOrder.current.request(),editTicket=editGeneration.current;
  try{
   await writes.current.flush();
   if(pending.current?.progress.outcome==='unknown'){
    // A successful local draft read alone is not a terminal send receipt.
    // Let the exact native request settle before enabling another submission.
    await backend<Draft>('Draft');await refresh();return;
   }
   let next=await backend<Draft>('Draft');
   if(generation!==lifetime.current)return;
   if(editTicket!==editGeneration.current){setError(t('chat.sentDraftSyncFailed'));return;}
   if(syncReadFailed&&unsavedAfterAccepted.current&&next.pendingSend){setError(t('chat.sentDraftSyncFailed'));return;}
   if(syncReadFailed&&unsavedAfterAccepted.current&&!next.pendingSend){
    const local=await backend<Draft>('SaveDraft',{revision:next.revision,text:draft.current,referenceIds:refs,quoted:quotedRef.current});
    saved.current=local;unsavedAfterAccepted.current=false;
    next=await backend<Draft>('Draft');
    if(generation!==lifetime.current||editTicket!==editGeneration.current)return;
   }else if(!unsavedAfterAccepted.current&&!conflicted.current){setDraftValue(next.text);setRefs(next.referenceIds??[]);quotedRef.current=next.quoted??null;setQuoted(quotedRef.current);}
   if(conflicted.current){setError(t('chat.draftSaveFailed'));return;}
   saved.current=next;
   const nextFiles=await desktop<DraftFile[]>('DraftFiles');
   if(generation!==lifetime.current)return;
   if(fileOrder.current.accept(fileTicket)){const visible=visibleDraftFiles(nextFiles,next);setFiles(visible);setFilesLoaded(true);if(!visible.length)setFeedback('');}
   setError(next.cleanupPending?t('chat.acceptedCleanupPending'):next.rejectedCleanupPending?t('chat.rejectedCleanupPending'):next.pendingSend?t('chat.originalAttachmentPending'):next.notice);setSendBlocked(!!(next.pendingSend||next.rejectedCleanupPending));setCleanupPending(!!(next.cleanupPending||next.rejectedCleanupPending));setSyncReadFailed(false);conflicted.current=false;setLoaded(true);
  }catch{if(generation===lifetime.current){setSendBlocked(true);setError(t(syncReadFailed||cleanupPending||pending.current?.progress.outcome==='accepted'?'chat.sentDraftSyncFailed':'chat.fileSelectionReadFailed'));}}
 };
 const submit=async()=>{
  if(working.current||busy||!loaded||!filesLoaded||sendBlocked||(cleanupPending&&files.length>0)||!canSubmit(snapshot)||(!draft.current.trim()&&!files.length))return;
  working.current=true;setBusy(true);setError('');setExpanded(false);
  const request:Submission={id:crypto.randomUUID(),text:draft.current,fileIds:files.map(f=>f.id),referenceIds:refs,quoted:quotedRef.current??undefined};
  const outgoing:Item={id:`outgoing:${request.id}`,requestId:request.id,seenAt:Date.now()*1000,turnKey:'',kind:'user',text:[draft.current,...files.map(f=>f.name)].filter(Boolean).join('\n'),status:'sending',details:'',activity:null,artifacts:[],quoted:request.quoted};
  const attempt={request,files:[...files],outgoing,progress:new SubmissionProgress(),generation:lifetime.current,draftRevision:saved.current.revision,editRevision:editGeneration.current};
  pending.current=attempt;onOutgoing?.(outgoing);
  // Move the message into chat in the same interaction frame. This is only a
  // view change: queued draft writes and the original submission stay intact.
  stopFileRead.current();fileOrder.current.reset();
  setDraftValue('');setRefs([]);quotedRef.current=null;setQuoted(null);setFiles([]);setFeedback('');
  try{
   try{await writes.current.flush();}
   catch{attempt.progress.observe('rejected');onOutgoing?.({...outgoing,status:'rejected'});restoreSubmittedDraft(attempt);return;}
   if(lifetime.current!==attempt.generation)return;
   attempt.draftRevision=saved.current.revision;
   if(conflicted.current){attempt.progress.observe('rejected');onOutgoing?.({...outgoing,status:'rejected'});restoreSubmittedDraft(attempt);return;}
   let receipt:Receipt;
   try{receipt=await backend<Receipt>('Submit',request);}
   catch{receipt={id:request.id,outcome:'unknown',message:t('chat.sendPendingChat')};}
   if(lifetime.current!==attempt.generation)return;
   const outcome=attempt.progress.observe(receipt.outcome);
   onOutgoing?.({...outgoing,status:outcome});
   if(outcome==='accepted')await accepted(attempt);
   else {restoreSubmittedDraft(attempt);setSendBlocked(outcome!=='rejected');setError(outcome==='rejected'?'':t('chat.sendPending'));}
  }finally{
   // Keep the disabled state until the authoritative send/stop action is fresh.
   try{await refresh();}catch{/* A read failure does not change the send receipt. */}
   if(lifetime.current===attempt.generation&&pending.current===attempt){working.current=false;setBusy(false);}
  }
 };
 useEffect(()=>{
  const attempt=pending.current;
  if(!attempt||snapshot?.lastReceipt.id!==attempt.request.id)return;
  if(snapshot.lastReceipt.outcome==='accepted')void accepted(attempt);
  else if(snapshot.lastReceipt.outcome==='rejected'&&attempt.progress.observe('rejected')==='rejected'){
   restoreSubmittedDraft(attempt);setSendBlocked(false);setError('');onOutgoing?.({...attempt.outgoing,status:'rejected'});
  }
 },[snapshot?.lastReceipt.id,snapshot?.lastReceipt.outcome]);
 const showRecovery=!!error;
 useEffect(()=>{
  if(!active||!(sendBlocked||cleanupPending||syncReadFailed||loaded&&!filesLoaded))return;
  let cancelled=false,timer:ReturnType<typeof setTimeout>,attempts=0;
  const read=async()=>{
   if(cancelled)return;
   await retryLocalCleanup();
   if(!cancelled&&++attempts<3)timer=setTimeout(()=>void read(),2000*attempts);
  };
  timer=setTimeout(()=>void read(),1000);
  return()=>{cancelled=true;clearTimeout(timer);};
 },[active,sendBlocked,cleanupPending,syncReadFailed,loaded,filesLoaded]);
 const primaryAction=composerAction(snapshot,!!(hasText||files.length||refs.length));
 const stopping=snapshot?.phase==='interrupting';
 // Background draft cleanup does not add a second progress row or obscure Stop.
 // When preparation really blocks a send, show progress in that fixed-size action.
 const preparingSend=primaryAction==='send'&&!showRecovery&&(busy||!loaded||!filesLoaded||sendBlocked||(cleanupPending&&files.length>0));
 const enabled=loaded&&!busy&&(primaryAction==='stop'?!stopping:filesLoaded&&!sendBlocked&&!(cleanupPending&&files.length>0)&&canSubmit(snapshot)&&!!(hasText||files.length));
 const interrupt=async()=>{
  if(busy||!loaded||!snapshot?.canInterrupt||stopping)return;
  setBusy(true);setError('');
  try{await backend('Interrupt');}
  catch(e){setError(e instanceof Error?e.message:t('chat.interruptFailed'));}
  finally{
   try{await refresh();}catch{setError(previous=>previous||t('chat.interruptPending'));}
   finally{setBusy(false);}
  }
 };
 const actionLabel=primaryAction==='stop'?(stopping?t('chat.stopping'):t('chat.stopWork')):snapshot?.canSteer&&snapshot.maintenance!=='dreaming'?t('chat.steerWork'):t('chat.send');
 const needsConnection=loaded&&!!(hasText||files.length)&&snapshot?.connection!=='ready'&&!snapshot?.canSteer;
 return <div className={`compose-area${dragging?' file-dragging':''}`} data-file-drop-target
  onDragEnter={e=>{if(e.dataTransfer.types.includes('Files'))setDragging(true)}}
  onDragOver={e=>{if(e.dataTransfer.types.includes('Files')){e.preventDefault();setDragging(true)}}}
  onDragLeave={e=>{if(!e.currentTarget.contains(e.relatedTarget as Node))setDragging(false)}}
  onDrop={()=>setDragging(false)}>
  <div className="composer-scroll">
   <textarea aria-label={t('chat.composerLabel')} ref={input} rows={1} defaultValue="" spellCheck={false} autoCorrect="off" disabled={!loaded||busy} onChange={e=>save(e.target.value,refs)} onCompositionStart={()=>{composing.current=true;}} onCompositionEnd={e=>{composing.current=false;save(e.currentTarget.value,refs);}} onPaste={e=>void paste(e)} onKeyDown={e=>handleComposerKey(e.nativeEvent,send.current,primaryAction)} placeholder={snapshot?.canSteer&&snapshot.maintenance!=='dreaming'?t('chat.steerPlaceholder'):t('chat.composerPlaceholder')} title={t('chat.composerKeyHint')}/>
  {!!error&&feedbackTarget&&createPortal(<ChatFeedback key={error} message={error} feedback={{issue:pending.current?.progress.outcome==='unknown'?'submission_unconfirmed':'background_read_failed'}} onRetry={()=>void retryLocalCleanup()}/>,feedbackTarget)}
  {!error&&needsConnection&&<div className="composer-connection-hint" role="status"><span>{t('chat.connectionUnavailableToSend')}</span></div>}
  {!error&&(dragging||feedback)&&<p role="status" className="input-feedback">{dragging?t('chat.dropAttachments'):t('chat.attachmentAlreadyAdded')}</p>}
  {(!!quoted||!!files.length||!!refs.length)&&<div className="composer-context">
  {quoted&&<div className="composer-quote"><p>{quoted.text}</p><button type="button" aria-label={t('chat.removeQuote')} disabled={busy} onClick={()=>{save(draft.current,refs,null);input.current?.focus();}}><Icon name="xmark"/></button></div>}
  {!!(files.length||refs.length)&&<ul className="attachments" aria-label={t('chat.attachmentsLabel')}>
   {files.map(f=><li key={f.id} className={f.unavailable?'attachment-unavailable':''} title={f.name}><DraftThumbnail file={f} returnFocus={input.current}/><button disabled={busy} aria-label={t('chat.removeAttachment',{name:f.name})} onClick={()=>{const ticket=fileOrder.current.request();void desktop<DraftFile[]>('RemoveFile',f.id).then(value=>{if(fileOrder.current.accept(ticket)){setFiles(visibleDraftFiles(value,saved.current));setFilesLoaded(true);setFeedback('');input.current?.focus();}}).catch(()=>setError(t('chat.attachmentUpdateFailed')));}}><Icon name="xmark"/></button></li>)}
   {refs.map(id=><li key={id}><span>{pluginLabels[id]??snapshot?.references.find(r=>r.id===id)?.name??(id.includes('-plugin:')?id.split(':').slice(1).join(':'):t('chat.referenceDefault'))}</span><button disabled={busy} aria-label={t('chat.removeReference')} onClick={()=>{save(draft.current,refs.filter(v=>v!==id));input.current?.focus();}}><Icon name="xmark"/></button></li>)}
  </ul>}
  </div>}
  </div>
  <div className="capsule composer-actions">
   <button ref={add} className="icon-button add" disabled={busy||!loaded} onClick={()=>setExpanded(!expanded)} aria-label={t('chat.addAttachmentOrReference')} aria-expanded={expanded} aria-haspopup="menu" aria-controls={expanded?'attachment-menu':undefined}><Icon name="plus"/></button>
   <button ref={send} className="icon-button send" disabled={!enabled} onClick={()=>void (primaryAction==='stop'?interrupt():submit())} aria-busy={preparingSend} aria-label={preparingSend?t('common.loading'):actionLabel} title={needsConnection?t('chat.connectionUnavailableToSend'):actionLabel}>{primaryAction==='stop'?<span className="composer-stop" aria-hidden="true"/>:preparingSend?<span className="composer-progress" aria-hidden="true"/>:<Icon name="arrow.up"/>}</button>
  </div>
  {expanded&&<AttachmentMenu trigger={add} selected={refs}
   onClose={()=>setExpanded(false)} onPick={()=>void pick()}
   onSelect={plugin=>{setPluginLabels(previous=>({...previous,[plugin.id]:plugin.name}));save(draft.current,[...refs,plugin.id]);setExpanded(false);input.current?.focus();}}/>}
 </div>;
},(a,b)=>a.active===b.active&&a.focusRevision===b.focusRevision&&a.quoteRequest===b.quoteRequest&&a.feedbackTarget===b.feedbackTarget&&
 a.refresh===b.refresh&&a.onOutgoing===b.onOutgoing&&sameComposerSnapshot(a.snapshot,b.snapshot));

export function History({composerGeneration=0,settingsEntry}:{composerGeneration?:number;settingsEntry?:ReactNode}={}) {
 const {t,locale} = useI18n();
 const [active,setActive]=useState(false),[error,setError]=useState(''),[busy,setBusy]=useState(false),[unread,setUnread]=useState(false);
 const [interactionOpen,setInteractionOpen]=useState(false),[interactionIndex,setInteractionIndex]=useState(0);
 const interactionDock=useRef<HTMLElement>(null);
 const [connectionError,setConnectionError]=useState('');
 const [feedbackTarget,setFeedbackTarget]=useState<HTMLDivElement|null>(null);
 const composerMounted=useRef(false);if(active)composerMounted.current=true;
 const [activation,setActivation]=useState(0),[presentation,setPresentation]=useState(0);
 const {snapshot,liveReplies,refresh}=useConversation(active,false,true);
 useEffect(()=>{if(active&&snapshot)surfaceReady('chat');},[active,snapshot,presentation]);
 const [outgoing,setOutgoing]=useState<Item[]>([]);
 const [menu,setMenu]=useState<{item:Item;x:number;y:number}|null>(null);
 const [quoteRequest,setQuoteRequest]=useState<{item:Item}|null>(null);
 const [quotePreview,setQuotePreview]=useState<QuotedMessage|null>(null);
 const closeMenu=useCallback(()=>setMenu(null),[]);
 const showMenu=useCallback((item:Item,x:number,y:number)=>setMenu({item,x,y}),[]);
 const quoteMessage=useCallback((item:Item)=>setQuoteRequest({item}),[]);
 const quoteConsumed=useCallback(()=>setQuoteRequest(null),[]);
 const showQuotePreview=useCallback((quote:QuotedMessage)=>setQuotePreview(quote),[]);
 useEffect(()=>{if(!active){setMenu(null);setQuotePreview(null);}},[active]);
 useEffect(()=>{
  if(!quotePreview)return;
  const close=(event:KeyboardEvent)=>{if(event.key==='Escape'){event.preventDefault();event.stopImmediatePropagation();setQuotePreview(null);}};
  window.addEventListener('keydown',close,true);
  return()=>window.removeEventListener('keydown',close,true);
 },[quotePreview]);
 const stage=useCallback((item:Item)=>{if(item.status==='sending'){position.current?.latest();setUnread(false);}setOutgoing(previous=>{const prior=previous.find(p=>p.requestId===item.requestId);return [...previous.filter(p=>p.requestId!==item.requestId),prior?.status==='accepted'?prior:item];});},[]);
 useEffect(()=>{const known=new Set(snapshot?.items.map(i=>i.requestId).filter(Boolean));setOutgoing(previous=>previous.filter(i=>!known.has(i.requestId)));},[snapshot]);
 const [earlierBusy,setEarlierBusy]=useState(false);
 const prepend=useRef<{id:string;top:number}|null>(null);
 const scroll=useRef<HTMLDivElement>(null),content=useRef<HTMLDivElement>(null),position=useRef<ChatScroll|null>(null);
 useLayoutEffect(()=>{position.current=new ChatScroll(scroll.current!,()=>{
   const el=scroll.current!,question=el.querySelector<HTMLElement>('[data-question-pending]');
   return question?Math.max(0,el.scrollTop+question.getBoundingClientRect().top-el.getBoundingClientRect().top-12):undefined;
  });return()=>{position.current=null;};},[]);
 useEffect(()=>{
  let activeNow=false;
  // Every explicit open must acknowledge its native presentation token, even
  // when the conversation and active state are unchanged behind another app.
  const opened=(event?:Event)=>{activeNow=true;setActive(true);setPresentation(value=>value+1);if(!(event instanceof CustomEvent && event.detail?.preservePosition))setActivation(value=>value+1);},visible=()=>{if(!activeNow)setActivation(value=>value+1);activeNow=true;setActive(true);},closed=()=>{activeNow=false;setActive(false);};
  let mounted=true;
  const stopVisibility=retryRead(()=>desktop<boolean>('HistoryVisible'),v=>{if(mounted&&v)visible();});
  const key=(e:KeyboardEvent)=>{if(!isWebSurface()&&activeNow&&!e.defaultPrevented&&!e.isComposing&&(e.key==='Escape'||(e.metaKey&&e.key==='w'))){e.preventDefault();void flushVisibleComposer().then(()=>desktop('CloseHistory')).catch(()=>{});}};
  surfaceMounted('chat');
  window.addEventListener('history-open',opened);window.addEventListener('history-visible',visible);window.addEventListener('history-close',closed);window.addEventListener('keydown',key);
  return()=>{mounted=false;stopVisibility();window.removeEventListener('history-open',opened);window.removeEventListener('history-visible',visible);window.removeEventListener('history-close',closed);window.removeEventListener('keydown',key);};
 },[]);
 useLayoutEffect(()=>{
  if(!active)return;
  prepend.current=null;position.current!.latest(true);setUnread(false);
 },[activation]);
 useLayoutEffect(()=>{
  if(!active)return;
  const resize=new ResizeObserver(()=>{position.current?.layout();});
  resize.observe(scroll.current!);resize.observe(content.current!);
  return()=>resize.disconnect();
 },[active]);
 const [answeredQuestions,setAnsweredQuestions]=useState<ReadonlySet<string>>(()=>new Set());
 const questionAnswered=useCallback((card:QuestionCard)=>setAnsweredQuestions(previous=>new Set(previous).add(questionIdentity(card))),[]);
 const messages=useMemo(()=>visibleQuestionMessages(withOutgoing(snapshot?.items??[],outgoing),answeredQuestions).filter(i=>i.kind==='user'||i.kind==='controlNotice'||i.kind==='tool'&&!!i.artifacts?.length||(i.kind==='assistant'&&(i.text.trim()||i.artifacts?.length))),[snapshot?.items,outgoing,answeredQuestions]);
 const timeAnchors=useRef<ReadonlyMap<string,ChatTimeAnchor>>(new Map());
 const timeline=useMemo(()=>chatTimeline(messages,timeAnchors.current),[messages]);
 useLayoutEffect(()=>{timeAnchors.current=timeline.anchors;},[timeline]);
 const contentKey=useMemo(()=>messages.map(i=>i.id+i.text+(i.quoted?.text??'')+(i.task?.status??'')+JSON.stringify(i.questionCards??[])+(i.artifacts??[]).map(a=>a.id+a.name).join('')).join(''),[messages]);
 const prompts=snapshot?.approvals.filter(p=>p.status==='pending')??[];
 const interactions=prompts.map(approval=>({key:`approval:${approval.id}`,approval}));
 const currentInteraction=Math.min(interactionIndex,Math.max(0,interactions.length-1));
 const interactionKey=interactions.map(entry=>entry.key).join('|');
 const refreshQuestions=useCallback(()=>{void refresh().catch(()=>{});},[refresh]);
 useEffect(()=>{
  setInteractionIndex(index=>Math.min(index,Math.max(0,interactions.length-1)));
  if(!interactions.length)setInteractionOpen(false);
 },[interactions.length]);
 useEffect(()=>{
  if(!interactionOpen||!active)return;
  const outside=(event:PointerEvent)=>{if(!interactionDock.current?.contains(event.target as Node))setInteractionOpen(false);};
  const escape=(event:KeyboardEvent)=>{if(event.key==='Escape'&&!event.isComposing){event.preventDefault();event.stopImmediatePropagation();setInteractionOpen(false);interactionDock.current?.querySelector('button')?.focus();}};
  window.addEventListener('pointerdown',outside,true);
  window.addEventListener('keydown',escape,true);
  return()=>{window.removeEventListener('pointerdown',outside,true);window.removeEventListener('keydown',escape,true);};
 },[interactionOpen,active]);
 useLayoutEffect(()=>{
  if(!active||!interactionOpen)return;
  const entry=interactionDock.current?.querySelector<HTMLElement>('[data-interaction]:not([hidden])');
  entry?.querySelector<HTMLElement>('.approval-choices button:not(:disabled)')?.focus({preventScroll:true});
 },[interactionOpen,currentInteraction]);
 const activity=chatActivity(snapshot,outgoing);
 const avatar=useAvatarPresentation(snapshot,active);
 const activeReply=active?animatedReplyID(snapshot,avatar.completion):null;
 const connection=useConnectionNotice(snapshot);
 useEffect(()=>{if(snapshot?.connection==='ready')setConnectionError('');},[snapshot?.connection]);
 const setup=snapshot?.connectionIssue==='runtime_missing'||snapshot?.connectionIssue==='runtime_protocol';
 const chooseRuntime=snapshot?.connectionIssue==='setup_required';
 const connectionText=chooseRuntime?t('chat.setupRequiredConnection'):
  snapshot?.connection==='connecting'?t('chat.checkingConnection'):
  snapshot?.connection==='login'?t('chat.loginRequiredConnection'):
  snapshot?.connectionIssue==='runtime_missing'?t('chat.runtimeMissingConnection'):
  snapshot?.connectionIssue==='runtime_protocol'?t('chat.runtimeProtocolConnection'):
  snapshot?.connectionIssue==='resource_exhausted'?t('chat.resourceConnection'):
  snapshot?.connectionIssue==='existing_server'?t('chat.existingServerConnection'):
  snapshot?.connectionIssue==='connection'?t('chat.offlineConnection'):
  t('chat.offlineConnection');
 useLayoutEffect(()=>{
  const el=scroll.current;if(!el||!active)return;
  if(prepend.current){
   if(earlierBusy)return;
   const anchor=el.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(prepend.current.id)}"]`);
   if(anchor)el.scrollTop+=anchor.getBoundingClientRect().top-prepend.current.top;
   prepend.current=null;
  }else if(position.current!.following){position.current!.layout();setUnread(false);}else setUnread(true);
 },[contentKey,interactionKey,activity,snapshot?.connection,snapshot?.phase,snapshot?.message,snapshot?.hasEarlier,earlierBusy]);
 const earlier=async()=>{if(earlierBusy)return;setEarlierBusy(true);setError('');const el=scroll.current!;position.current!.following=false;const anchor=Array.from(el.querySelectorAll<HTMLElement>('[data-message-id]')).find(item=>item.getBoundingClientRect().bottom>el.getBoundingClientRect().top);if(anchor)prepend.current={id:anchor.dataset.messageId!,top:anchor.getBoundingClientRect().top};try{await backend('LoadEarlier');await refresh();}catch{prepend.current=null;setError(t('chat.loadEarlierFailed'));}finally{setEarlierBusy(false);}};
 const action=async(method:string)=>{setBusy(true);setError('');setConnectionError('');try{await backend(method);}catch(e){setConnectionError(e instanceof Error?e.message:t('chat.actionFailed'));}finally{setBusy(false);try{await refresh();}catch{setConnectionError(previous=>previous||t('chat.actionFailed'));}}};
 return <main className="history-surface" aria-label={t('chat.historyAriaLabel')}>
  <div className="chat-scroll" ref={scroll} onScroll={()=>{if(active&&position.current?.scrolled())setUnread(false);}}>
   <div className="chat-content" ref={content}>
   {!messages.length&&!connection&&!activity&&!interactions.length&&<p className="empty-conversation">{t('chat.emptyConversation')}</p>}
   {snapshot?.hasEarlier&&<div className="history-pagination"><button className="text-action" disabled={earlierBusy} onClick={()=>void earlier()}>{earlierBusy?t('common.loading'):t('chat.loadEarlier')}</button></div>}
   <div className="history-messages">{timeline.rows.map(({key,item:i,dividerAt})=><div key={key} className="history-message-group">
    {dividerAt!==undefined&&<div className="history-time-divider"><time dateTime={new Date(dividerAt/1000).toISOString()}>{chatTimeLabel(dividerAt,locale)}</time></div>}
    <Message item={i} report={setError} onContextMenu={showMenu} onQuotePreview={showQuotePreview} refresh={refreshQuestions} onQuestionAnswered={questionAnswered} animate={i.id===activeReply} clip={i.id===activeReply?(i.id===avatar.completion?'delight':avatar.clip):'companion'} reveal={active&&liveReplies.has(i.id)} incomplete={incompleteAssistant(i,snapshot)}/>
   </div>)}</div>
   {activity&&<WorkingMessage activity={activity} active={active} tool={snapshot?.activity} clip={activity==='thinking'?'think':avatar.clip}/>}
   {connection&&<article className="message-row assistant state-message">
    <BotAvatar animate={active&&prompts.length>0&&!connection} clip={avatar.clip}/>
    <div className="message assistant state-bubble">
   {connection&&snapshot&&<section className="connection-card" aria-label={t('chat.connectionCardAriaLabel')}>
    <strong>{snapshot.connection==='connecting'?t('chat.connectingTitle'):snapshot.connection==='login'?t('chat.loginTitle'):chooseRuntime?t('chat.chooseRuntimeTitle'):setup?t('chat.setupTitle'):t('chat.reconnectTitle')}</strong>
    <p>{connectionText}</p>
    {connectionError&&<Recovery message={connectionError}/>}
    <div className="connection-actions">
     {snapshot.connection==='login'&&!snapshot.loginPending&&<button disabled={busy} onClick={()=>void desktop('OpenRuntimeSettings').catch(()=>setConnectionError(t('chat.actionFailed')))}>{t('chat.loginInBrowser')}</button>}
     {snapshot.loginPending&&<button disabled={busy} onClick={()=>void action('CancelLogin')}>{t('chat.cancelLogin')}</button>}
     {!snapshot.loginPending&&!chooseRuntime&&snapshot.connection!=='connecting'&&<button disabled={busy} onClick={()=>void action('Reconnect')}>{busy?t('chat.connectingTitle'):setup?t('chat.recheckSetup'):t('chat.reconnect')}</button>}
     {setup&&<button className="text-action" onClick={()=>void action('OpenConnectionHelp')}>{t('chat.setupHelp')}</button>}
     {<button className={chooseRuntime?'primary':'text-action'} onClick={()=>void desktop('OpenRuntimeSettings')}>{t('chat.connectionSettings')}</button>}
    </div>
   </section>}
    </div>
   </article>}
   {/* Connection failures already have a scoped retry/settings card above. */}
   {!!(error||connectionError&&!connection)&&<ChatFeedback key={error||connectionError} message={error||connectionError} onDismiss={()=>{setError('');setConnectionError('');}}/>}
   <div ref={setFeedbackTarget}/>
   </div>
  </div>
  {unread&&<button className="new-messages" onClick={()=>{position.current!.latest();setUnread(false);}}>{t('chat.viewNewMessages')}</button>}
  {!!interactions.length&&<aside ref={interactionDock} className="approval-dock" aria-label={t('chat.interactionEyebrow')}>
   <button className="approval-dock-trigger" type="button" aria-expanded={interactionOpen} aria-haspopup="dialog" aria-controls="approval-dock-panel" onClick={()=>setInteractionOpen(open=>!open)}>{t('chat.approvalQueue',{current:currentInteraction+1,total:interactions.length})}{interactionOpen?<CaretDownIcon size={14} aria-hidden="true"/>:<CaretUpIcon size={14} aria-hidden="true"/>}</button>
   <div id="approval-dock-panel" className="approval-dock-panel" role="dialog" aria-label={t('chat.interactionEyebrow')} hidden={!interactionOpen}>
    <div className="approval-dock-nav"><span>{t('chat.approvalEyebrow')}{interactions.length>1&&` · ${currentInteraction+1}/${interactions.length}`}</span><div>{interactions.length>1&&<><button type="button" aria-label={t('chat.interactionPrevious')} disabled={currentInteraction===0} onClick={()=>setInteractionIndex(i=>i-1)}><CaretLeftIcon size={16} aria-hidden="true"/></button><button type="button" aria-label={t('chat.interactionNext')} disabled={currentInteraction>=interactions.length-1} onClick={()=>setInteractionIndex(i=>i+1)}><CaretRightIcon size={16} aria-hidden="true"/></button></>}<button type="button" aria-label={t('common.close')} onClick={()=>setInteractionOpen(false)}><XIcon size={15} aria-hidden="true"/></button></div></div>
    {interactions.map((entry,index)=><div data-interaction key={entry.key} hidden={index!==currentInteraction}><Prompt value={entry.approval} refresh={refreshQuestions}/></div>)}
   </div>
  </aside>}
  <footer>
   {settingsEntry}
   {composerMounted.current&&<Composer key={composerGeneration} snapshot={snapshot} active={active} focusRevision={activation} refresh={refresh} onOutgoing={stage} quoteRequest={quoteRequest} onQuoteConsumed={quoteConsumed} feedbackTarget={feedbackTarget}/>}
  </footer>
  {menu&&<MessageMenu item={menu.item} x={menu.x} y={menu.y} onQuote={quoteMessage} onClose={closeMenu} report={setError}/>}
  {quotePreview&&createPortal(<div className="quote-preview-backdrop" onClick={()=>setQuotePreview(null)}><section className="quote-preview" role="dialog" aria-modal="true" aria-label={t('chat.quoteMessage')} onClick={e=>e.stopPropagation()}><button className="quote-preview-close" aria-label={t('common.close')} onClick={()=>setQuotePreview(null)}><Icon name="xmark"/></button><pre>{quotePreview.text}</pre></section></div>,document.body)}
 </main>;
}
