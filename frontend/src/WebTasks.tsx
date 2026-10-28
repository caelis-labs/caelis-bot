import {useEffect,useRef,useState} from 'react';
import {useWorkerOutput} from './WorkerOutput';
import {workerTerminal} from './worker-output-read';
import {primaryWebTask} from './web-task-choices';
import {WorkerChat} from './WorkerChat';
import {SettingsSelect} from './SettingsSelect';
import {useI18n} from './i18n';
import type {WebClient} from './web-transport';

export type WebTask={id:string;title:string;status:string;machineName?:string;createdAt?:string};

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

export function WebTasks({client,tasks,selected,onSelect,onClose,listError,onRetryList,active=true}:{client:WebClient;tasks:WebTask[];selected?:string;onSelect:(id:string)=>void;onClose:()=>void;listError:boolean;onRetryList:()=>void;active?:boolean}){
 const {t}=useI18n();
 const recentSelection=useRef('');
 const [showHistory,setShowHistory]=useState(false);
 useEffect(()=>{if(!active)setShowHistory(false)},[active]);
 useEffect(()=>{if(selected)recentSelection.current=selected;},[selected]);
 const id=selected||tasks.find(t=>t.id===recentSelection.current)?.id||primaryWebTask(tasks)?.id||'',task=tasks.find(t=>t.id===id);
 const output=useWorkerOutput(id,active,client.taskConversation,client.taskObservation,task?.status);
 const shownTask=output.observation?.task??task;
 const historyCount=tasks.filter(task=>workerTerminal.has(task.status)).length;
 const heading=active&&(tasks.length>1||historyCount>0)?<SettingsSelect compact centered={false} searchable searchLabel={t('chat.taskSearch')}
  menuAction={historyCount>0?{label:t(showHistory?'chat.taskHideHistory':'chat.taskShowHistory',{count:historyCount}),onClick:()=>setShowHistory(value=>!value)}:undefined} aria-label={t('chat.taskChoose')} value={id} onChange={e=>onSelect(e.target.value)}>
  {tasks.map(t=><option key={t.id} value={t.id} title={t.title} hidden={!showHistory&&workerTerminal.has(t.status)&&t.id!==id}>{t.title}</option>)}
 </SettingsSelect>:undefined;
 return <aside className="web-task-pane" aria-label={t('chat.taskPanel')}>
  {id?<WorkerChat active={active} id={id} task={shownTask} output={output} heading={heading} closeLabel={t('chat.taskBackToChat')} onClose={onClose}/>:<><div className="worker-chat-heading"><div className="worker-chat-title"><h2>{t('chat.taskPanel')}</h2></div><button className="worker-chat-close" onClick={onClose}>{t('chat.taskBackToChat')}</button></div><p className="worker-chat-empty">{t(listError?'chat.taskListUnavailable':'chat.taskListEmpty')}{listError&&<button className="text-action" onClick={onRetryList}>{t('chat.taskRetryRead')}</button>}</p></>}
 </aside>;
}
