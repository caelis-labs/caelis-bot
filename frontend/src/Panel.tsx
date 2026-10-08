import { memo, useCallback, useEffectEvent, useEffect, useLayoutEffect, useMemo, useRef, useState, type ClipboardEvent } from 'react';
import { backend, desktop, type DraftFile, type PasteResult } from './desktop';
import type { Approval, ChatUpdate, Decision, Draft, Item, Receipt, Snapshot, Submission } from './backend/contract';
import { handleComposerKey } from './composer-keyboard';
import { CopyText, MessageContent } from './MessageContent';
import { canSubmit, chatActivity, composerAction, liveReplyIDs, withOutgoing } from './chat-presentation';
import { WorkingMessage } from './WorkingMessage';
import { BotAvatar } from './BotAvatar';
import { useAvatarPresentation } from './use-avatar-presentation';
import { animatedReplyID, type PortraitClip } from './avatar-presentation';
import { AttachmentMenu } from './AttachmentMenu';
import { ScreenMessage } from './ScreenMessage';
import { MediaMessage } from './MediaMessage';
import { ChatScroll } from './chat-scroll';
import { ConversationOrder, FileObservationOrder, SubmissionProgress, DraftQueue, acceptedDraftSettled, visibleDraftFiles, readDraftAndFiles, retryRead, sameComposerSnapshot } from './chat-observation';
import { useI18n } from './i18n';
import { approvalChoice, approvalResult, approvalText, approvalTitle } from './approval-presentation';
import { CheckIcon, XIcon } from './SettingsIcons';
import type { MessageKey } from './i18n/catalogs';

function Icon({ name }: { name: string }) { return <img className="symbol" src={`/icons/${name}.png`} alt="" />; }

function DraftThumbnail({file}:{file:DraftFile}) {
 const [url,setURL]=useState('');
 useEffect(()=>{
  let active=true;setURL('');
  if(!file.image||file.unavailable)return;
  void desktop<string>('DraftImage',file.id).then(value=>{if(active&&/^data:image\/(png|jpeg|webp);base64,/.test(value))setURL(value)}).catch(()=>{});
  return()=>{active=false};
 },[file.id,file.image,file.unavailable]);
 return url?<img className="draft-thumbnail" src={url} alt=""/>:<Icon name="paperclip"/>;
}

function draftSize(bytes:number):string {
 if(bytes<1024)return `${bytes} B`;
 const divisor=bytes<1024*1024?1024:1024*1024;
 return `${new Intl.NumberFormat(undefined,{maximumFractionDigits:1}).format(bytes/divisor)} ${divisor===1024?'KB':'MB'}`;
}
function attachmentType(type:string,t:(key:MessageKey)=>string):string {
 if(type.startsWith('image/'))return t('chat.imageFile');
 if(type==='application/pdf')return t('chat.pdfFile');
 if(type.startsWith('text/'))return t('chat.textFile');
 return t('chat.fileTypeUnknown');
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
    {(value.questions ?? []).map(q => <label className="question" key={q.id}>{q.title}{q.required && <span aria-label={t('chat.required')}> *</span>}
      {q.type === 'select' || q.type === 'boolean' ? <select disabled={!enabled} value={answers[q.id]?.[0] ?? ''} onChange={e => setAnswers({ ...answers, [q.id]:[e.target.value] })}>
        <option value="">{t('chat.approvalSelectPlaceholder')}</option>
        {q.type === 'boolean' ? <><option value="true">{t('chat.yes')}</option><option value="false">{t('chat.no')}</option></> : q.options.map(o => <option key={o.id} value={o.id}>{o.label}</option>)}
      </select> : <><input type={q.secret ? 'password' : q.type === 'number' || q.type === 'integer' ? 'number' : 'text'} disabled={!enabled} value={answers[q.id]?.[0] ?? ''} onChange={e => setAnswers({ ...answers, [q.id]:[e.target.value] })} />
        {q.options.length > 0 && <div className="question-options">{q.options.map(o => <button key={o.id} disabled={!enabled} onClick={() => setAnswers({ ...answers, [q.id]:[o.id] })}>{o.label}</button>)}</div>}</>}
    </label>)}
    {error && <p className="inline-error" role="alert">{error}</p>}
    {value.status === 'pending' ? <>
      <div className="approval-choices">{value.choices.map(c => c.scope === 'rule' || c.scope === 'conversation' ?
        <div key={c.id} className="permission-option"><p>{c.scope === 'rule' ? t('chat.approvalRuleScope') : t('chat.approvalConversationScope')}</p>{c.details && <pre>{c.details}</pre>}{button(c)}</div> : button(c))}</div>
    </> : <p className="quiet approval-result" role="status" aria-label={resultLabel}>{value.status==='resolved'&&value.resolution?.outcome==='allowed'?<CheckIcon aria-hidden="true" size={16}/>:value.status==='resolved'&&(value.resolution?.outcome==='declined'||value.resolution?.outcome==='cancelled')?<XIcon aria-hidden="true" size={16}/>:null}<span>{resultLabel}</span></p>}
  </section>;
}
type MessageProps={ item: Item; report: (message: string) => void; animate?:boolean; reveal?:boolean; clip?:PortraitClip };
const Message=memo(function Message({ item, report, animate=false, reveal=false, clip }: MessageProps) {
  const {t} = useI18n();
  const statusLabel = getItemStatusLabel(item.status, t);
  return <article data-message-id={item.id} className={`message-row ${item.kind}`}>
   {item.kind==='assistant'&&<BotAvatar animate={animate} clip={clip}/>}
   <div className={`message ${item.kind}`}>
    {item.kind==='user'&&item.screen ? <ScreenMessage value={item.screen} note={item.text} report={report}/> : item.kind==='user'&&item.media ? <MediaMessage value={item.media} note={item.media.caption??''}/> : item.kind === 'activity' ? <details><summary>{item.text}<span>{statusLabel}</span></summary>{item.details && <pre>{item.details}</pre>}</details> : item.kind === 'assistant' ? <MessageContent key={item.id} text={item.text} report={report} animate={reveal}/> : <p>{item.text}</p>}
    {item.artifacts?.map(file => <button className="artifact" key={file.id} onClick={() => void backend('RevealArtifact',file.id).catch(() => report(t('chat.artifactUnavailable')))}><Icon name="paperclip" />{file.name}<span>{t('chat.revealInFinder')}</span></button>)}
    {!!item.text&&item.kind!=='activity'&&<div className="message-actions"><CopyText text={item.text} report={report}/></div>}
    {item.kind==='user'&&['sending','unknown','rejected'].includes(item.status)&&<small className="outgoing-status" role="status">{statusLabel}</small>}
   </div>
  </article>;
},(before,after)=>before.report===after.report&&before.animate===after.animate&&before.reveal===after.reveal&&
 (before.animate?before.clip===after.clip:true)&&before.item.id===after.item.id&&before.item.kind===after.item.kind&&
 before.item.text===after.item.text&&before.item.status===after.item.status&&
 JSON.stringify(before.item.artifacts)===JSON.stringify(after.item.artifacts)&&
 JSON.stringify(before.item.screen)===JSON.stringify(after.item.screen)&&
 JSON.stringify(before.item.media)===JSON.stringify(after.item.media));

