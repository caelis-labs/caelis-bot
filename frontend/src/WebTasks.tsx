import {useEffect,useLayoutEffect,useRef,useState} from 'react';
import {CheckCircleIcon} from '@phosphor-icons/react/dist/csr/CheckCircle';
import {XCircleIcon} from '@phosphor-icons/react/dist/csr/XCircle';
import {ClockIcon} from '@phosphor-icons/react/dist/csr/Clock';
import {QuestionIcon} from '@phosphor-icons/react/dist/csr/Question';
import {PauseCircleIcon} from '@phosphor-icons/react/dist/csr/PauseCircle';
import {XIcon} from '@phosphor-icons/react/dist/csr/X';
import {useWorkerOutput,WorkerOutput} from './WorkerOutput';
import {SettingsSelect} from './SettingsSelect';
import {useI18n} from './i18n';
import type {WebClient} from './web-transport';

export type WebTask={id:string;title:string;status:string;machineName?:string;createdAt?:string};

function TaskTitle({task}:{task?:WebTask}) {
 const {t}=useI18n(),status=task?.status??'unknown';
 const running=['running','working'].includes(status),done=status==='completed',failed=status==='failed',stopped=['cancelled','interrupted','stopped'].includes(status),approval=['approval','waiting_approval','awaiting_approval'].includes(status),idle=['idle','queued','pending'].includes(status);
 const label=t(running?'native.taskRunning':done?'native.taskFinished':failed?'native.taskFailed':stopped?'native.taskInterrupted':approval?'native.taskApproval':idle?'native.taskIdle':'native.taskUnknown');
 const Icon=done?CheckCircleIcon:failed?XCircleIcon:stopped?PauseCircleIcon:approval||idle?ClockIcon:QuestionIcon;
 return <span className="web-task-title"><span className={`web-task-state${running?' running':''}${failed?' failed':''}`} role="img" aria-label={label} title={label}>{running?<span className="task-spinner"/>:<Icon size={16} aria-hidden="true"/>}</span><span className="web-task-title-text">{task?.title||t('chat.taskTitle')}</span></span>;
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
 const [error,setError]=useState('');
 const output=useWorkerOutput(id,true,client.taskConversation,client.taskObservation,task?.status);
 const scroll=useRef<HTMLDivElement>(null),positions=useRef(new Map<string,number>());
 useLayoutEffect(()=>{if(scroll.current)scroll.current.scrollTop=positions.current.get(id)??0;setError('');},[id,!!output.conversation]);
 const shownTask=output.observation?.task??task;

 return <aside className="web-task-pane" aria-label={t('chat.taskPanel')}>
  <header className="web-task-heading">
   {tasks.length>1?<SettingsSelect compact centered={false} aria-label={t('chat.taskChoose')} value={id} onChange={e=>onSelect(e.target.value)}>
    {tasks.map(t=><option key={t.id} value={t.id} title={t.title}><TaskTitle task={t}/></option>)}
   </SettingsSelect>:<h2><TaskTitle task={shownTask}/></h2>}
   <button className="web-task-close" aria-label={t('chat.closeTaskPanel')} title={t('chat.closeTaskPanel')} onClick={onClose}><XIcon size={18}/></button>
  </header>
  <div className="web-task-scroll" ref={scroll} onScroll={()=>{if(scroll.current)positions.current.set(id,scroll.current.scrollTop)}}>
   {id&&<>
    {shownTask&&<p className="web-task-notice">{t('chat.taskMachine',{name:shownTask.machineName||t('chat.taskLocalMachine')})}</p>}
    <WorkerOutput {...output} items={output.observation?.items} report={setError} onRetry={()=>{output.retry();onRetryList();}}/>
   </>}
   {!id&&!listError&&<p className="web-task-notice">{t('chat.taskListEmpty')}</p>}
   {(error||listError)&&<div className="web-task-notice" role="status"><p>{error||t('chat.taskListUnavailable')}</p><button className="text-action" onClick={()=>{setError('');output.retry();onRetryList();}}>{t('chat.taskRetryRead')}</button></div>}

  </div>
 </aside>;
}
