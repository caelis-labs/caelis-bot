import {useEffect, useState} from 'react';
import type {TaskConversation, WorkerTaskObservation} from './backend/contract';
import {useI18n} from './i18n';
import {createWorkerOutputReader} from './worker-output-read';

const retain=<T,>(old:Record<string,T>,id:string,value:T)=>Object.fromEntries([...Object.entries(old).filter(([key])=>key!==id).slice(-4),[id,value]]);

// Both surfaces read the original Manager route. A failed history read does not
// turn completed work into an empty successful transcript or erase cached text.
export function useWorkerOutput(id:string,active:boolean,readHistory:(id:string)=>Promise<TaskConversation>,readObservation:(id:string)=>Promise<WorkerTaskObservation>,statusHint?:string){
 const [histories,setHistories]=useState<Record<string,TaskConversation>>({}),[observations,setObservations]=useState<Record<string,WorkerTaskObservation>>({});
 const [historyFailures,setHistoryFailures]=useState<Record<string,boolean>>({}),[observationFailures,setObservationFailures]=useState<Record<string,boolean>>({}),[attempt,setAttempt]=useState(0);
 useEffect(()=>{
  if(!active||!id)return;
  setHistoryFailures(old=>retain(old,id,false));setObservationFailures(old=>retain(old,id,false));
  const reader=createWorkerOutputReader({id,readHistory,readObservation,initialHistory:histories[id],initialObservation:observations[id],
   observation:next=>setObservations(old=>retain(old,id,next)),history:next=>setHistories(old=>retain(old,id,next)),
   observationFailed:failed=>setObservationFailures(old=>retain(old,id,failed)),historyFailed:failed=>setHistoryFailures(old=>retain(old,id,failed))});
  void reader.read();const timer=window.setInterval(()=>void reader.read(),1000);
  return()=>{reader.close();window.clearInterval(timer);};
 },[id,active,readHistory,readObservation,statusHint,attempt]);
 return {conversation:histories[id],observation:observations[id],historyFailed:historyFailures[id]??false,observationFailed:observationFailures[id]??false,retry:()=>setAttempt(value=>value+1)};
}

export function WorkerTaskStatus({status}:{status?:string}){
 const {t}=useI18n();
 const key=['running','working'].includes(status??'')?'native.taskRunning':status==='completed'?'native.taskFinished':status==='failed'?'native.taskFailed':['cancelled','interrupted','stopped'].includes(status??'')?'native.taskInterrupted':['approval','waiting_approval','awaiting_approval'].includes(status??'')?'native.taskApproval':['idle','queued','pending'].includes(status??'')?'native.taskIdle':'native.taskUnknown';
 return <>{t(key)}</>;
}