export function useConversation(active: boolean, pet=false, chat=false, composer=false) {
 const [conversation,setConversation]=useState<{snapshot:Snapshot|null;liveReplies:Set<string>}>({snapshot:null,liveReplies:new Set()});
 const observation=useRef(conversation);
 const order=useRef(new ConversationOrder()),botStatus=useRef(''),observing=useRef(active);observing.current=active;
 const refresh=useCallback(async()=>{
  if(!observing.current)return;
  const ticket=order.current.request(),expectedRevision=order.current.revision;
  let next:Snapshot|null;
  if(chat){const update=await backend<ChatUpdate>('ChatSnapshot',expectedRevision,botStatus.current);next=update.changed?update.snapshot:null;}
  else next=await backend<Snapshot>(pet?'PetSnapshot':composer?'ComposerSnapshot':'Snapshot');
  if(!next){order.current.accept(ticket,expectedRevision);return;}
  if(observing.current&&order.current.accept(ticket,next.revision)){
   botStatus.current=next.botStatus;
   observation.current={snapshot:next,liveReplies:liveReplyIDs(observation.current.snapshot,next,observation.current.liveReplies,pet)};
   setConversation(observation.current);
  }
 },[pet,chat,composer]);
 useEffect(()=>{
  order.current.reset();
  if(!active)return;
  observation.current={snapshot:null,liveReplies:new Set()};
  botStatus.current='';
  let stopped=false,timer=0;
  const poll=async()=>{try{await refresh();}catch{/* Preserve confirmed state across a failed observation. */}finally{if(!stopped)timer=window.setTimeout(()=>void poll(),450);}};
  void poll();return()=>{stopped=true;clearTimeout(timer);order.current.reset();};
 },[active,pet,chat,composer,refresh]);
 return {...conversation,refresh};
}

