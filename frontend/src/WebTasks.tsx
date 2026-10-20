import {useEffect,useLayoutEffect,useRef,useState} from 'react';
import {XIcon} from '@phosphor-icons/react/dist/csr/X';
import {BotAvatar} from './BotAvatar';
import {MessageContent} from './MessageContent';
import {SettingsSelect} from './SettingsSelect';
import {useI18n} from './i18n';
import type {WebClient} from './web-transport';

export type WebTask={id:string;title:string;status:string;machineName?:string;createdAt?:string};
type Conversation={status:string;messages:Array<{id:string;role:string;text:string}>};
const terminal=new Set(['completed','failed','cancelled','interrupted','stopped']);
const labels:Record<string,string>={running:'进行中',working:'进行中',completed:'已完成',failed:'未完成',unknown:'等待核对',pending:'等待中',stopped:'已停止',cancelled:'已取消',interrupted:'已停止'};
const statusLabel=(status:string)=>labels[status]??'已保留';

// Observe the original task owner only. No terminal, start, send or report-ack
// endpoint participates in this surface.
export function useWebTasks(client:WebClient,active:boolean){
 const [tasks,setTasks]=useState<WebTask[]>([]),[error,setError]=useState(false),[attempt,setAttempt]=useState(0);
 useEffect(()=>{
  if(!active)return;
  let live=true,reading=false;
  const read=async()=>{
   if(reading)return;reading=true;
   try{
    const value:WebTask[]=await client.taskList();
    if(live){setTasks([...value].sort((a,b)=>(b.createdAt??'').localeCompare(a.createdAt??'')||a.id.localeCompare(b.id)));setError(false);}
   }catch{if(live)setError(true)}finally{reading=false}
  };
  void read();const timer=setInterval(()=>void read(),2500);
  return()=>{live=false;clearInterval(timer)};
 },[client,active,attempt]);
 return {tasks,error,retry:()=>setAttempt(n=>n+1)};
}

export function WebTasks({client,tasks,selected,onSelect,onClose,listError,onRetryList}:{client:WebClient;tasks:WebTask[];selected?:string;onSelect:(id:string)=>void;onClose:()=>void;listError:boolean;onRetryList:()=>void}){
 const {locale}=useI18n();
 const recentSelection=useRef('');
 useEffect(()=>{if(selected)recentSelection.current=selected;},[selected]);
 const id=selected||tasks.find(t=>t.id===recentSelection.current)?.id||tasks[0]?.id||'',task=tasks.find(t=>t.id===id);
 const [conversations,setConversations]=useState<Record<string,Conversation>>({}),[error,setError]=useState(''),[attempt,setAttempt]=useState(0);
 const scroll=useRef<HTMLDivElement>(null),positions=useRef(new Map<string,number>());
 const current=conversations[id];
 useEffect(()=>{
  if(!id)return;
  setError('');
  let live=true,reading=false,done=false;
  const read=async()=>{
   if(reading||done)return;reading=true;
   try{
    const value:Conversation=await client.taskConversation(id);
    if(live){
     setConversations(old=>({...Object.fromEntries(Object.entries(old).slice(-4)),[id]:value}));setError('');
     done=terminal.has(value.status);
    }
   }catch{if(live)setError('暂时无法读取这项工作的记录。')}finally{reading=false}
  };
  void read();const timer=setInterval(()=>void read(),2500);
  return()=>{live=false;clearInterval(timer)};
 },[client,id,task?.status,attempt]);
 useLayoutEffect(()=>{if(scroll.current)scroll.current.scrollTop=positions.current.get(id)??0;},[id,!!current]);
 const time=task?.createdAt&&Number.isFinite(Date.parse(task.createdAt))?new Intl.DateTimeFormat(locale,{month:'numeric',day:'numeric',hour:'2-digit',minute:'2-digit',hour12:false}).format(new Date(task.createdAt)):'';
 return <aside className="web-task-pane" aria-label="后台任务">
  <header className="web-task-heading"><h2>后台任务</h2><button className="web-task-close" aria-label="返回聊天" title="返回聊天" onClick={onClose}><XIcon size={18}/></button></header>
  {!!tasks.length&&<div className="web-task-selection">
   {tasks.length>1?<SettingsSelect aria-label="选择任务" value={id} onChange={e=>onSelect(e.target.value)}>
    {tasks.map(t=><option key={t.id} value={t.id} data-description={statusLabel(t.status)}>{t.title||'后台任务'}</option>)}
   </SettingsSelect>:<h3>{task?.title||'后台任务'}</h3>}
   <div className="web-task-meta"><span className={`web-task-status ${task?.status||'unknown'}`}><i aria-hidden="true"/>{statusLabel(task?.status||'unknown')}</span>{time&&<time dateTime={task!.createdAt} title="创建时间">{time}</time>}{task?.machineName&&<span>{task.machineName}</span>}</div>
  </div>}
  <div className="web-task-scroll" ref={scroll} onScroll={()=>{if(scroll.current)positions.current.set(id,scroll.current.scrollTop)}}>
   {current?.messages.map(message=><article key={message.id} className={`web-task-message ${message.role}`}>
    {message.role==='user'?<details className="web-task-request"><summary>任务要求</summary><MessageContent text={message.text} report={setError}/></details>:<><div className="web-task-author"><BotAvatar/><span>工作进展</span></div><MessageContent text={message.text} report={setError}/></>}
   </article>)}
   {id&&!current&&!error&&<p className="web-task-notice" role="status">正在读取任务…</p>}
   {current&&!current.messages.length&&<p className="web-task-notice">{terminal.has(current.status)?'暂无可显示的任务记录。':'任务正在进行，结果会显示在这里。'}</p>}
   {!id&&!listError&&<p className="web-task-notice">暂无后台任务。</p>}
   {(error||listError)&&<div className="web-task-notice" role="status"><p>{error||'暂时无法读取任务列表，原工作仍保留。'}</p><button className="text-action" onClick={()=>{setAttempt(n=>n+1);onRetryList()}}>重新读取</button></div>}
  </div>
 </aside>;
}
