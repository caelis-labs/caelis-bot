import {useEffect,useState} from 'react';
import {backend,desktop} from './desktop';
import type {Task,TaskConversation,WorkerTaskObservation} from './backend/contract';
import {useWorkerOutput,workerTaskStatusKey} from './WorkerOutput';
import {useI18n} from './i18n';
import {WorkerChat} from './WorkerChat';

const readHistory=(id:string)=>backend<TaskConversation>('WorkerTaskConversation',id);
const readObservation=(id:string)=>backend<WorkerTaskObservation>('WorkerTaskObservation',id);
export function WorkerTaskWindow(){
 const {t}=useI18n();
 const id=new URLSearchParams(location.search).get('task')??'';
 const [task,setTask]=useState<Task>();
 const output=useWorkerOutput(id,!!id,readHistory,readObservation);
 useEffect(()=>{let live=true;void backend<Task[]>('WorkerTasks').then(tasks=>{if(live)setTask(tasks.find(t=>t.id===id))}).catch(()=>{});return()=>{live=false}},[id]);
 const current=output.observation?.task??task;
 const status=output.observation?.task.status??output.conversation?.status??task?.status;
 const title=current?`${current.title||t('chat.taskTitle')} [${current.machineName||t('chat.taskLocalMachine')}] · ${output.observationFailed?t('chat.taskLastStatus')+' ':''}${t(workerTaskStatusKey(status))}`:'';
 useEffect(()=>{if(title)void desktop('UpdateWorkerTaskWindowTitle',id,title).catch(()=>{})},[id,title]);
 return <WorkerChat id={id} task={current} output={output} hideHeading/>;
}
