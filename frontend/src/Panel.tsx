import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { backend, desktop, type DraftFile } from './desktop';
import type { Approval, ChatUpdate, Decision, Draft, Item, Receipt, Review, Snapshot, Submission } from './backend/contract';
import { handleComposerKey } from './composer-keyboard';
import { CopyText, MessageContent } from './MessageContent';
import { chatActivity, composerAction } from './chat-presentation';
import { WorkingMessage } from './WorkingMessage';

function Icon({ name }: { name: string }) { return <img className="symbol" src={`/icons/${name}.png`} alt="" />; }
export const labels: Record<string,string> = { working: '正在处理', sending: '正在发送', attention: '需要确认', interrupting: '正在停止', completed: '已完成', interrupted: '已停止', failed: '未完成', unknown: '结果待确认', unconfirmed: '结果未确认' };
export const reviewLabels: Record<string,string> = { inProgress:'正在自动审查', denied:'操作未通过自动审查', timedOut:'自动审查超时', aborted:'自动审查已中止' };

function ReviewNotice({value}:{value:Review}) {
 return <section className="review-notice" aria-label={reviewLabels[value.status]??'自动审查结果'}>
  <strong>{reviewLabels[value.status]??'自动审查结果待确认'}</strong>
  {value.rationale&&<p>{value.rationale}</p>}
  {value.action&&<details><summary>查看涉及的操作</summary><pre>{value.action}</pre></details>}
 </section>;
}