// One native-host draft, two exclusive editors. Writes serialize and use a
// revision fence so a delayed hidden renderer cannot overwrite newer text.
type ComposerProps={snapshot:Snapshot|null;quick?:boolean;active?:boolean;activation?:number;focusRevision?:number;refresh:()=>Promise<void>;onOutgoing?:(item:Item)=>void};
// Each renderer mounts one Composer. UI-initiated close and editor switch await
// its write barrier before asking the native host to change windows.
let flushVisibleComposer=async()=>{};
const Composer=memo(function Composer({snapshot,quick=false,active=true,activation=0,focusRevision=0,refresh,onOutgoing}:ComposerProps) {
 const {t} = useI18n();
 const draftLoadFailed=useEffectEvent(()=>t('chat.draftLoadFailed'));
 const input=useRef<HTMLTextAreaElement>(null),send=useRef<HTMLButtonElement>(null),add=useRef<HTMLButtonElement>(null),composer=useRef<HTMLDivElement>(null);
 const [draft,setDraft]=useState(''),[refs,setRefs]=useState<string[]>([]),[files,setFiles]=useState<DraftFile[]>([]);
 const [busy,setBusy]=useState(false),[expanded,setExpanded]=useState(false),[error,setError]=useState(''),[loaded,setLoaded]=useState(false);
 const [sendBlocked,setSendBlocked]=useState(false),[cleanupPending,setCleanupPending]=useState(false),[syncReadFailed,setSyncReadFailed]=useState(false),[filesLoaded,setFilesLoaded]=useState(false);
 const [dragging,setDragging]=useState(false),[feedback,setFeedback]=useState<'duplicate'|''>('');
 const visible=useRef(active);visible.current=active;
 useEffect(()=>{setExpanded(false);},[active,activation]);
 const pending=useRef<{request:Submission;outgoing:Item;progress:SubmissionProgress;generation:number;draftRevision:number}|null>(null);
 const lifetime=useRef(0),working=useRef(false);
 const fileOrder=useRef(new FileObservationOrder()),editGeneration=useRef(0),unsavedAfterAccepted=useRef(false),stopFileRead=useRef<()=>void>(()=>{});
 const saved=useRef<Draft>({revision:0,text:'',referenceIds:[],notice:''});
 const writes=useRef(new DraftQueue()), conflicted=useRef(false),composing=useRef(false);
 const registration=useRef<Promise<void>|null>(null);
 const register=()=>{
  if(!registration.current){
   const attempt=desktop('RegisterDraftEditor',quick?'panel':'history');
   registration.current=attempt;
   void attempt.catch(()=>{if(registration.current===attempt)registration.current=null;});
  }
  return registration.current;
 };
 useEffect(()=>{
  const flush=async()=>{
   if(composing.current||unsavedAfterAccepted.current)throw new Error('draft is not ready for handoff');
   await writes.current.flush();
   if(conflicted.current)throw new Error('draft save conflict');
  };
  const handoff=(event:Event)=>{
   const id=(event as CustomEvent<number>).detail;
   if(!Number.isSafeInteger(id)||id<=0)return;
   void flush().then(()=>desktop('ConfirmDraftFlush',id,true),()=>desktop('ConfirmDraftFlush',id,false)).catch(()=>{});
  };
  flushVisibleComposer=flush;
  window.addEventListener('draft-flush-request',handoff);
  return()=>{window.removeEventListener('draft-flush-request',handoff);if(flushVisibleComposer===flush)flushVisibleComposer=async()=>{};};
 },[]);
 const flushDraft=useEffectEvent(async()=>{
  try{await writes.current.flush();}
  catch{setError(t('chat.draftSaveFailed'));}
 });
 useEffect(()=>{
  // Native close hides reusable WebViews. Flush on the host event and focus
  // loss, independently of React's activation cleanup.
  const closing=quick?'panel-close':'history-close';
  const flush=()=>{void flushDraft();};
  window.addEventListener(closing,flush);
  window.addEventListener('blur',flush);
  window.addEventListener('pagehide',flush);
  return()=>{window.removeEventListener(closing,flush);window.removeEventListener('blur',flush);window.removeEventListener('pagehide',flush);};
 },[quick]);
 useEffect(()=>{
  lifetime.current++;working.current=false;pending.current=null;setBusy(false);
  setLoaded(false);setFilesLoaded(false);setSendBlocked(false);setCleanupPending(false);setSyncReadFailed(false);fileOrder.current.reset();
  const readFiles=()=>{stopFileRead.current();const ticket=fileOrder.current.request();setFilesLoaded(false);stopFileRead.current=retryRead(()=>desktop<DraftFile[]>('DraftFiles'),value=>{if(!fileOrder.current.accept(ticket))return;const visible=visibleDraftFiles(value,saved.current);setFiles(visible);setFilesLoaded(true);setError(previous=>previous===t('chat.fileSelectionReadFailed')?(saved.current.cleanupPending?t('chat.acceptedCleanupPending'):saved.current.rejectedCleanupPending?t('chat.rejectedCleanupPending'):saved.current.pendingSend?t('chat.originalAttachmentPending'):saved.current.notice):previous);if(!visible.length)setFeedback('');},()=>setError(previous=>previous||t('chat.fileSelectionReadFailed')));};
  const stopDraft=retryRead(async()=>{
   await register();
   await writes.current.flush();
   await desktop('FlushOtherDraft',quick?'panel':'history');
   return backend<Draft>('Draft');
  },d=>{saved.current=d;conflicted.current=false;unsavedAfterAccepted.current=false;setDraft(d.text);setRefs(d.referenceIds??[]);setFiles(old=>visibleDraftFiles(old,d));setError(d.cleanupPending?t('chat.acceptedCleanupPending'):d.rejectedCleanupPending?t('chat.rejectedCleanupPending'):d.pendingSend?t('chat.originalAttachmentPending'):d.notice);setSendBlocked(!!(d.pendingSend||d.rejectedCleanupPending));setCleanupPending(!!(d.cleanupPending||d.rejectedCleanupPending));setLoaded(true);readFiles();if(visible.current)input.current?.focus();},()=>setError(draftLoadFailed()));
  const changed=(event:Event)=>{
   const detail=(event as CustomEvent<string|{error:string;added:number}>).detail;
   readFiles();setDragging(false);
   const failure=typeof detail==='string'?detail:detail?.error??'';
   setError(previous=>previous===t('chat.sentDraftSyncFailed')?previous:failure||(saved.current.cleanupPending?t('chat.acceptedCleanupPending'):saved.current.rejectedCleanupPending?t('chat.rejectedCleanupPending'):saved.current.pendingSend?t('chat.originalAttachmentPending'):saved.current.notice));setFeedback('');
  };
  window.addEventListener('files-changed',changed);
  return()=>{void flushDraft();lifetime.current++;stopDraft();stopFileRead.current();fileOrder.current.reset();window.removeEventListener('files-changed',changed);};
 },[activation]);
 useEffect(()=>{if(active&&loaded&&!busy){input.current?.focus({preventScroll:true});if(quick){const frame=requestAnimationFrame(()=>void desktop('PanelReady',activation));return()=>cancelAnimationFrame(frame);}}},[active,loaded,busy,activation,focusRevision]);
 useEffect(()=>{
  if(!quick||!active||!loaded)return;
  const focus=()=>input.current?.focus();
  window.addEventListener('panel-focus',focus);window.addEventListener('focus',focus);
  return()=>{window.removeEventListener('panel-focus',focus);window.removeEventListener('focus',focus);};
 },[quick,active,loaded]);
 const save=(text:string,referenceIds:string[])=>{
  editGeneration.current++;
  setDraft(text);setRefs(referenceIds);
  if(syncReadFailed){unsavedAfterAccepted.current=true;setError(t('chat.sentDraftSyncFailed'));return;}
  if(composing.current)return;
  void writes.current.enqueue(async()=>{
   if(conflicted.current)return;
   try{saved.current=await backend<Draft>('SaveDraft',{revision:saved.current.revision,text,referenceIds});}
   catch(e){conflicted.current=true;setError(e instanceof Error?e.message:t('chat.draftSaveFailed'));throw e;}
  });
 };
 useLayoutEffect(()=>{const editor=input.current;if(editor){editor.style.height='0px';editor.style.height=`${Math.max(27,Math.min(127,editor.scrollHeight))}px`;}},[draft]);
 const pick=async()=>{setExpanded(false);setBusy(true);setError('');setFeedback('');const ticket=fileOrder.current.request();try{const selected=visibleDraftFiles(await desktop<DraftFile[]>('PickFiles'),saved.current);if(fileOrder.current.accept(ticket)){setFiles(selected);setFilesLoaded(true);}setExpanded(false);}catch(e){setError(e instanceof Error?e.message:t('chat.pickFilesFailed'));}finally{setBusy(false);input.current?.focus();}};
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
   else if(text){save(draft.slice(0,start)+text+draft.slice(end),refs);requestAnimationFrame(()=>input.current?.setSelectionRange(start+text.length,start+text.length));}
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
    if(editTicket===editGeneration.current){setDraft(next.text);setRefs(next.referenceIds??[]);}
    const filesCurrent=fileOrder.current.accept(fileTicket),visible=visibleDraftFiles(nextFiles,next);
    if(filesCurrent){setFiles(visible);setFilesLoaded(true);setFeedback('');}
    setError(next.cleanupPending?t('chat.acceptedCleanupPending'):next.rejectedCleanupPending?t('chat.rejectedCleanupPending'):next.pendingSend?t('chat.originalAttachmentPending'):next.notice);setSendBlocked(!!(next.pendingSend||next.rejectedCleanupPending));setCleanupPending(!!(next.cleanupPending||next.rejectedCleanupPending));setSyncReadFailed(false);setLoaded(true);
    if(quick&&filesCurrent&&!next.notice&&!next.cleanupPending&&!next.text&&!next.referenceIds?.length&&!visible.length)await desktop('ClosePanel').catch(()=>{});
  });}catch{
   if(lifetime.current===attempt.generation&&pending.current===attempt){
    setDraft(current=>current===attempt.request.text?'':current);
    setRefs(current=>current.length===attempt.request.referenceIds.length&&current.every((id,index)=>id===attempt.request.referenceIds[index])?[]:current);
    setFiles(current=>current.filter(file=>!attempt.request.fileIds.includes(file.id)));
    setFeedback('');setLoaded(true);setSendBlocked(true);setSyncReadFailed(true);setError(t('chat.sentDraftSyncFailed'));
   }
  }
 };
 const retryLocalCleanup=async()=>{
  stopFileRead.current();
  const generation=lifetime.current,fileTicket=fileOrder.current.request(),editTicket=editGeneration.current;
  try{
   await writes.current.flush();
   let next=await backend<Draft>('Draft');
   if(generation!==lifetime.current)return;
   if(editTicket!==editGeneration.current){setError(t('chat.sentDraftSyncFailed'));return;}
   if(syncReadFailed&&unsavedAfterAccepted.current&&next.pendingSend){setError(t('chat.sentDraftSyncFailed'));return;}
   if(syncReadFailed&&unsavedAfterAccepted.current&&!next.pendingSend){
    const local=await backend<Draft>('SaveDraft',{revision:next.revision,text:draft,referenceIds:refs});
    saved.current=local;unsavedAfterAccepted.current=false;
    next=await backend<Draft>('Draft');
    if(generation!==lifetime.current||editTicket!==editGeneration.current)return;
   }else if(!unsavedAfterAccepted.current){setDraft(next.text);setRefs(next.referenceIds??[]);}
   saved.current=next;
   const nextFiles=await desktop<DraftFile[]>('DraftFiles');
   if(generation!==lifetime.current)return;
   if(fileOrder.current.accept(fileTicket)){const visible=visibleDraftFiles(nextFiles,next);setFiles(visible);setFilesLoaded(true);if(!visible.length)setFeedback('');}
   setError(next.cleanupPending?t('chat.acceptedCleanupPending'):next.rejectedCleanupPending?t('chat.rejectedCleanupPending'):next.pendingSend?t('chat.originalAttachmentPending'):next.notice);setSendBlocked(!!(next.pendingSend||next.rejectedCleanupPending));setCleanupPending(!!(next.cleanupPending||next.rejectedCleanupPending));setSyncReadFailed(false);conflicted.current=false;
  }catch{if(generation===lifetime.current){setSendBlocked(true);setError(t(syncReadFailed||cleanupPending||pending.current?.progress.outcome==='accepted'?'chat.sentDraftSyncFailed':'chat.fileSelectionReadFailed'));}}
 };
 const submit=async()=>{
  if(working.current||busy||!loaded||!filesLoaded||sendBlocked||(cleanupPending&&files.length>0)||!canSubmit(snapshot)||(!draft.trim()&&!files.length))return;
  working.current=true;setBusy(true);setError('');setExpanded(false);
  const request:Submission={id:crypto.randomUUID(),text:draft,fileIds:files.map(f=>f.id),referenceIds:refs};
  const outgoing:Item={id:`outgoing:${request.id}`,requestId:request.id,turnKey:'',kind:'user',text:[draft,...files.map(f=>f.name)].filter(Boolean).join('\n'),status:'sending',details:'',activity:null,artifacts:[]};
  const attempt={request,outgoing,progress:new SubmissionProgress(),generation:lifetime.current,draftRevision:saved.current.revision};
  pending.current=attempt;onOutgoing?.(outgoing);
  try{
   try{await writes.current.flush();}
   catch{attempt.progress.observe('rejected');onOutgoing?.({...outgoing,status:'rejected'});return;}
   if(lifetime.current!==attempt.generation)return;
   attempt.draftRevision=saved.current.revision;
   if(conflicted.current){attempt.progress.observe('rejected');onOutgoing?.({...outgoing,status:'rejected'});return;}
   let receipt:Receipt;
   try{receipt=await backend<Receipt>('Submit',request);}
   catch{receipt={id:request.id,outcome:'unknown',message:t('chat.sendPendingChat')};}
   if(lifetime.current!==attempt.generation)return;
   const outcome=attempt.progress.observe(receipt.outcome);
   onOutgoing?.({...outgoing,status:outcome});
   if(outcome==='accepted')await accepted(attempt);
   else setError(receipt.message||t('chat.sendPending'));
  }finally{
   // Keep the disabled state until the authoritative send/stop action is fresh.
   try{await refresh();}catch{/* A read failure does not change the send receipt. */}
   if(lifetime.current===attempt.generation&&pending.current===attempt){working.current=false;setBusy(false);}
  }
 };
 useEffect(()=>{
  const attempt=pending.current;
  if(attempt&&snapshot?.lastReceipt.id===attempt.request.id&&snapshot.lastReceipt.outcome==='accepted')void accepted(attempt);
 },[snapshot?.lastReceipt.id,snapshot?.lastReceipt.outcome]);
 const primaryAction=composerAction(snapshot,quick,!!(draft.trim()||files.length||refs.length));
 const stopping=snapshot?.phase==='interrupting';
 const enabled=loaded&&!busy&&(primaryAction==='stop'?!stopping:filesLoaded&&!sendBlocked&&!(cleanupPending&&files.length>0)&&canSubmit(snapshot)&&!!(draft.trim()||files.length));
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
 const needsConnection=loaded&&!!(draft.trim()||files.length)&&snapshot?.connection!=='ready'&&!snapshot?.canSteer;
 return <div ref={composer} className={`compose-area${dragging?' file-dragging':''}`} data-file-drop-target
  onDragEnter={e=>{if(e.dataTransfer.types.includes('Files'))setDragging(true)}}
  onDragOver={e=>{if(e.dataTransfer.types.includes('Files')){e.preventDefault();setDragging(true)}}}
  onDragLeave={e=>{if(!e.currentTarget.contains(e.relatedTarget as Node))setDragging(false)}}
  onDrop={()=>setDragging(false)}>
  <div className="capsule">
   <button ref={add} className="icon-button add" disabled={busy||!loaded} onClick={()=>setExpanded(!expanded)} aria-label={t('chat.addAttachmentOrReference')} aria-expanded={expanded} aria-haspopup="menu" aria-controls={expanded?'attachment-menu':undefined}><Icon name="plus"/></button>
   <textarea aria-label={t('chat.composerLabel')} ref={input} rows={1} value={draft} disabled={!loaded||busy} onChange={e=>save(e.target.value,refs)} onCompositionStart={()=>{composing.current=true;}} onCompositionEnd={e=>{composing.current=false;save(e.currentTarget.value,refs);}} onPaste={e=>void paste(e)} onKeyDown={e=>handleComposerKey(e.nativeEvent,send.current,primaryAction)} placeholder={snapshot?.canSteer&&snapshot.maintenance!=='dreaming'?t('chat.steerPlaceholder'):t('chat.composerPlaceholder')} title={t('chat.composerKeyHint')}/>
   <button ref={send} className="icon-button send" disabled={!enabled} onClick={()=>void (primaryAction==='stop'?interrupt():submit())} aria-label={actionLabel} title={needsConnection?t('chat.connectionUnavailableToSend'):actionLabel}>{primaryAction==='stop'?<span className="composer-stop" aria-hidden="true"/>:<Icon name="arrow.up"/>}</button>
  </div>
  {!!error&&<p role="alert" className="input-error">{error}</p>}
  {!error&&needsConnection&&<div className="composer-connection-hint" role="status"><span>{t('chat.connectionUnavailableToSend')}</span>{quick&&<button className="text-action" onClick={()=>void desktop('OpenRuntimeSettings')}>{t('chat.connectionSettings')}</button>}</div>}
  {(sendBlocked||cleanupPending||loaded&&!filesLoaded)&&<button className="quiet" type="button" onClick={()=>void retryLocalCleanup()}>{t('chat.retryDraftCleanup')}</button>}
  {!error&&(dragging||feedback||files.length>0)&&<p role="status" className="input-feedback">{dragging?t('chat.dropAttachments'):feedback==='duplicate'?t('chat.attachmentAlreadyAdded'):t('chat.attachmentsSelected',{count:files.length})}</p>}
  {!!(files.length||refs.length)&&<ul className="attachments" aria-label={t('chat.attachmentsLabel')}>
   {files.map(f=><li key={f.id} className={f.unavailable?'attachment-unavailable':''}><DraftThumbnail file={f}/><span className="attachment-details" title={f.name}><strong>{f.name}{f.unavailable?t('chat.attachmentUnavailableSuffix'):''}</strong><small>{attachmentType(f.type,t)} · {draftSize(f.size)}</small></span><button disabled={busy} aria-label={t('chat.removeAttachment',{name:f.name})} onClick={()=>{const ticket=fileOrder.current.request();void desktop<DraftFile[]>('RemoveFile',f.id).then(value=>{if(fileOrder.current.accept(ticket)){setFiles(visibleDraftFiles(value,saved.current));setFilesLoaded(true);setFeedback('');}}).catch(()=>setError(t('chat.attachmentUpdateFailed')));}}><Icon name="xmark"/></button></li>)}
   {refs.map(id=><li key={id}><span>{snapshot?.references.find(r=>r.id===id)?.name??t('chat.referenceDefault')}</span><button disabled={busy} aria-label={t('chat.removeReference')} onClick={()=>save(draft,refs.filter(v=>v!==id))}><Icon name="xmark"/></button></li>)}
  </ul>}
  {expanded&&<AttachmentMenu trigger={add} composer={composer} quick={quick} activation={activation} references={snapshot?.references??[]} selected={refs}
   onClose={()=>setExpanded(false)} onPick={()=>void pick()} onError={()=>setError(t('chat.menuOpenFailed'))}
   onSelect={id=>{save(draft,[...refs,id]);setExpanded(false);input.current?.focus();}}/>}
 </div>;
},(a,b)=>a.quick===b.quick&&a.active===b.active&&a.activation===b.activation&&a.focusRevision===b.focusRevision&&
 a.refresh===b.refresh&&a.onOutgoing===b.onOutgoing&&sameComposerSnapshot(a.snapshot,b.snapshot));

