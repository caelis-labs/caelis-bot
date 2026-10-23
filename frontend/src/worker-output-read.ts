import type {TaskConversation, WorkerTaskObservation} from './backend/contract';

export const workerTerminal=new Set(['completed','failed','cancelled','interrupted','stopped']);

// A missing collection from an older host must not crash polling or erase text
// already observed. An explicit empty array remains an authoritative empty view.
export function normalizeWorkerObservation(value:WorkerTaskObservation,previous?:WorkerTaskObservation):WorkerTaskObservation {
 if(!value?.task||typeof value.task.status!=='string')throw new Error('worker_observation_unavailable');
 return {...value,items:Array.isArray(value.items)?value.items:previous?.items??[]};
}

// One reader belongs to one mounted task view. Reads are bounded by transport;
// failures retain the caller's last successful projection and always free the
// in-flight guard so the next scheduled read can recover.
export function createWorkerOutputReader(io:{
 id:string;
 readHistory:(id:string)=>Promise<TaskConversation>;
 readObservation:(id:string)=>Promise<WorkerTaskObservation>;
 observation:(value:WorkerTaskObservation)=>void;
 history:(value:TaskConversation)=>void;
 observationFailed:(failed:boolean)=>void;
 historyFailed:(failed:boolean)=>void;
 now?:()=>number;
}) {
 let live=true,reading=false,historySettled=false,lastHistory=-Infinity,completedRevision='',status='';
 let previous:WorkerTaskObservation|undefined;
 const read=async()=>{
  if(!live||reading)return;
  reading=true;
  try {
   const now=(io.now??Date.now)(),historyDue=!historySettled&&now-lastHistory>=2500;
   if(historyDue)lastHistory=now;
   // Capture synchronous host refusals as read failures as well as rejections.
   const [observed,history]=await Promise.allSettled([
    Promise.resolve().then(()=>io.readObservation(io.id)),
    historyDue?Promise.resolve().then(()=>io.readHistory(io.id)):Promise.resolve(undefined),
   ]);
   if(!live)return;
   if(observed.status==='fulfilled'){
    try {
     const next=normalizeWorkerObservation(observed.value,previous);
     const completed=JSON.stringify(next.items.filter(item=>item.status==='completed').map(item=>[item.id,item.text,item.artifacts]));
     if(completedRevision&&completedRevision!==completed)historySettled=false;
     completedRevision=completed;
     if(status&&status!==next.task.status)historySettled=false;
     status=next.task.status;previous=next;
     io.observation(next);io.observationFailed(false);
    }catch{io.observationFailed(true)}
   }else io.observationFailed(true);
   if(historyDue){
    if(history.status==='fulfilled'&&history.value&&Array.isArray(history.value.messages)){
     io.history(history.value);io.historyFailed(false);
     historySettled=workerTerminal.has(history.value.status)&&(!status||workerTerminal.has(status));
    }else io.historyFailed(true);
   }
  }finally{reading=false}
 };
 return {read,close:()=>{live=false}};
}