export function Prompt({ value, refresh }: { value: Approval; refresh: () => void }) {
  const [answers, setAnswers] = useState<Record<string,string[]>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const enabled = value.status === 'pending' && !busy;
  const decide = async (choice: string) => {
    setBusy(true); setError('');
    try { await backend('Decide', { id: value.id, choice, answers } satisfies Decision); }
    catch (e) { setError(e instanceof Error ? e.message : '暂时无法确认，请重试'); }
    finally { setBusy(false); refresh(); }
  };
  const advanced = value.choices.filter(c => c.scope === 'rule' || c.scope === 'conversation');
  const immediate = value.choices.filter(c => !advanced.includes(c));
  const button = (c: typeof value.choices[number]) => <button key={c.id} className={c.scope === 'once' || c.id === 'allow' || c.id === 'answer' || c.id === 'accept' ? 'primary-decision' : ''} disabled={!enabled} onClick={() => void decide(c.id)}>{c.label}</button>;
  return <section className="approval" aria-label={value.title}>
    <p className="event-eyebrow">需要你的决定</p>
    <h3>{value.title}</h3>
    {value.description && <p>{value.description}</p>}
    {value.action && <pre className="approval-action">{value.action}</pre>}
    {value.target && <p className="approval-target"><span>位置</span>{value.target}</p>}
    {value.details && (!value.action || value.details !== `${value.action}\n位置：${value.target}`) && <details open={!value.action}><summary>查看完整操作</summary><pre>{value.details}</pre></details>}
    {value.url && <button className="text-action" disabled={!enabled} onClick={() => void backend('OpenApprovalURL',value.id).catch(() => setError('链接已失效'))}>打开授权页面 ↗</button>}
    {(value.questions ?? []).map(q => <label className="question" key={q.id}>{q.title}{q.required && <span aria-label="必填"> *</span>}
      {q.type === 'select' || q.type === 'boolean' ? <select disabled={!enabled} value={answers[q.id]?.[0] ?? ''} onChange={e => setAnswers({ ...answers, [q.id]:[e.target.value] })}>
        <option value="">请选择</option>
        {q.type === 'boolean' ? <><option value="true">是</option><option value="false">否</option></> : q.options.map(o => <option key={o.id} value={o.id}>{o.label}</option>)}
      </select> : <><input type={q.secret ? 'password' : q.type === 'number' || q.type === 'integer' ? 'number' : 'text'} disabled={!enabled} value={answers[q.id]?.[0] ?? ''} onChange={e => setAnswers({ ...answers, [q.id]:[e.target.value] })} />
        {q.options.length > 0 && <div className="question-options">{q.options.map(o => <button key={o.id} disabled={!enabled} onClick={() => setAnswers({ ...answers, [q.id]:[o.id] })}>{o.label}</button>)}</div>}</>}
    </label>)}
    {error && <p className="inline-error" role="alert">{error}</p>}
    {value.status === 'pending' ? <>
      <div className="approval-choices">{immediate.map(button)}</div>
      {advanced.length > 0 && <details className="advanced-permission"><summary>更多授权方式</summary>{advanced.map(c => <div key={c.id} className="permission-option"><p>{c.scope === 'rule' ? '保存规则会影响后续相同操作，请核对范围。' : '授权将继续用于 Bot 的后续请求，直到后台对话结束。'}</p>{c.details && <pre>{c.details}</pre>}{button(c)}</div>)}</details>}
    </> : <p className="quiet" role="status">{value.status === 'resolved' ? '已处理' : value.status === 'sent' || value.status === 'sending' ? '已提交，等待确认' : '连接或结果待确认，请重新连接核对'}</p>}
  </section>;
}
function Message({ item, report }: { item: Item; report: (message: string) => void }) {
  return <article data-message-id={item.id} className={`message-row ${item.kind}`}>
   {item.kind==='assistant'&&<img className="bot-avatar" src="/icons/caelis-avatar.png" alt="Caelis Bot" width="32" height="32" draggable={false}/>}
   <div className={`message ${item.kind}`}>
    {item.kind === 'activity' ? <details><summary>{item.text}<span>{labels[item.status] ?? (item.status === 'inProgress' ? '进行中' : item.status === 'declined' ? '已拒绝' : '')}</span></summary>{item.details && <pre>{item.details}</pre>}</details> : item.kind === 'assistant' ? <MessageContent text={item.text} report={report}/> : <p>{item.text}</p>}
    {item.artifacts?.map(file => <button className="artifact" key={file.id} onClick={() => void backend('RevealArtifact',file.id).catch(() => report('文件已不可用'))}><Icon name="paperclip" />{file.name}<span>在访达中显示</span></button>)}
    {!!item.text&&item.kind!=='activity'&&<div className="message-actions"><CopyText text={item.text} report={report}/></div>}
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
function Composer({snapshot,quick=false,active=true,activation=0,refresh}:{snapshot:Snapshot|null;quick?:boolean;active?:boolean;activation?:number;refresh:()=>Promise<void>}) {
 const input=useRef<HTMLTextAreaElement>(null),send=useRef<HTMLButtonElement>(null);
 const [draft,setDraft]=useState(''),[refs,setRefs]=useState<string[]>([]),[files,setFiles]=useState<DraftFile[]>([]);
 const [busy,setBusy]=useState(false),[expanded,setExpanded]=useState(false),[error,setError]=useState(''),[loaded,setLoaded]=useState(false);
 const visible=useRef(active);visible.current=active;
 const pending=useRef<Submission|null>(null);
 const saved=useRef<Draft>({revision:0,text:'',referenceIds:[],notice:''});
 const writes=useRef<Promise<void>>(Promise.resolve()), conflicted=useRef(false);
 const readFiles=()=>desktop<DraftFile[]>('DraftFiles').then(setFiles);
 useEffect(()=>{
  let mounted=true;
  setLoaded(false);conflicted.current=false;
  void writes.current.then(()=>backend<Draft>('Draft')).then(d=>{if(mounted){saved.current=d;setDraft(d.text);setRefs(d.referenceIds??[]);setError(d.notice);setLoaded(true);if(visible.current)input.current?.focus();}}).catch(()=>setError('暂时无法读取草稿'));
  void readFiles();
  const changed=(event:Event)=>{void readFiles();setError((event as CustomEvent<string>).detail??'');};
  window.addEventListener('files-changed',changed);
  return()=>{mounted=false;window.removeEventListener('files-changed',changed);};
 },[activation]);
 useEffect(()=>{if(active&&loaded&&!busy){input.current?.focus();if(quick){const frame=requestAnimationFrame(()=>void desktop('PanelReady',activation));return()=>cancelAnimationFrame(frame);}}},[active,loaded,busy,activation]);
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
   catch(e){conflicted.current=true;setError(e instanceof Error?e.message:'草稿暂未保存；请保留当前内容并重新打开');}
  });
 };
 useLayoutEffect(()=>{const editor=input.current;if(editor){editor.style.height='0px';editor.style.height=`${Math.max(27,Math.min(127,editor.scrollHeight))}px`;}},[draft]);
 const pick=async()=>{setBusy(true);setError('');try{setFiles(await desktop<DraftFile[]>('PickFiles'));setExpanded(false);}catch(e){setError(e instanceof Error?e.message:'无法选择文件');}finally{setBusy(false);input.current?.focus();}};
 const submit=async()=>{
  if(busy||!loaded||!(snapshot?.canSend||(!quick&&snapshot?.canSteer))||(!draft.trim()&&!files.length))return;
  setBusy(true);setError('');setExpanded(false);await writes.current;
  if(conflicted.current){setBusy(false);return;}
  const request:Submission={id:crypto.randomUUID(),text:draft,fileIds:files.map(f=>f.id),referenceIds:refs};
 pending.current=request;
  try{
   const receipt=await backend<Receipt>('Submit',request);
   if(receipt.outcome==='accepted'){
    pending.current=null;
    saved.current=await backend<Draft>('Draft');setDraft(saved.current.text);setRefs(saved.current.referenceIds??[]);setError(saved.current.notice);await readFiles();
    if(quick)await desktop('ClosePanel');
   }else setError(receipt.message||'发送结果待确认，草稿已保留');
  }catch{setError('发送结果待确认，草稿已保留；请在聊天窗口核对');}
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
 const enabled=loaded&&!busy&&(primaryAction==='stop'?!stopping:(snapshot?.canSend||(!quick&&snapshot?.canSteer))&&!!(draft.trim()||files.length));
 const interrupt=async()=>{
  if(busy||!loaded||!snapshot?.canInterrupt||stopping)return;
  setBusy(true);setError('');
  try{await backend('Interrupt');}
  catch(e){setError(e instanceof Error?e.message:'暂时无法停止，请重试');}
  finally{
   try{await refresh();}catch{setError(previous=>previous||'停止状态暂未更新，请稍后核对');}
   finally{setBusy(false);}
  }
 };
 const actionLabel=primaryAction==='stop'?(stopping?'正在停止':'停止工作'):!quick&&snapshot?.canSteer?'补充说明':'发送';
 return <div className="compose-area" data-file-drop-target>
  <div className="capsule">
   <button className="icon-button add" disabled={busy||!loaded} onClick={()=>setExpanded(!expanded)} aria-label="添加附件或引用" aria-expanded={expanded}><Icon name="plus"/></button>
   <textarea aria-label="写下想法" ref={input} rows={1} value={draft} disabled={!loaded||busy} onChange={e=>save(e.target.value,refs)} onKeyDown={e=>handleComposerKey(e.nativeEvent,send.current,primaryAction)} placeholder={!quick&&snapshot?.canSteer?'补充说明…':'写下想法…'} title="Enter 发送，Shift+Enter 换行"/>
   <button ref={send} className="icon-button send" disabled={!enabled} onClick={()=>void (primaryAction==='stop'?interrupt():submit())} aria-label={actionLabel} title={actionLabel}>{primaryAction==='stop'?<span className="composer-stop" aria-hidden="true"/>:<Icon name="arrow.up"/>}</button>
  </div>
  {!!error&&<p role="alert" className="input-error">{error}</p>}
  {!!(files.length||refs.length)&&<ul className="attachments" aria-label="待发送的附件与引用">
   {files.map(f=><li key={f.id} className={f.unavailable?'attachment-unavailable':''}><Icon name="paperclip"/><span title={f.name}>{f.name}{f.unavailable?'（已不可用，请移除重选）':''}</span><button disabled={busy} aria-label={`移除 ${f.name}`} onClick={()=>void desktop<DraftFile[]>('RemoveFile',f.id).then(setFiles).catch(()=>setError('附件选择暂未保存，请重试'))}><Icon name="xmark"/></button></li>)}
   {refs.map(id=><li key={id}><span>{snapshot?.references.find(r=>r.id===id)?.name??'引用'}</span><button disabled={busy} aria-label="移除引用" onClick={()=>save(draft,refs.filter(v=>v!==id))}><Icon name="xmark"/></button></li>)}
  </ul>}
  {expanded&&<section className="add-options" aria-label="附件与引用">
   <button className="action-row" disabled={busy} onClick={()=>void pick()}><Icon name="paperclip"/>添加文件</button>
   {!!snapshot?.references.length&&<><p className="menu-caption">插件与技能</p><div className="reference-options">{snapshot.references.map(r=><button key={r.id} className="action-row" disabled={refs.includes(r.id)} title={r.description} onClick={()=>{save(draft,[...refs,r.id]);setExpanded(false);input.current?.focus();}}><span>{r.name}<small>{r.description}</small></span></button>)}</div></>}
  </section>}
 </div>;
}

