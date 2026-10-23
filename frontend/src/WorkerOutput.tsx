import {useEffect, useState} from 'react';
import type {Item, TaskConversation, WorkerTaskObservation} from './backend/contract';
import {ArtifactLink} from './ArtifactLink';
import {MessageContent} from './MessageContent';
import {useI18n} from './i18n';
import {createWorkerOutputReader,workerTerminal as terminal} from './worker-output-read';

const retain=<T,>(old:Record<string,T>,id:string,value:T)=>Object.fromEntries([...Object.entries(old).filter(([key])=>key!==id).slice(-4),[id,value]]);

// Both surfaces read the original Manager route. A failed history read does not
// turn completed work into an empty successful transcript or erase cached text.
export function useWorkerOutput(id:string,active:boolean,readHistory:(id:string)=>Promise<TaskConversation>,readObservation:(id:string)=>Promise<WorkerTaskObservation>,statusHint?:string){
 const [histories,setHistories]=useState<Record<string,TaskConversation>>({}),[observations,setObservations]=useState<Record<string,WorkerTaskObservation>>({});
 const [historyFailed,setHistoryFailed]=useState(false),[observationFailed,setObservationFailed]=useState(false),[attempt,setAttempt]=useState(0);
 useEffect(()=>{
  if(!active||!id)return;
  setHistoryFailed(false);setObservationFailed(false);
  const reader=createWorkerOutputReader({id,readHistory,readObservation,
   observation:next=>setObservations(old=>retain(old,id,next)),history:next=>setHistories(old=>retain(old,id,next)),
   observationFailed:setObservationFailed,historyFailed:setHistoryFailed});
  void reader.read();const timer=window.setInterval(()=>void reader.read(),1000);
  return()=>{reader.close();window.clearInterval(timer);};
 },[id,active,readHistory,readObservation,statusHint,attempt]);
 return {conversation:histories[id],observation:observations[id],historyFailed,observationFailed,retry:()=>setAttempt(value=>value+1)};
}

export function WorkerOutput({conversation,items=[],historyFailed,observationFailed,report,onRetry}:{conversation?:TaskConversation;items?:Item[];historyFailed:boolean;observationFailed:boolean;report:(message:string)=>void;onRetry:()=>void}){
 const {t}=useI18n();
 const messages=(Array.isArray(conversation?.messages)?conversation.messages:[]).filter(message=>message.role==='user'||message.role==='assistant');
 const shownFiles=new Set(messages.flatMap(message=>(message.artifacts??[]).map(file=>file.id)));
 const historicalIDs=new Set(messages.map(message=>message.id));
 const live=(Array.isArray(items)?items:[]).flatMap(item=>{
  const artifacts=(item.artifacts??[]).filter(file=>!shownFiles.has(file.id));
  artifacts.forEach(file=>shownFiles.add(file.id));
  const text=!(item.historyId&&historicalIDs.has(item.historyId))&&['assistant','activity','tool'].includes(item.kind)?item.text:'';
  return text.trim()||artifacts.length?[{...item,text,artifacts}]:[];
 });
 return <>
  {messages.map(message=><article key={message.id} className={`web-task-message ${message.role}`} style={{marginBlock:24,lineHeight:1.75,fontSize:14}}>
   <MessageContent text={message.text} report={report}/>
   {!!message.artifacts?.length&&<div className="artifacts">{message.artifacts.map(file=><ArtifactLink file={file} report={report} key={file.id}/>)}</div>}
  </article>)}
  {live.map(item=><article key={item.id} className="web-task-message assistant" style={{marginBlock:24,lineHeight:1.75,fontSize:14}} aria-label={t('chat.taskLiveOutput')}>
   {item.text&&<MessageContent text={item.text} report={report}/>}
   {!!item.artifacts.length&&<div className="artifacts">{item.artifacts.map(file=><ArtifactLink file={file} report={report} key={file.id}/>)}</div>}
  </article>)}
  {!conversation&&!historyFailed&&<p className="settings-note" role="status">{t('chat.taskLoading')}</p>}
  {conversation&&!messages.length&&!live.length&&!historyFailed&&<p className="settings-note">{t(terminal.has(conversation.status)?'chat.taskHistoryEmpty':'chat.taskInProgress')}</p>}
  {(historyFailed||observationFailed)&&<div className="settings-note" role="status">
   {historyFailed&&<p>{t('chat.taskHistoryUnavailable')}</p>}
   {observationFailed&&<p>{t('chat.taskObservationUnavailable')}</p>}
   <button className="text-action" onClick={onRetry}>{t('chat.taskRetryRead')}</button>
  </div>}
 </>;
}

export function WorkerTaskStatus({status}:{status?:string}){
 const {t}=useI18n();
 const key=['running','working'].includes(status??'')?'native.taskRunning':status==='completed'?'native.taskFinished':status==='failed'?'native.taskFailed':['cancelled','interrupted','stopped'].includes(status??'')?'native.taskInterrupted':['approval','waiting_approval','awaiting_approval'].includes(status??'')?'native.taskApproval':['idle','queued','pending'].includes(status??'')?'native.taskIdle':'native.taskUnknown';
 return <>{t(key)}</>;
}