export function Panel() {
 const {t} = useI18n();
 const [active,setActive]=useState(false),[activation,setActivation]=useState(0);const surface=useRef<HTMLElement>(null);
 const {snapshot,refresh}=useConversation(active,false,false,true);
 useEffect(()=>{
  const open=(event:Event)=>{setActivation((event as CustomEvent<{activation:number}>).detail?.activation??0);setActive(true);},close=()=>setActive(false);
  const key=(e:KeyboardEvent)=>{if(!e.isComposing&&(e.key==='Escape'||(e.metaKey&&e.key==='w'))){e.preventDefault();void flushVisibleComposer().then(()=>desktop('ClosePanel')).catch(()=>{});}};
  window.addEventListener('panel-open',open);window.addEventListener('panel-close',close);window.addEventListener('keydown',key);
  return()=>{window.removeEventListener('panel-open',open);window.removeEventListener('panel-close',close);window.removeEventListener('keydown',key);};
 },[]);
 useEffect(()=>{const resize=new ResizeObserver(()=>{if(surface.current)void desktop('SetPanelHeight',Math.max(64,Math.min(500,Math.ceil(surface.current.getBoundingClientRect().height))));});resize.observe(surface.current!);return()=>resize.disconnect();},[]);
 return <main ref={surface} className="input-surface" aria-label={t('chat.panelAriaLabel')}><Composer snapshot={snapshot} quick active={active} activation={activation} refresh={refresh}/>{active&&snapshot&&!canSubmit(snapshot)&&<button className="text-action" onClick={()=>void flushVisibleComposer().then(()=>desktop('OpenHistory')).catch(()=>{})}>{t('chat.openChatToReview')}</button>}</main>;
}

