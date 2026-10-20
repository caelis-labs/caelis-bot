import {GearSixIcon} from '@phosphor-icons/react/dist/csr/GearSix';
import {ListChecksIcon} from '@phosphor-icons/react/dist/csr/ListChecks';
import './theme/display';
import {useEffect,useRef,useState} from 'react';
import {History} from './Panel';
import {Settings} from './Settings';
import {I18nProvider} from './i18n';
import {Recovery,SurfaceBoundary} from './SurfaceBoundary';
import {BotSetup} from './BotSetup';
import {SetupLoading} from './SetupLoading';
import {SettingsDialog} from './settings/runtime/SettingsDialog';
import {MessageContent} from './MessageContent';
import {desktop} from './desktop';
import type {WebClient,WebNavigation} from './web-transport';

type Task={id:string;title:string;status:string;machineName?:string;result?:string};
type Conversation={status:string;messages:Array<{id:string;role:string;text:string}>};

export function WebApp({client}:{client:WebClient}){
 const [nav,setNav]=useState<WebNavigation|null>(null),[navigationFailed,setNavigationFailed]=useState(false),[editing,setEditing]=useState<boolean|null>(null),[tasksOpen,setTasksOpen]=useState(false);
 const onboarding=nav?.section==='setup';
 const [preview,setPreview]=useState<{id:string;text:string;truncated:boolean}|null>(null),[notice,setNotice]=useState('');
 const content=useRef<HTMLDivElement>(null);
 const [editorGeneration,setEditorGeneration]=useState(0);const editorActive=useRef(false);
 const updateEditor=(value:boolean)=>{if(value&&!editorActive.current)setEditorGeneration(n=>n+1);editorActive.current=value;setEditing(value);};
 useEffect(()=>{
  const navigate=(event:Event)=>{setNavigationFailed(false);setNav((event as CustomEvent<WebNavigation>).detail);};
  const navigationFailure=()=>setNavigationFailed(true);
  const showPreview=(event:Event)=>{const id=(event as CustomEvent<{id:string}>).detail.id;void desktop<{text:string;truncated:boolean}>('PreviewArtifact',id).then(v=>setPreview({id,...v})).catch(()=>setNotice('此文件暂时无法预览。'));};
  window.addEventListener('web-navigation',navigate);window.addEventListener('web-preview',showPreview);
  window.addEventListener('web-navigation-failed',navigationFailure);
  let active=true;const renew=()=>void client.claim().then(value=>{if(active)updateEditor(value)}).catch(()=>{if(active)setEditing(false)});
  renew();const timer=setInterval(renew,10000);
  void client.restoreNavigation();
  return()=>{active=false;clearInterval(timer);window.removeEventListener('web-navigation',navigate);window.removeEventListener('web-preview',showPreview);window.removeEventListener('web-navigation-failed',navigationFailure);};
 },[client]);
 useEffect(()=>{
  if(!nav)return;
  document.body.dataset.surface=nav.view==='settings'?'settings':'history';
  window.dispatchEvent(new Event(nav.view==='settings'?'history-close':'settings-close'));
  window.dispatchEvent(new Event(nav.view==='settings'?'settings-open':'history-open'));
 },[nav]);
 useEffect(()=>{
  // This only controls editing affordances. The server checks the lease and
  // epoch on every draft, upload and submit; DOM state grants no authority.
  const apply=()=>content.current?.querySelectorAll<HTMLElement>('.capsule').forEach(node=>{node.inert=!editing;});
  apply();const observer=new MutationObserver(apply);if(content.current)observer.observe(content.current,{childList:true,subtree:true});return()=>observer.disconnect();
 },[editing]);
 return <I18nProvider><SurfaceBoundary><div className="web-app">
  {nav&&!onboarding&&nav.view!=='settings'&&<div className="web-tools"><button aria-label="后台任务" title="后台任务" onClick={()=>setTasksOpen(true)}><ListChecksIcon size={19}/></button><button aria-label="设置" title="设置" onClick={()=>void client.navigation({view:'settings',section:'general'}).catch(()=>setNavigationFailed(true))}><GearSixIcon size={19}/></button></div>}
  {client.fixture&&<div className="web-editing" role="status">测试后端 · 无真实模型或外部任务执行</div>}
  {editing===false&&nav?.view==='chat'&&<div className="web-editing" role="status">另一页面正在编辑。<button onClick={()=>void client.claim(true).then(updateEditor)}>在此继续编辑</button></div>}
  <div className="web-content" ref={content}>
   {navigationFailed&&<Recovery onRetry={()=>void client.restoreNavigation()}/>}
   {!nav&&!navigationFailed&&<SetupLoading/>}
   {onboarding?<div className="web-settings"><main className="settings-window setup-window"><div className="setup-page"><BotSetup onDone={()=>void client.navigation({view:'chat'}).catch(()=>setNavigationFailed(true))}/></div></main></div>:nav&&<>
    <div className="web-chat" hidden={nav.view==='settings'}><History key={editorGeneration}/></div>
    <div className="web-settings" hidden={nav.view!=='settings'}><Settings onNavigate={section=>void client.navigation({view:'settings',section}).catch(()=>setNavigationFailed(true))}/></div>
   </>}
  </div>
  {nav&&!onboarding&&(tasksOpen||nav.view==='task')&&<Tasks client={client} selected={nav.task} onClose={()=>{setTasksOpen(false);void client.navigation({view:'chat'}).catch(()=>setNavigationFailed(true))}}/>}
  {preview&&<SettingsDialog surfaceClassName="settings-window" portalTarget={document.body} title="文件预览" onClose={()=>setPreview(null)}><pre className="web-preview">{preview.text}</pre>{preview.truncated&&<p>仅显示开头部分，可下载完整文件。</p>}<button onClick={()=>void desktop('SaveArtifactAs',preview.id)}>下载文件</button></SettingsDialog>}
  {notice&&<SettingsDialog surfaceClassName="settings-window" portalTarget={document.body} title="文件" onClose={()=>setNotice('')}><p>{notice}</p></SettingsDialog>}
 </div></SurfaceBoundary></I18nProvider>;
}

