import {useEffect,useState} from 'react';
import {backend,desktop} from './desktop';
import type {Task,TaskConversation,WorkerTaskObservation} from './backend/contract';
import {useWorkerOutput} from './WorkerOutput';
import {WorkerChat} from './WorkerChat';

const readHistory=(id:string)=>backend<TaskConversation>('WorkerTaskConversation',id);
const readObservation=(id:string)=>backend<WorkerTaskObservation>('WorkerTaskObservation',id);
export function WorkerTaskWindow(){
 const id=new URLSearchParams(location.search).get('task')??'';
 const [task,setTask]=useState<Task>();
 const output=useWorkerOutput(id,!!id,readHistory,readObservation);
 useEffect(()=>{let live=true;void backend<Task[]>('WorkerTasks').then(tasks=>{if(live)setTask(tasks.find(t=>t.id===id))}).catch(()=>{});return()=>{live=false}},[id]);
 return <WorkerChat id={id} task={output.observation?.task??task} output={output} onClose={()=>void desktop('CloseWorkerTaskWindow',id).catch(()=>{})}/>;
}