export function History() {
 const {t} = useI18n();
 const [active,setActive]=useState(false),[error,setError]=useState(''),[busy,setBusy]=useState(false),[unread,setUnread]=useState(false);
 const [connectionError,setConnectionError]=useState('');
 const [activation,setActivation]=useState(0);
 const {snapshot,liveReplies,refresh}=useConversation(active,false,true);
 const [outgoing,setOutgoing]=useState<Item[]>([]);
 const stage=useCallback((item:Item)=>{if(item.status==='sending'){position.current?.latest();setUnread(false);}setOutgoing(previous=>{const prior=previous.find(p=>p.requestId===item.requestId);return [...previous.filter(p=>p.requestId!==item.requestId),prior?.status==='accepted'?prior:item];});},[]);
 useEffect(()=>{const known=new Set(snapshot?.items.map(i=>i.requestId).filter(Boolean));setOutgoing(previous=>previous.filter(i=>!known.has(i.requestId)));},[snapshot]);
 const [earlierBusy,setEarlierBusy]=useState(false);
 const prepend=useRef<{id:string;top:number}|null>(null);
 const scroll=useRef<HTMLDivElement>(null),content=useRef<HTMLDivElement>(null),position=useRef<ChatScroll|null>(null);
 useLayoutEffect(()=>{position.current=new ChatScroll(scroll.current!);return()=>{position.current=null;};},[]);
 useEffect(()=>{
  let activeNow=false;
  const opened=()=>{activeNow=true;setActive(true);setActivation(value=>value+1);},visible=()=>{if(!activeNow)setActivation(value=>value+1);activeNow=true;setActive(true);},closed=()=>{activeNow=false;setActive(false);};
  let mounted=true;
  const stopVisibility=retryRead(()=>desktop<boolean>('HistoryVisible'),v=>{if(mounted&&v)visible();});
  const key=(e:KeyboardEvent)=>{if(!e.isComposing&&(e.key==='Escape'||(e.metaKey&&e.key==='w'))){e.preventDefault();void flushVisibleComposer().then(()=>desktop('CloseHistory')).catch(()=>{});}};
  window.addEventListener('history-open',opened);window.addEventListener('history-visible',visible);window.addEventListener('history-close',closed);window.addEventListener('keydown',key);
  return()=>{mounted=false;stopVisibility();window.removeEventListener('history-open',opened);window.removeEventListener('history-visible',visible);window.removeEventListener('history-close',closed);window.removeEventListener('keydown',key);};
 },[]);
 useLayoutEffect(()=>{
  if(!active)return;
  prepend.current=null;position.current!.latest();setUnread(false);
 },[active,activation]);
 useLayoutEffect(()=>{
  if(!active)return;
  const resize=new ResizeObserver(()=>{position.current?.layout();});
  resize.observe(scroll.current!);resize.observe(content.current!);
  return()=>resize.disconnect();
 },[active]);
 const messages=useMemo(()=>withOutgoing(snapshot?.items??[],outgoing).filter(i=>i.kind==='user'||(i.kind==='assistant'&&(i.text.trim()||i.artifacts?.length))),[snapshot?.items,outgoing]);
 const contentKey=useMemo(()=>messages.map(i=>i.id+i.text).join(''),[messages]);
 const prompts=snapshot?.approvals.filter(p=>p.status!=='resolved')??[];
 const settled=prompts.length?[]:snapshot?.approvals.filter(p=>p.status==='resolved'&&p.turnKey===snapshot.currentTurn).slice(-1)??[];
 const approvalCards=[...prompts,...settled];
 const promptKey=approvalCards.map(p=>p.id+p.status+p.resolution?.choiceId).join('');
 const activity=chatActivity(snapshot);
 const avatar=useAvatarPresentation(snapshot,active);
 const activeReply=active?animatedReplyID(snapshot,avatar.completion):null;
 const connection=snapshot&&snapshot.connection!=='ready';
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
  snapshot?.message||t('chat.offlineConnection');
 useLayoutEffect(()=>{
  const el=scroll.current;if(!el||!active)return;
  if(prepend.current){
   if(earlierBusy)return;
   const anchor=el.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(prepend.current.id)}"]`);
   if(anchor)el.scrollTop+=anchor.getBoundingClientRect().top-prepend.current.top;
   prepend.current=null;
  }else if(position.current!.following){position.current!.layout();setUnread(false);}else setUnread(true);
 },[contentKey,promptKey,activity,snapshot?.connection,snapshot?.phase,snapshot?.message,snapshot?.hasEarlier,earlierBusy]);
 const earlier=async()=>{if(earlierBusy)return;setEarlierBusy(true);setError('');const el=scroll.current!;position.current!.following=false;const anchor=Array.from(el.querySelectorAll<HTMLElement>('[data-message-id]')).find(item=>item.getBoundingClientRect().bottom>el.getBoundingClientRect().top);if(anchor)prepend.current={id:anchor.dataset.messageId!,top:anchor.getBoundingClientRect().top};try{await backend('LoadEarlier');await refresh();}catch{prepend.current=null;setError(t('chat.loadEarlierFailed'));}finally{setEarlierBusy(false);}};
 const action=async(method:string)=>{setBusy(true);setError('');setConnectionError('');try{await backend(method);}catch(e){setConnectionError(e instanceof Error?e.message:t('chat.actionFailed'));}finally{setBusy(false);try{await refresh();}catch{setConnectionError(previous=>previous||t('chat.actionFailed'));}}};
 return <main className="history-surface" aria-label={t('chat.historyAriaLabel')}>
  <div className="chat-scroll" ref={scroll} onScroll={()=>{if(active&&position.current?.scrolled())setUnread(false);}}>
   <div className="chat-content" ref={content}>
   {!messages.length&&!connection&&!activity&&!prompts.length&&<p className="empty-conversation">{t('chat.emptyConversation')}</p>}
   {snapshot?.hasEarlier&&<div className="history-pagination"><button className="text-action" disabled={earlierBusy} onClick={()=>void earlier()}>{earlierBusy?t('common.loading'):t('chat.loadEarlier')}</button></div>}
   <div className="history-messages">{messages.map(i=><Message key={i.requestId||i.id} item={i} report={setError} animate={i.id===activeReply} clip={i.id===activeReply?(i.id===avatar.completion?'delight':avatar.clip):'companion'} reveal={active&&liveReplies.has(i.id)}/>)}</div>
   {activity&&<WorkingMessage activity={activity} active={active} tool={snapshot?.activity} clip={avatar.clip}/>}
   {(!!approvalCards.length||connection||!!snapshot?.message||snapshot?.phase==='unknown')&&<article className="message-row assistant state-message">
    <BotAvatar animate={active&&prompts.length>0&&!connection} clip={avatar.clip}/>
    <div className={`message assistant state-bubble ${approvalCards.length?'approval-bubble':''}`}>
   {approvalCards.map(p=><Prompt key={p.id} value={p} refresh={()=>void refresh().catch(()=>{})}/>)}
   {connection&&<section className="connection-card" aria-label={t('chat.connectionCardAriaLabel')}>
    <strong>{snapshot.connection==='connecting'?t('chat.connectingTitle'):snapshot.connection==='login'?t('chat.loginTitle'):chooseRuntime?t('chat.chooseRuntimeTitle'):setup?t('chat.setupTitle'):t('chat.reconnectTitle')}</strong>
    <p>{connectionText}</p>
    <div className="connection-actions">
     {snapshot.connection==='login'&&!snapshot.loginPending&&<button disabled={busy} onClick={()=>void action('Login')}>{t('chat.loginInBrowser')}</button>}
     {snapshot.loginPending&&<button disabled={busy} onClick={()=>void action('CancelLogin')}>{t('chat.cancelLogin')}</button>}
     {!snapshot.loginPending&&!chooseRuntime&&snapshot.connection!=='connecting'&&<button disabled={busy} onClick={()=>void action('Connect')}>{busy?t('chat.connectingTitle'):setup?t('chat.recheckSetup'):t('chat.reconnect')}</button>}
     {setup&&<button className="text-action" onClick={()=>void action('OpenConnectionHelp')}>{t('chat.setupHelp')}</button>}
     {<button className={chooseRuntime?'primary':'text-action'} onClick={()=>void desktop('OpenRuntimeSettings')}>{t('chat.connectionSettings')}</button>}
    </div>
   </section>}
   {!connection&&!!snapshot?.message&&<p className="connection-message" role="status">{snapshot.message}</p>}
   {!connection&&snapshot?.phase==='unknown'&&<div className="connection-actions">
    <button disabled={busy} onClick={()=>void action('Connect')}>{t('chat.reconnect')}</button>
   </div>}
    </div>
   </article>}
   {!!(error||connectionError)&&<p className="inline-error" role="alert">{error||connectionError}</p>}
   </div>
  </div>
  {unread&&<button className="new-messages" onClick={()=>{position.current!.latest();setUnread(false);}}>{t('chat.viewNewMessages')}</button>}
  <footer>
   {active&&<Composer snapshot={snapshot} focusRevision={activation} refresh={refresh} onOutgoing={stage}/>}
  </footer>
 </main>;
}