function Tasks({client,selected,onClose}:{client:WebClient;selected?:string;onClose:()=>void}){
 const [tasks,setTasks]=useState<Task[]>([]),[id,setID]=useState(selected??''),[conversation,setConversation]=useState<Conversation|null>(null),[error,setError]=useState('');
 useEffect(()=>{let active=true;const read=()=>void client.taskList().then(value=>{if(active)setTasks(value)}).catch(()=>{if(active)setError('暂时无法读取任务，原工作仍保留。')});read();const timer=setInterval(read,2000);return()=>{active=false;clearInterval(timer)}},[client]);
 useEffect(()=>{let active=true;setConversation(null);setError('');const read=()=>{if(id)void client.taskConversation(id).then(value=>{if(active){setConversation(value);setError('')}}).catch(()=>{if(active)setError('暂时无法读取这项工作的记录。')})};read();const timer=setInterval(read,3000);return()=>{active=false;clearInterval(timer)}},[id,client]);
 return <SettingsDialog surfaceClassName="settings-window" portalTarget={document.body} title={tasks.find(task=>task.id===id)?.title??'后台任务'} onClose={onClose}><div className="web-tasks">
  {id?<><button onClick={()=>setID('')}>返回任务</button>{conversation?.messages.map(message=><article key={message.id}><small>{message.role==='user'?'你':'助手'}</small><MessageContent text={message.text} report={setError}/></article>)}{!conversation?.messages.length&&tasks.find(task=>task.id===id)?.result&&<MessageContent text={tasks.find(task=>task.id===id)!.result!} report={setError}/>}<span role="status">{conversation?.status==='running'?'进行中':conversation?.status==='completed'?'已完成':''}</span>{!conversation&&!error&&<p>正在读取…</p>}</>:tasks.length?tasks.map(task=><button key={task.id} className="web-task-row" onClick={()=>setID(task.id)}><strong>{task.title}</strong><span>{({running:'进行中',completed:'已完成',failed:'未完成',unknown:'等待核对',pending:'等待中',stopped:'已停止'} as Record<string,string>)[task.status]??'已保留'}{task.machineName?' · '+task.machineName:''}</span></button>):<p>暂无后台任务。</p>}
  {error&&<p role="status">{error}</p>}
 </div></SettingsDialog>;
}
