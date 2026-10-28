import {useLayoutEffect,useMemo,useRef,useState,type ReactNode} from 'react';
import type {Task} from './backend/contract';
import {CopyText,MessageContent} from './MessageContent';
import {ArtifactLink} from './ArtifactLink';
import {ChatScroll} from './chat-scroll';
import {useI18n} from './i18n';
import {workerMessages,workerMessageStart,workerLiveReplies} from './worker-messages';
import {workerTerminal} from './worker-output-read';
import {WorkerTaskStatus,type useWorkerOutput} from './WorkerOutput';

type Output=Omit<ReturnType<typeof useWorkerOutput>,'environmentUnavailable'>&{environmentUnavailable?:boolean};

// Presentation only: the same message protocol, Markdown, files and reading
// intent serve the independent native window and the Web split pane.
export function WorkerChat({id,task,output,onClose,heading,closeLabel,active=true,hideHeading=false}:{id:string;task?:Pick<Task,'title'|'status'|'machineName'>;output:Output;onClose?:()=>void;heading?:ReactNode;closeLabel?:string;active?:boolean;hideHeading?:boolean}){
 const {t}=useI18n();
 const messages=useMemo(()=>workerMessages(output.conversation,output.observation?.items),[output.conversation,output.observation]);
 const status=output.observation?.task.status??output.conversation?.status??task?.status;
 // A loading render is not a history baseline; wait for the first observation.
 const observed=!!output.observation;
 const previous=useRef<{id:string;active:boolean;observed:boolean;messages:typeof messages;live:Set<string>}>(undefined);
 const live=useMemo(()=>active&&!workerTerminal.has(status??'')?workerLiveReplies(previous.current?.id===id&&previous.current.active&&previous.current.observed?previous.current.messages:undefined,messages,previous.current?.live??new Set()):new Set<string>(),[id,active,messages,status]);
 useLayoutEffect(()=>{previous.current={id,active,observed,messages,live}},[id,active,observed,messages,live]);
 const [error,setError]=useState(''),[limit,setLimit]=useState(40),[following,setFollowing]=useState(true);
 const [startID,setStartID]=useState<string>();
 const start=workerMessageStart(messages,limit,following?undefined:startID);
 const viewport=useRef<HTMLDivElement>(null),content=useRef<HTMLDivElement>(null),scroll=useRef<ChatScroll|null>(null);
 const reading=useRef(new Map<string,{top:number;following:boolean;limit:number;startID?:string}>()),prepend=useRef<number|undefined>(undefined),restore=useRef<{top:number;limit:number}|undefined>(undefined);
 useLayoutEffect(()=>{
  const element=viewport.current;if(!active||!element)return;
  const intent=new ChatScroll(element);scroll.current=intent;
  const old=reading.current.get(id);setLimit(old?.limit??40);setStartID(old?.startID);setError('');
  if(old){intent.following=old.following;restore.current={top:old.top,limit:old.limit};}
  intent.layout();setFollowing(intent.following);
  let watching=true;
  const observer=new ResizeObserver(()=>{if(watching){intent.layout();setFollowing(intent.following)}});
  observer.observe(element);if(content.current)observer.observe(content.current);
  return()=>{watching=false;const saved=reading.current.get(id);reading.current.set(id,{...saved,top:saved?.top??0,following:intent.following,limit:saved?.limit??40});observer.disconnect();scroll.current=null;};
 },[id,active]);
 useLayoutEffect(()=>{
  const element=viewport.current;if(!active||!element)return;
  if(restore.current&&restore.current.limit===limit){element.scrollTop=restore.current.top;restore.current=undefined;}
  if(prepend.current!==undefined){element.scrollTop+=element.scrollHeight-prepend.current;prepend.current=undefined;}
  scroll.current?.layout();
 },[id,active,messages,limit,start]);
 const latest=()=>{scroll.current?.latest();setFollowing(true);setStartID(undefined)};
 const earlier=()=>{
  if(viewport.current)prepend.current=viewport.current.scrollHeight;
  const anchor=messages[Math.max(0,start-40)]?.id,value=messages.length-Math.max(0,start-40);
  if(scroll.current)scroll.current.following=false;
  setFollowing(false);setStartID(anchor);setLimit(value);
  reading.current.set(id,{top:viewport.current?.scrollTop??0,following:false,limit:value,startID:anchor});
 };

 const failed=output.historyFailed||output.observationFailed;
 return <section className="worker-chat" data-updates={output.observationFailed?'unavailable':'available'} aria-label={t('chat.taskPanel')}>
  {!hideHeading&&<header className="worker-chat-heading"><div className="worker-chat-title"><div className="worker-chat-title-line">{heading??<h2>{task?.title||t('chat.taskTitle')}</h2>}<span className="worker-chat-status" role="status">{output.observationFailed&&<>{t('chat.taskLastStatus')} </>}<WorkerTaskStatus status={status}/></span></div><span className="worker-chat-machine">{task?(task.machineName||t('chat.taskLocalMachine')):''}</span></div>{onClose&&<button className="worker-chat-close" onClick={onClose} aria-label={t('chat.closeTaskPanel')}>{closeLabel??t('chat.taskReturn')}</button>}</header>}
  {failed&&<div className="worker-chat-notice" role="status"><span>{t(output.environmentUnavailable?'chat.taskEnvironmentDeleted':'chat.taskUpdateUnavailable')}</span>{!output.environmentUnavailable&&<button className="text-action" onClick={output.retry}>{t('chat.taskRetryRead')}</button>}</div>}
  <div className="worker-chat-body"><div className="worker-chat-scroll" ref={viewport} onScroll={()=>{if(active&&scroll.current&&viewport.current){const follows=scroll.current.scrolled(),anchor=follows?undefined:messages[start]?.id;setFollowing(follows);setStartID(anchor);reading.current.set(id,{top:viewport.current.scrollTop,following:follows,limit,startID:anchor})}}}>
   <div ref={content} className="worker-chat-content">
    {start>0&&<button className="worker-chat-earlier text-action" onClick={earlier}>{t('chat.taskEarlier')}</button>}
    {messages.slice(start).map(message=><article key={message.id} data-worker-message={message.id} className={`worker-message ${message.role}`}>
     {message.text&&<><div className="worker-message-actions"><CopyText text={message.text} report={setError}/></div>
     <MessageContent text={message.text} report={setError} animate={live.has(message.id)}/></>}
     {!!message.artifacts.length&&<div className="artifacts">{message.artifacts.map(file=><ArtifactLink key={file.id} file={file} report={setError}/>)}</div>}
    </article>)}
    {!messages.length&&<p className="worker-chat-empty" role="status">{t(output.conversation?'chat.taskHistoryEmpty':failed?'chat.taskHistoryUnavailable':'chat.taskLoading')}</p>}
   </div>
  </div>{!following&&<button className="worker-chat-latest" onClick={latest}>{t('chat.taskLatest')} ↓</button>}</div>
  {error&&<div className="worker-chat-notice" role="status"><span>{error}</span><button className="text-action" onClick={()=>setError('')}>{t('chat.taskDismiss')}</button></div>}
 </section>;
}
