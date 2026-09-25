import { useEffectEvent, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { backend, desktop, type DraftFile } from './desktop';
import type { Approval, ChatUpdate, Decision, Draft, Item, Receipt, Review, Snapshot, Submission } from './backend/contract';
import { handleComposerKey } from './composer-keyboard';
import { CopyText, MessageContent } from './MessageContent';
import { activeReplyID, canSubmit, chatActivity, composerAction, withOutgoing } from './chat-presentation';
import { WorkingMessage } from './WorkingMessage';
import { BotAvatar } from './BotAvatar';
import { AttachmentMenu } from './AttachmentMenu';
import { ChatScroll } from './chat-scroll';
import { useI18n } from './i18n';
import { approvalChoice, approvalText, approvalTitle } from './approval-presentation';
import type { MessageKey } from './i18n/catalogs';

function Icon({ name }: { name: string }) { return <img className="symbol" src={`/icons/${name}.png`} alt="" />; }

export function getReviewLabel(status: string, t: (key: MessageKey) => string): string {
 switch (status) {
  case 'inProgress': return t('chat.reviewInProgress');
  case 'denied': return t('chat.reviewDenied');
  case 'timedOut': return t('chat.reviewTimedOut');
  case 'aborted': return t('chat.reviewAborted');
  default: return t('chat.reviewPending');
 }
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

function ReviewNotice({value}:{value:Review}) {
 const {t} = useI18n();
 const statusText = getReviewLabel(value.status, t);
 return <section className="review-notice" aria-label={statusText}>
  <strong>{statusText}</strong>
  {value.rationale&&<p>{value.rationale}</p>}
  {value.action&&<details><summary>{t('chat.reviewActionDetails')}</summary><pre>{value.action}</pre></details>}
 </section>;
}

export function Prompt({ value, refresh }: { value: Approval; refresh: () => void }) {
  const {t,locale} = useI18n();
  const title = approvalTitle(value,locale);
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
  const advanced = value.choices.filter(c => c.scope === 'rule' || c.scope === 'conversation');
  const immediate = value.choices.filter(c => !advanced.includes(c));
  const button = (c: typeof value.choices[number]) => <button key={c.id} className={c.scope === 'once' || c.id === 'allow' || c.id === 'answer' || c.id === 'accept' ? 'primary-decision' : ''} disabled={!enabled} onClick={() => void decide(c.id)}>{approvalChoice(c,locale)}</button>;
  return <section className="approval" aria-label={title}>
    <p className="event-eyebrow">{t('chat.approvalEyebrow')}</p>
    <h3>{title}</h3>
    {value.taskTitle && <p>{t('chat.approvalTask',{name:value.taskTitle})}</p>}
    {value.noticeKey && <p>{approvalText(locale,value.noticeKey,'')}</p>}
    {value.description && <p>{value.description}</p>}
    {value.action && <pre className="approval-action">{value.action}</pre>}
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
      <div className="approval-choices">{immediate.map(button)}</div>
      {advanced.length > 0 && <details className="advanced-permission"><summary>{t('chat.approvalAdvanced')}</summary>{advanced.map(c => <div key={c.id} className="permission-option"><p>{c.scope === 'rule' ? t('chat.approvalRuleScope') : t('chat.approvalConversationScope')}</p>{c.details && <pre>{c.details}</pre>}{button(c)}</div>)}</details>}
    </> : <p className="quiet" role="status">{value.status === 'resolved' ? t('chat.approvalResolved') : value.status === 'sent' || value.status === 'sending' ? t('chat.approvalSent') : t('chat.approvalUnknown')}</p>}
  </section>;
}
function Message({ item, report, animate=false }: { item: Item; report: (message: string) => void; animate?:boolean }) {
  const {t} = useI18n();
  const statusLabel = getItemStatusLabel(item.status, t);
  return <article data-message-id={item.id} className={`message-row ${item.kind}`}>
   {item.kind==='assistant'&&<BotAvatar animate={animate}/>}
   <div className={`message ${item.kind}`}>
    {item.kind === 'activity' ? <details><summary>{item.text}<span>{statusLabel}</span></summary>{item.details && <pre>{item.details}</pre>}</details> : item.kind === 'assistant' ? <MessageContent text={item.text} report={report}/> : <p>{item.text}</p>}
    {item.artifacts?.map(file => <button className="artifact" key={file.id} onClick={() => void backend('RevealArtifact',file.id).catch(() => report(t('chat.artifactUnavailable')))}><Icon name="paperclip" />{file.name}<span>{t('chat.revealInFinder')}</span></button>)}
    {!!item.text&&item.kind!=='activity'&&<div className="message-actions"><CopyText text={item.text} report={report}/></div>}
    {item.kind==='user'&&['sending','unknown','rejected'].includes(item.status)&&<small className="outgoing-status" role="status">{statusLabel}</small>}
   </div>
  </article>;
}

export function useConversation(active: boolean, pet=false, chat=false, composer=false) {
 const [snapshot,setSnapshot]=useState<Snapshot|null>(null);
 const revision=useRef(0),botStatus=useRef('');
 const read=async()=>{if(chat){const update=await backend<ChatUpdate>('ChatSnapshot',revision.current,botStatus.current);if(!update.changed)return null;return update.snapshot;}return backend<Snapshot>(pet?'PetSnapshot':composer?'ComposerSnapshot':'Snapshot');};
 const accept=(next:Snapshot)=>{if(next.revision>=revision.current){revision.current=next.revision;botStatus.current=next.botStatus;setSnapshot(next);}};
 const refresh=async()=>{const next=await read();if(next)accept(next);};
 useEffect(()=>{
  if(!active)return;
  let stopped=false,timer=0;
  const poll=async()=>{try{const next=await read();if(!stopped&&next)accept(next);}catch{/* Preserve confirmed state across a failed observation. */}finally{if(!stopped)timer=window.setTimeout(()=>void poll(),450);}};
  void poll();return()=>{stopped=true;clearTimeout(timer);};
 },[active,pet,chat,composer]);
 return {snapshot,refresh};
}

// One native-host draft, two exclusive editors. Writes serialize and use a
// revision fence so a delayed hidden renderer cannot overwrite newer text.
function Composer({snapshot,quick=false,active=true,activation=0,focusRevision=0,refresh,onOutgoing}:{snapshot:Snapshot|null;quick?:boolean;active?:boolean;activation?:number;focusRevision?:number;refresh:()=>Promise<void>;onOutgoing?:(item:Item)=>void}) {
 const {t} = useI18n();
 const draftLoadFailed=useEffectEvent(()=>t('chat.draftLoadFailed'));
 const input=useRef<HTMLTextAreaElement>(null),send=useRef<HTMLButtonElement>(null),add=useRef<HTMLButtonElement>(null),composer=useRef<HTMLDivElement>(null);
 const [draft,setDraft]=useState(''),[refs,setRefs]=useState<string[]>([]),[files,setFiles]=useState<DraftFile[]>([]);
 const [busy,setBusy]=useState(false),[expanded,setExpanded]=useState(false),[error,setError]=useState(''),[loaded,setLoaded]=useState(false);
 const visible=useRef(active);visible.current=active;
 useEffect(()=>{setExpanded(false);},[active,activation]);
 const pending=useRef<Submission|null>(null);
 const saved=useRef<Draft>({revision:0,text:'',referenceIds:[],notice:''});
 const writes=useRef<Promise<void>>(Promise.resolve()), conflicted=useRef(false);
 const readFiles=()=>desktop<DraftFile[]>('DraftFiles').then(setFiles);
 useEffect(()=>{
  let mounted=true;
  setLoaded(false);conflicted.current=false;
  void writes.current.then(()=>backend<Draft>('Draft')).then(d=>{if(mounted){saved.current=d;setDraft(d.text);setRefs(d.referenceIds??[]);setError(d.notice);setLoaded(true);if(visible.current)input.current?.focus();}}).catch(()=>setError(draftLoadFailed()));
  void readFiles();
  const changed=(event:Event)=>{void readFiles();setError((event as CustomEvent<string>).detail??'');};
  window.addEventListener('files-changed',changed);
  return()=>{mounted=false;window.removeEventListener('files-changed',changed);};
 },[activation]);
 useEffect(()=>{if(active&&loaded&&!busy){input.current?.focus({preventScroll:true});if(quick){const frame=requestAnimationFrame(()=>void desktop('PanelReady',activation));return()=>cancelAnimationFrame(frame);}}},[active,loaded,busy,activation,focusRevision]);
 useEffect(()=>{
  if(!quick||!active||!loaded)return;
  const focus=()=>input.current?.focus();
  window.addEventListener('panel-focus',focus);window.addEventListener('focus',focus);
  return()=>{window.removeEventListener('panel-focus',focus);window.removeEventListener('focus',focus);};
 },[quick,active,loaded]);
 const save=(text:string,referenceIds:string[])=>{
  setDraft(text);setRefs(referenceIds);
  writes.current=writes.current.then(async()=>{
   if(conflicted.current)return;
   try{saved.current=await backend<Draft>('SaveDraft',{revision:saved.current.revision,text,referenceIds});}
   catch(e){conflicted.current=true;setError(e instanceof Error?e.message:t('chat.draftSaveFailed'));}
  });
 };
 useLayoutEffect(()=>{const editor=input.current;if(editor){editor.style.height='0px';editor.style.height=`${Math.max(27,Math.min(127,editor.scrollHeight))}px`;}},[draft]);
 const pick=async()=>{setExpanded(false);setBusy(true);setError('');try{setFiles(await desktop<DraftFile[]>('PickFiles'));setExpanded(false);}catch(e){setError(e instanceof Error?e.message:t('chat.pickFilesFailed'));}finally{setBusy(false);input.current?.focus();}};
 const submit=async()=>{
  if(busy||!loaded||!canSubmit(snapshot)||(!draft.trim()&&!files.length))return;
  setBusy(true);setError('');setExpanded(false);
  const request:Submission={id:crypto.randomUUID(),text:draft,fileIds:files.map(f=>f.id),referenceIds:refs};
  const outgoing:Item={id:`outgoing:${request.id}`,requestId:request.id,turnKey:'',kind:'user',text:[draft,...files.map(f=>f.name)].filter(Boolean).join('\n'),status:'sending',details:'',artifacts:[]};
  onOutgoing?.(outgoing);
  await writes.current;
  if(conflicted.current){onOutgoing?.({...outgoing,status:'rejected'});setBusy(false);return;}
  pending.current=request;
  try{
   const receipt=await backend<Receipt>('Submit',request);
   onOutgoing?.({...outgoing,status:receipt.outcome||'unknown'});
   if(receipt.outcome==='accepted'){
    pending.current=null;
    saved.current=await backend<Draft>('Draft');setDraft(saved.current.text);setRefs(saved.current.referenceIds??[]);setError(saved.current.notice);await readFiles();
    if(quick)await desktop('ClosePanel');
   }else setError(receipt.message||t('chat.sendPending'));
  }catch{onOutgoing?.({...outgoing,status:'unknown'});setError(t('chat.sendPendingChat'));}
  finally{setBusy(false);await refresh();}
 };
 useEffect(()=>{
  const attempt=pending.current;
  if(attempt&&snapshot?.lastReceipt.id===attempt.id&&snapshot.lastReceipt.outcome==='accepted'){
   pending.current=null;
   void backend<Draft>('Draft').then(d=>{saved.current=d;setDraft(d.text);setRefs(d.referenceIds??[]);setError(d.notice);void readFiles();if(quick)void desktop('ClosePanel');});
  }
 },[snapshot?.lastReceipt.id,snapshot?.lastReceipt.outcome]);
 const primaryAction=composerAction(snapshot,quick,!!(draft.trim()||files.length||refs.length));
 const stopping=snapshot?.phase==='interrupting';
 const enabled=loaded&&!busy&&(primaryAction==='stop'?!stopping:canSubmit(snapshot)&&!!(draft.trim()||files.length));
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
 const actionLabel=primaryAction==='stop'?(stopping?t('chat.stopping'):t('chat.stopWork')):snapshot?.canSteer?t('chat.steerWork'):t('chat.send');
 return <div ref={composer} className="compose-area" data-file-drop-target>
  <div className="capsule">
   <button ref={add} className="icon-button add" disabled={busy||!loaded} onClick={()=>setExpanded(!expanded)} aria-label={t('chat.addAttachmentOrReference')} aria-expanded={expanded} aria-haspopup="menu" aria-controls={expanded?'attachment-menu':undefined}><Icon name="plus"/></button>
   <textarea aria-label={t('chat.composerLabel')} ref={input} rows={1} value={draft} disabled={!loaded||busy} onChange={e=>save(e.target.value,refs)} onKeyDown={e=>handleComposerKey(e.nativeEvent,send.current,primaryAction)} placeholder={snapshot?.canSteer?t('chat.steerPlaceholder'):t('chat.composerPlaceholder')} title={t('chat.composerKeyHint')}/>
   <button ref={send} className="icon-button send" disabled={!enabled} onClick={()=>void (primaryAction==='stop'?interrupt():submit())} aria-label={actionLabel} title={actionLabel}>{primaryAction==='stop'?<span className="composer-stop" aria-hidden="true"/>:<Icon name="arrow.up"/>}</button>
  </div>
  {!!error&&<p role="alert" className="input-error">{error}</p>}
  {!!(files.length||refs.length)&&<ul className="attachments" aria-label={t('chat.attachmentsLabel')}>
   {files.map(f=><li key={f.id} className={f.unavailable?'attachment-unavailable':''}><Icon name="paperclip"/><span title={f.name}>{f.name}{f.unavailable?t('chat.attachmentUnavailableSuffix'):''}</span><button disabled={busy} aria-label={t('chat.removeAttachment',{name:f.name})} onClick={()=>void desktop<DraftFile[]>('RemoveFile',f.id).then(setFiles).catch(()=>setError(t('chat.attachmentUpdateFailed')))}><Icon name="xmark"/></button></li>)}
   {refs.map(id=><li key={id}><span>{snapshot?.references.find(r=>r.id===id)?.name??t('chat.referenceDefault')}</span><button disabled={busy} aria-label={t('chat.removeReference')} onClick={()=>save(draft,refs.filter(v=>v!==id))}><Icon name="xmark"/></button></li>)}
  </ul>}
  {expanded&&<AttachmentMenu trigger={add} composer={composer} quick={quick} activation={activation} references={snapshot?.references??[]} selected={refs}
   onClose={()=>setExpanded(false)} onPick={()=>void pick()} onError={()=>setError(t('chat.menuOpenFailed'))}
   onSelect={id=>{save(draft,[...refs,id]);setExpanded(false);input.current?.focus();}}/>}
 </div>;
}

export function Panel() {
 const {t} = useI18n();
 const [active,setActive]=useState(false),[activation,setActivation]=useState(0);const surface=useRef<HTMLElement>(null);
 const {snapshot,refresh}=useConversation(active,false,false,true);
 useEffect(()=>{
  const open=(event:Event)=>{setActivation((event as CustomEvent<{activation:number}>).detail?.activation??0);setActive(true);},close=()=>setActive(false);
  const key=(e:KeyboardEvent)=>{if(!e.isComposing&&(e.key==='Escape'||(e.metaKey&&e.key==='w'))){e.preventDefault();void desktop('ClosePanel');}};
  window.addEventListener('panel-open',open);window.addEventListener('panel-close',close);window.addEventListener('keydown',key);
  return()=>{window.removeEventListener('panel-open',open);window.removeEventListener('panel-close',close);window.removeEventListener('keydown',key);};
 },[]);
 useEffect(()=>{const resize=new ResizeObserver(()=>{if(surface.current)void desktop('SetPanelHeight',Math.max(64,Math.min(500,Math.ceil(surface.current.getBoundingClientRect().height))));});resize.observe(surface.current!);return()=>resize.disconnect();},[]);
 return <main ref={surface} className="input-surface" aria-label={t('chat.panelAriaLabel')}><Composer snapshot={snapshot} quick active={active} activation={activation} refresh={refresh}/>{active&&snapshot&&!canSubmit(snapshot)&&<button className="text-action" onClick={()=>void desktop('OpenHistory')}>{t('chat.openChatToReview')}</button>}</main>;
}

export function History() {
 const {t} = useI18n();
 const [active,setActive]=useState(false),[error,setError]=useState(''),[busy,setBusy]=useState(false),[unread,setUnread]=useState(false);
 const [activation,setActivation]=useState(0);
 const {snapshot,refresh}=useConversation(active,false,true);
 const [outgoing,setOutgoing]=useState<Item[]>([]);
 const stage=(item:Item)=>setOutgoing(previous=>[...previous.filter(p=>p.requestId!==item.requestId),item]);
 useEffect(()=>{const known=new Set(snapshot?.items.map(i=>i.requestId).filter(Boolean));setOutgoing(previous=>previous.filter(i=>!known.has(i.requestId)));},[snapshot]);
 const [earlierBusy,setEarlierBusy]=useState(false);
 const prepend=useRef<{id:string;top:number}|null>(null);
 const scroll=useRef<HTMLDivElement>(null),content=useRef<HTMLDivElement>(null),position=useRef<ChatScroll|null>(null);
 useLayoutEffect(()=>{position.current=new ChatScroll(scroll.current!);return()=>{position.current=null;};},[]);
 useEffect(()=>{
  const opened=()=>{setActive(true);setActivation(value=>value+1);},visible=()=>setActive(true),closed=()=>setActive(false);
  let mounted=true;
  void desktop<boolean>('HistoryVisible').then(v=>{if(mounted&&v)visible();});
  const key=(e:KeyboardEvent)=>{if(!e.isComposing&&(e.key==='Escape'||(e.metaKey&&e.key==='w'))){e.preventDefault();void desktop('CloseHistory');}};
  window.addEventListener('history-open',opened);window.addEventListener('history-visible',visible);window.addEventListener('history-close',closed);window.addEventListener('keydown',key);
  return()=>{mounted=false;window.removeEventListener('history-open',opened);window.removeEventListener('history-visible',visible);window.removeEventListener('history-close',closed);window.removeEventListener('keydown',key);};
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
 const messages=withOutgoing(snapshot?.items??[],outgoing).filter(i=>i.kind==='user'||(i.kind==='assistant'&&(i.text.trim()||i.artifacts?.length)));
 const contentKey=messages.map(i=>i.id+i.text).join('');
 const prompts=snapshot?.approvals.filter(p=>p.status!=='resolved')??[];
 const promptKey=prompts.map(p=>p.id+p.status).join('');
 const reviews=snapshot?.reviews?.filter(r=>r.status==='denied'||r.status==='timedOut'||r.status==='aborted')??[];
 const activity=chatActivity(snapshot);
 const activeReply=active?activeReplyID(snapshot):null;
 const connection=snapshot&&snapshot.connection!=='ready';
 const setup=snapshot?.connectionIssue==='runtime_missing'||snapshot?.connectionIssue==='runtime_protocol';
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
 const action=async(method:string)=>{setBusy(true);setError('');try{await backend(method);}catch(e){setError(e instanceof Error?e.message:t('chat.actionFailed'));}finally{setBusy(false);await refresh();}};
 return <main className="history-surface" aria-label={t('chat.historyAriaLabel')}>
  <div className="chat-scroll" ref={scroll} onScroll={()=>{if(active&&position.current?.scrolled())setUnread(false);}}>
   <div className="chat-content" ref={content}>
   {!messages.length&&!connection&&!activity&&!prompts.length&&<p className="empty-conversation">{t('chat.emptyConversation')}</p>}
   {snapshot?.hasEarlier&&<div className="history-pagination"><button className="text-action" disabled={earlierBusy||snapshot.connection!=='ready'} onClick={()=>void earlier()}>{earlierBusy?t('common.loading'):t('chat.loadEarlier')}</button></div>}
   <div className="history-messages">{messages.map(i=><Message key={i.requestId||i.id} item={i} report={setError} animate={i.id===activeReply}/>)}</div>
   {activity&&<WorkingMessage activity={activity} active={active}/>}
   {(!!prompts.length||!!reviews.length||connection||!!snapshot?.message||snapshot?.phase==='unknown')&&<article className="message-row assistant state-message">
    <BotAvatar/>
    <div className={`message assistant state-bubble ${prompts.length?'approval-bubble':''}`}>
   {prompts.map(p=><Prompt key={p.id} value={p} refresh={()=>void refresh()}/>)}
   {reviews.map(r=><ReviewNotice key={r.id} value={r}/>)}
   {connection&&<section className="connection-card" aria-label={t('chat.connectionCardAriaLabel')}>
    <strong>{snapshot.connection==='connecting'?t('chat.connectingTitle'):snapshot.connection==='login'?t('chat.loginTitle'):setup?t('chat.setupTitle'):t('chat.reconnectTitle')}</strong>
    <p>{snapshot.message||t('chat.checkingConnection')}</p>
    <div className="connection-actions">
     {snapshot.connection==='login'&&!snapshot.loginPending&&<button disabled={busy} onClick={()=>void action('Login')}>{t('chat.loginInBrowser')}</button>}
     {snapshot.loginPending&&<button disabled={busy} onClick={()=>void action('CancelLogin')}>{t('chat.cancelLogin')}</button>}
     {snapshot.connection!=='connecting'&&!snapshot.loginPending&&<button disabled={busy} onClick={()=>void action('Connect')}>{setup?t('chat.recheckSetup'):t('chat.reconnect')}</button>}
     {setup&&<button className="text-action" onClick={()=>void action('OpenConnectionHelp')}>{t('chat.setupHelp')}</button>}
     {snapshot.connection!=='connecting'&&<button className="text-action" onClick={()=>void desktop('OpenRuntimeSettings')}>{t('chat.connectionSettings')}</button>}
    </div>
   </section>}
   {!connection&&!!snapshot?.message&&<p className="connection-message" role="status">{snapshot.message}</p>}
   {!connection&&snapshot?.phase==='unknown'&&<div className="connection-actions">
    <button disabled={busy} onClick={()=>void action('Connect')}>{t('chat.reconnect')}</button>
   </div>}
    </div>
   </article>}
   {!!error&&<p className="inline-error" role="alert">{error}</p>}
   </div>
  </div>
  {unread&&<button className="new-messages" onClick={()=>{position.current!.latest();setUnread(false);}}>{t('chat.viewNewMessages')}</button>}
  <footer>
   {active&&<Composer snapshot={snapshot} focusRevision={activation} refresh={refresh} onOutgoing={stage}/>}
  </footer>
 </main>;
}