export function Panel() {
 const [active,setActive]=useState(false),[activation,setActivation]=useState(0);const surface=useRef<HTMLElement>(null);
 const {snapshot,refresh}=useConversation(active,false,false,true);
 useEffect(()=>{
  const open=(event:Event)=>{setActivation((event as CustomEvent<{activation:number}>).detail?.activation??0);setActive(true);},close=()=>setActive(false);
  const key=(e:KeyboardEvent)=>{if(!e.isComposing&&(e.key==='Escape'||(e.metaKey&&e.key==='w'))){e.preventDefault();void desktop('ClosePanel');}};
  window.addEventListener('panel-open',open);window.addEventListener('panel-close',close);window.addEventListener('keydown',key);
  return()=>{window.removeEventListener('panel-open',open);window.removeEventListener('panel-close',close);window.removeEventListener('keydown',key);};
 },[]);
 useEffect(()=>{const resize=new ResizeObserver(()=>{if(surface.current)void desktop('SetPanelHeight',Math.max(64,Math.min(500,Math.ceil(surface.current.getBoundingClientRect().height))));});resize.observe(surface.current!);return()=>resize.disconnect();},[]);
 return <main ref={surface} className="input-surface" aria-label="发送给 Caelis Bot"><Composer snapshot={snapshot} quick active={active} activation={activation} refresh={refresh}/>{active&&snapshot&&!snapshot.canSend&&<button className="text-action" onClick={()=>void desktop('OpenHistory')}>{snapshot.canSteer?'正在处理，打开对话补充说明':'打开对话查看连接或待确认事项'}</button>}</main>;
}

