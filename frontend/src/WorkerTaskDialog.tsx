import {useEffect,useLayoutEffect,useRef,useState} from 'react';
import {backend,desktop} from './desktop';
import type {Task,TaskConversation,WorkerTaskObservation} from './backend/contract';
import {isWebSurface} from './transport';
import {useI18n} from './i18n';
import {SettingsDialog} from './settings/runtime/SettingsDialog';
import {useWorkerOutput,WorkerOutput,WorkerTaskStatus} from './WorkerOutput';

const readHistory=(id:string)=>backend<TaskConversation>('WorkerTaskConversation',id);
const readObservation=(id:string)=>backend<WorkerTaskObservation>('WorkerTaskObservation',id);

// Selecting a task opens a view in A. The resident Composer stays mounted, and
// this view has no creation, continuation, decision or model-control authority.
export function WorkerTaskDialog({active}:{active:boolean}){
 const {t}=useI18n();
 const [selected,setSelected]=useState(''),[error,setError]=useState(''),[details,setDetails]=useState<Record<string,Task>>({});
 const closing=useRef(false),generation=useRef(0),scroll=useRef<HTMLDivElement>(null),positions=useRef(new Map<string,number>());
 useEffect(()=>{
  if(!active||isWebSurface())return;
  let live=true,reading=false;
  const read=async()=>{
   if(reading||closing.current)return;reading=true;const current=generation.current;
   try{const id=await desktop<string>('WorkerTaskSelection');if(live&&current===generation.current)setSelected(id);}catch{/* Keep an already visible original task during transient read failure. */}finally{reading=false;}
  };
  void read();const timer=window.setInterval(()=>void read(),500);
  return()=>{live=false;window.clearInterval(timer);};
 },[active]);
 useEffect(()=>{
  if(!active||!selected||isWebSurface())return;
  let live=true;
  void backend<Task[]>('WorkerTasks').then(tasks=>{const task=tasks.find(value=>value.id===selected);if(live&&task)setDetails(old=>Object.fromEntries([...Object.entries(old).filter(([id])=>id!==selected).slice(-4),[selected,task]]));}).catch(()=>{});
  return()=>{live=false;};
 },[active,selected]);
 const output=useWorkerOutput(selected,active&&!isWebSurface(),readHistory,readObservation);
 const task=output.observation?.task??details[selected];
 useLayoutEffect(()=>{if(scroll.current)scroll.current.scrollTop=positions.current.get(selected)??0;setError('');},[selected,active]);
 const close=async()=>{
  if(closing.current)return;closing.current=true;generation.current++;
  try{await desktop('CloseWorkerTask');setSelected('');setError('');}catch{setError(t('chat.actionFailed'));}finally{closing.current=false;}
 };
 if(!active||!selected||isWebSurface())return null;
 return <SettingsDialog portalTarget={document.body} title={task?.title||t('chat.taskTitle')} className="draft-image-viewer" onClose={()=>void close()}>
  <p className="settings-note">{task?<><WorkerTaskStatus status={task.status}/> · {t('chat.taskMachine',{name:task.machineName||t('chat.taskLocalMachine')})}</>:t('chat.taskLoading')}</p>
  <div ref={scroll} style={{overflowY:'auto',minHeight:0,maxHeight:'65vh'}} onScroll={()=>{if(scroll.current)positions.current.set(selected,scroll.current.scrollTop);}}>
   <WorkerOutput {...output} items={output.observation?.items} report={setError} onRetry={output.retry}/>
  </div>
  <p className="settings-note">{t('chat.taskInteractionNote')}</p>
  {error&&<p className="settings-note" role="status">{error}</p>}
 </SettingsDialog>;
}
