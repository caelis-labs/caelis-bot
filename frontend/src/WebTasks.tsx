import {useEffect,useLayoutEffect,useRef,useState} from 'react';
import {CheckCircleIcon} from '@phosphor-icons/react/dist/csr/CheckCircle';
import {XCircleIcon} from '@phosphor-icons/react/dist/csr/XCircle';
import {ClockIcon} from '@phosphor-icons/react/dist/csr/Clock';
import {QuestionIcon} from '@phosphor-icons/react/dist/csr/Question';
import {PauseCircleIcon} from '@phosphor-icons/react/dist/csr/PauseCircle';
import {XIcon} from '@phosphor-icons/react/dist/csr/X';
import {MessageContent} from './MessageContent';
import {SettingsSelect} from './SettingsSelect';
import {useI18n} from './i18n';
import type {WebClient} from './web-transport';

export type WebTask={id:string;title:string;status:string;machineName?:string;createdAt?:string};
type Conversation={status:string;messages:Array<{id:string;role:string;text:string}>};
const terminal=new Set(['completed','failed','cancelled','interrupted','stopped']);

function TaskTitle({task}:{task?:WebTask}) {
 const {t}=useI18n(),status=task?.status??'unknown';
 const running=['running','working'].includes(status),done=status==='completed',failed=status==='failed',stopped=['cancelled','interrupted','stopped'].includes(status),approval=['approval','waiting_approval','awaiting_approval'].includes(status),idle=['idle','queued','pending'].includes(status);
 const label=t(running?'native.taskRunning':done?'native.taskFinished':failed?'native.taskFailed':stopped?'native.taskInterrupted':approval?'native.taskApproval':idle?'native.taskIdle':'native.taskUnknown');
 const Icon=done?CheckCircleIcon:failed?XCircleIcon:stopped?PauseCircleIcon:approval||idle?ClockIcon:QuestionIcon;
 return <span className="web-task-title"><span className={`web-task-state${running?' running':''}${failed?' failed':''}`} role="img" aria-label={label} title={label}>{running?<span className="task-spinner"/>:<Icon size={16} aria-hidden="true"/>}</span><span className="web-task-title-text">{task?.title||'任务'}</span></span>;
}

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
 const {t}=useI18n();
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
 const messages=current?.messages.filter(message=>message.role==='user'||message.role==='assistant');
 return <aside className="web-task-pane" aria-label="后台任务">
  <header className="web-task-heading">
   {tasks.length>1?<SettingsSelect compact centered={false} aria-label="选择任务" value={id} onChange={e=>onSelect(e.target.value)}>
    {tasks.map(t=><option key={t.id} value={t.id} title={t.title}><TaskTitle task={t}/></option>)}
   </SettingsSelect>:<h2><TaskTitle task={task}/></h2>}
   <button className="web-task-close" aria-label={t('chat.closeTaskPanel')} title={t('chat.closeTaskPanel')} onClick={onClose}><XIcon size={18}/></button>
  </header>
  <div className="web-task-scroll" ref={scroll} onScroll={()=>{if(scroll.current)positions.current.set(id,scroll.current.scrollTop)}}>
   {messages?.map(message=><article key={message.id} className={`web-task-message ${message.role}`}>
    <MessageContent text={message.text} report={setError}/>
   </article>)}
   {id&&!current&&!error&&<p className="web-task-notice" role="status">正在读取任务…</p>}
   {current&&!messages?.length&&<p className="web-task-notice">{terminal.has(current.status)?'暂无可显示的任务记录。':'任务正在进行，结果会显示在这里。'}</p>}
   {!id&&!listError&&<p className="web-task-notice">暂无后台任务。</p>}
   {(error||listError)&&<div className="web-task-notice" role="status"><p>{error||'暂时无法读取任务列表，原工作仍保留。'}</p><button className="text-action" onClick={()=>{setAttempt(n=>n+1);onRetryList()}}>重新读取</button></div>}
  </div>
 </aside>;
}