export function History() {
 const [active,setActive]=useState(false),[error,setError]=useState(''),[busy,setBusy]=useState(false),[unread,setUnread]=useState(false);
 const {snapshot,refresh}=useConversation(active,false,true);
 const [earlierBusy,setEarlierBusy]=useState(false);
 const prepend=useRef<{id:string;top:number}|null>(null);
 const scroll=useRef<HTMLDivElement>(null),follow=useRef(true);
 useEffect(()=>{
  const opened=()=>{setActive(true);},closed=()=>setActive(false);
  void desktop<boolean>('HistoryVisible').then(v=>{if(v)opened();});
  const key=(e:KeyboardEvent)=>{if(!e.isComposing&&(e.key==='Escape'||(e.metaKey&&e.key==='w'))){e.preventDefault();void desktop('CloseHistory');}};
  window.addEventListener('history-open',opened);window.addEventListener('history-close',closed);window.addEventListener('keydown',key);
  return()=>{window.removeEventListener('history-open',opened);window.removeEventListener('history-close',closed);window.removeEventListener('keydown',key);};
 },[]);
 const messages=snapshot?.items.filter(i=>i.kind==='user'||(i.kind==='assistant'&&(i.text.trim()||i.artifacts?.length)))??[];
 const contentKey=messages.map(i=>i.id+i.text).join('');
 const prompts=snapshot?.approvals.filter(p=>p.status!=='resolved')??[];
 const promptKey=prompts.map(p=>p.id+p.status).join('');
 const reviews=snapshot?.reviews?.filter(r=>r.status==='denied'||r.status==='timedOut'||r.status==='aborted')??[];
 const activity=chatActivity(snapshot);
 const connection=snapshot&&snapshot.connection!=='ready';
 const setup=snapshot?.connectionIssue==='runtime_missing'||snapshot?.connectionIssue==='runtime_protocol';
 useLayoutEffect(()=>{
  const el=scroll.current;if(!el)return;
  if(prepend.current){
   if(earlierBusy)return;
   const anchor=el.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(prepend.current.id)}"]`);
   if(anchor)el.scrollTop+=anchor.getBoundingClientRect().top-prepend.current.top;
   prepend.current=null;
  }else if(follow.current){el.scrollTop=el.scrollHeight;setUnread(false);}else setUnread(true);
 },[contentKey,promptKey,activity,snapshot?.hasEarlier,earlierBusy]);
 const earlier=async()=>{if(earlierBusy)return;setEarlierBusy(true);setError('');const el=scroll.current!;follow.current=false;const anchor=Array.from(el.querySelectorAll<HTMLElement>('[data-message-id]')).find(item=>item.getBoundingClientRect().bottom>el.getBoundingClientRect().top);if(anchor)prepend.current={id:anchor.dataset.messageId!,top:anchor.getBoundingClientRect().top};try{await backend('LoadEarlier');await refresh();}catch{prepend.current=null;setError('更早消息暂时无法读取，请重试');}finally{setEarlierBusy(false);}};
 const action=async(method:string)=>{setBusy(true);setError('');try{await backend(method);}catch(e){setError(e instanceof Error?e.message:'暂时无法操作');}finally{setBusy(false);await refresh();}};
 return <main className="history-surface" aria-label="与 Caelis Bot 聊天">
  <div className="chat-scroll" ref={scroll} onScroll={()=>{const el=scroll.current!;follow.current=el.scrollHeight-el.scrollTop-el.clientHeight<56;if(follow.current)setUnread(false);}}>
   {!messages.length&&!connection&&!activity&&!prompts.length&&<p className="empty-conversation">有什么想和我说的？</p>}
   {snapshot?.hasEarlier&&<div className="history-pagination"><button className="text-action" disabled={earlierBusy||snapshot.connection!=='ready'} onClick={()=>void earlier()}>{earlierBusy?'正在读取…':'查看更早消息'}</button></div>}
   <div className="history-messages">{messages.map(i=><Message key={i.id} item={i} report={setError}/>)}</div>
   {activity&&<WorkingMessage activity={activity} active={active}/>}
   {prompts.map(p=><Prompt key={p.id} value={p} refresh={()=>void refresh()}/>)}
   {reviews.map(r=><ReviewNotice key={r.id} value={r}/>)}
   {connection&&<section className="connection-card" aria-label="连接 Codex">
    <strong>{snapshot.connection==='connecting'?'正在连接本机 Codex…':snapshot.connection==='login'?'连接你的 Codex 账户':setup?'准备本机 Codex':'恢复连接'}</strong>
    <p>{snapshot.message||'正在检查本机安装和登录状态。'}</p>
    <div className="connection-actions">
     {snapshot.connection==='login'&&!snapshot.loginPending&&<button disabled={busy} onClick={()=>void action('Login')}>在浏览器中登录</button>}
     {snapshot.loginPending&&<button disabled={busy} onClick={()=>void action('CancelLogin')}>取消登录</button>}
     {snapshot.connection!=='connecting'&&!snapshot.loginPending&&<button disabled={busy} onClick={()=>void action('Connect')}>{setup?'重新检测':'重新连接'}</button>}
     {setup&&<button className="text-action" onClick={()=>void action('OpenConnectionHelp')}>安装说明 ↗</button>}
     {snapshot.connection!=='connecting'&&<button className="text-action" onClick={()=>void desktop('OpenRuntimeSettings')}>连接设置…</button>}
    </div>
   </section>}
   {!connection&&!!snapshot?.message&&<p className="connection-message" role="status">{snapshot.message}</p>}
   {!!error&&<p className="inline-error" role="alert">{error}</p>}
   {!connection&&snapshot?.phase==='unknown'&&<div className="connection-actions">
    <button disabled={busy} onClick={()=>void action('Connect')}>重新连接</button>
   </div>}
  </div>
  {unread&&<button className="new-messages" onClick={()=>{follow.current=true;scroll.current!.scrollTop=scroll.current!.scrollHeight;setUnread(false);}}>查看新消息 ↓</button>}
  <footer>
   {active&&<Composer snapshot={snapshot} refresh={refresh}/>}
  </footer>
 </main>;
}
