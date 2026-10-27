import type {Item,TaskConversation, WorkerTaskObservation} from './backend/contract';
import {historyCoversObservation} from './worker-messages.ts';

export const workerTerminal=new Set(['completed','failed','cancelled','interrupted','stopped']);

// A missing collection from an older host must not crash polling or erase text
// already observed. An explicit empty array remains an authoritative empty view.
export function normalizeWorkerObservation(value:WorkerTaskObservation,previous?:WorkerTaskObservation):WorkerTaskObservation {
 if(!value?.task||typeof value.task.status!=='string')throw new Error('worker_observation_unavailable');
 return {...value,items:Array.isArray(value.items)?value.items:previous?.items??[]};
}

// This is a bounded presentation cache, not an event log or execution fact.
// History replaces an observed message only through its exact native key.
export function retainWorkerOutput(previous:Item[],current:Item[],history?:TaskConversation):Item[] {
 const read=new Map((history?.messages??[]).map(message=>[message.id,message]));
 const merged=new Map<string,Item>();
 for(const item of [...previous,...current]){
  const final=item.historyId?read.get(item.historyId):undefined;
  const filesCovered=!(item.artifacts??[]).some(file=>!final?.artifacts?.some(stored=>stored.id===file.id));
  if(final&&filesCovered&&historyCoversObservation(final.text,item.text)){merged.delete(item.id);continue;}
  if(final&&filesCovered&&item.status!=='inProgress'&&!item.text.startsWith(final.text)){merged.delete(item.id);continue;}
  merged.set(item.id,item);
 }
 let bytes=0;
 const bounded:Item[]=[];
 for(const item of [...merged.values()].reverse()){
  const size=new TextEncoder().encode(JSON.stringify(item)).length;
  if(size>512*1024)continue;
  if(bounded.length>=200||bytes+size>512*1024)break;
  bytes+=size;bounded.push(item);
 }
 return bounded.reverse();
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
 environmentUnavailable?:(unavailable:boolean)=>void;
 observationFailed:(failed:boolean)=>void;
 historyFailed:(failed:boolean)=>void;
 initialObservation?:WorkerTaskObservation;
 initialHistory?:TaskConversation;
 now?:()=>number;
}) {
 let live=true,observing=false,readingHistory=false,historySettled=false,lastHistory=-Infinity,completedRevision='',status='';
 let previous=io.initialObservation,lastTranscript=io.initialHistory,unavailable=false;
 const unavailableError=(error:unknown)=>{if(error instanceof Error&&error.message==='original_machine_unavailable'){unavailable=true;io.environmentUnavailable?.(true);}};
 const observe=async()=>{
  if(!live||observing||unavailable)return;
  observing=true;
  try{
   const value=await io.readObservation(io.id);
   if(!live)return;
   const next=normalizeWorkerObservation(value,previous);
   if(next.task.id!==io.id)throw new Error('worker_observation_identity_changed');
   next.items=retainWorkerOutput(previous?.items??[],next.items,lastTranscript);
   const completed=JSON.stringify(next.items.filter(item=>['user','assistant'].includes(item.kind)&&item.status==='completed').map(item=>[item.id,item.text,item.artifacts]));
   if(completedRevision&&completedRevision!==completed)historySettled=false;
   completedRevision=completed;
   if(!workerTerminal.has(next.task.status)||status!==next.task.status)historySettled=false;
   status=next.task.status;previous=next;
   io.observation(next);io.observationFailed(false);
  }catch(error){if(live){unavailableError(error);io.observationFailed(true)}}finally{observing=false}
 };
 const history=async()=>{
  const now=(io.now??Date.now)();
  if(!live||unavailable||readingHistory||historySettled||now-lastHistory<2500)return;
  readingHistory=true;lastHistory=now;
  try{
   const value=await io.readHistory(io.id);
   if(!live)return;
   if(!value||!Array.isArray(value.messages))throw new Error('worker_history_unavailable');
   lastTranscript=value;
   io.history(value);io.historyFailed(false);
   historySettled=workerTerminal.has(value.status)&&workerTerminal.has(status);
  }catch(error){if(live){unavailableError(error);io.historyFailed(true)}}finally{readingHistory=false}
 };
 // History can traverse several native pages. It must not hold live updates
 // behind that read, and neither channel may overlap its own previous request.
 return {read:async()=>{await Promise.all([observe(),history()])},close:()=>{live=false}};
}
