import {ChatSettingsButton} from './ChatSettingsButton';
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
import {WebTasks,useWebTasks} from './WebTasks';
import {desktop} from './desktop';
import {settingsHasModal} from './context-navigation';
import type {WebClient,WebNavigation} from './web-transport';


export function WebApp({client}:{client:WebClient}){
 const [nav,setNav]=useState<WebNavigation|null>(null),[navigationFailed,setNavigationFailed]=useState(false),[editing,setEditing]=useState<boolean|null>(null),[tasksOpen,setTasksOpen]=useState(false);
 const onboarding=nav?.section==='setup';
 const taskState=useWebTasks(client,!!nav&&!onboarding);
 // Settings overlays the retained workspace; it must not change its width or
 // selected result while chat scrolling/drafts remain mounted underneath.
 const retainedWorkspace=useRef<WebNavigation>({view:'chat'});
 if(nav&&nav.view!=='settings')retainedWorkspace.current=nav;
 const workspaceNav=nav?.view==='settings'?retainedWorkspace.current:nav;
 const taskOpen=tasksOpen||workspaceNav?.view==='task';
 const lastTask=useRef('');
 if(nav?.task)lastTask.current=nav.task;
 const openTasks=()=>{if(settingsHasModal())return;const id=lastTask.current||taskState.tasks[0]?.id;if(id)void client.navigation({view:'task',task:id}).catch(()=>setNavigationFailed(true));else setTasksOpen(true);};
 const closeTasks=()=>{setTasksOpen(false);void client.navigation({view:'chat'}).then(()=>requestAnimationFrame(()=>workspace.current?.querySelector<HTMLElement>('.composer-editor[contenteditable=true]')?.focus({preventScroll:true}))).catch(()=>setNavigationFailed(true));};
 const [preview,setPreview]=useState<{id:string;text:string;truncated:boolean}|null>(null),[notice,setNotice]=useState('');
 const content=useRef<HTMLDivElement>(null),workspace=useRef<HTMLDivElement>(null),settingsPanel=useRef<HTMLDivElement>(null);
 const settingsOpen=nav?.view==='settings'&&!onboarding;
 const [editorGeneration,setEditorGeneration]=useState(0);const editorActive=useRef(false);
 const updateEditor=(value:boolean)=>{if(value&&!editorActive.current)setEditorGeneration(n=>n+1);editorActive.current=value;setEditing(value);};
 useEffect(()=>{
  const navigate=(event:Event)=>{setNavigationFailed(false);setTasksOpen(false);setNav((event as CustomEvent<WebNavigation>).detail);};
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
  if(nav.view==='settings')window.dispatchEvent(new CustomEvent('settings-open',{detail:{section:nav.section}}));
  else{window.dispatchEvent(new Event('settings-close'));window.dispatchEvent(new CustomEvent('history-open',{detail:{preservePosition:true}}));}
 },[nav]);
 useEffect(()=>{
  if(!settingsOpen)return;
  const prior=document.activeElement as HTMLElement|null;
  if(workspace.current)workspace.current.inert=true;
  const frame=requestAnimationFrame(()=>settingsPanel.current?.querySelector<HTMLElement>('.settings-navigation')?.focus());
  return()=>{cancelAnimationFrame(frame);if(workspace.current)workspace.current.inert=false;if(prior?.isConnected)prior.focus({preventScroll:true});};
 },[settingsOpen]);
 useEffect(()=>{
  // This only controls editing affordances. The server checks the lease and
  // epoch on every draft, upload and submit; DOM state grants no authority.
  const apply=()=>content.current?.querySelectorAll<HTMLElement>('.compose-area').forEach(node=>{node.inert=!editing;});
  apply();const observer=new MutationObserver(apply);if(content.current)observer.observe(content.current,{childList:true,subtree:true});return()=>observer.disconnect();
 },[editing]);
 return <I18nProvider><SurfaceBoundary><div className="web-app">
  {client.fixture&&<div className="web-editing" role="status">测试后端 · 无真实模型或外部任务执行</div>}
  {editing===false&&(nav?.view==='chat'||nav?.view==='task')&&<div className="web-editing" role="status">另一页面正在编辑。<button onClick={()=>void client.claim(true).then(updateEditor)}>在此继续编辑</button></div>}
  <div className="web-content" ref={content}>
   {navigationFailed&&<Recovery message="暂时无法打开此页面，请重试。" onRetry={()=>void client.restoreNavigation()}/>}
   {!nav&&!navigationFailed&&<SetupLoading/>}
   {onboarding?<div className="web-settings"><main className="settings-window setup-window"><div className="setup-page"><BotSetup onDone={()=>void client.navigation({view:'chat'}).catch(()=>setNavigationFailed(true))}/></div></main></div>:nav&&<>
    <div ref={workspace} className={`web-workspace${taskOpen?' has-tasks':''}${taskOpen?' task-focused':''}`}>
     <div className="web-chat">
      <div className="web-tools">{(taskState.tasks.length>0||taskState.error)&&<button className="web-task-toggle" aria-label="后台任务" title="后台任务" aria-expanded={!!taskOpen} onClick={taskOpen?closeTasks:openTasks}><ListChecksIcon size={19}/></button>}</div>
      <History composerGeneration={editorGeneration} settingsEntry={<ChatSettingsButton/>}/>
     </div>
     <WebTasks active={!!taskOpen&&!settingsOpen} client={client} tasks={taskState.tasks} selected={workspaceNav?.task} onSelect={id=>void client.navigation({view:'task',task:id}).catch(()=>setNavigationFailed(true))} onClose={closeTasks} listError={taskState.error} onRetryList={taskState.retry}/>
    </div>
    <div className="web-settings-layer" hidden={!settingsOpen} onPointerDown={event=>{if(event.target===event.currentTarget&&!settingsHasModal())void desktop('CloseSettings').catch(()=>setNavigationFailed(true));}}>
     <div className="web-settings web-settings-panel" ref={settingsPanel} role="dialog" aria-modal="true" aria-label="CaelisBot" data-settings-panel tabIndex={-1} onKeyDown={event=>{
      if(event.key!=='Tab'||settingsHasModal())return;
      const nodes=[...settingsPanel.current!.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),textarea:not(:disabled),select:not(:disabled),a[href],[tabindex="0"]')].filter(n=>n.getClientRects().length&&!n.closest('[inert]'));
      const first=nodes[0],last=nodes.at(-1);
      if(event.shiftKey&&(document.activeElement===first||document.activeElement===settingsPanel.current)){event.preventDefault();last?.focus();}
      else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first?.focus();}
     }}><Settings onNavigate={section=>void client.navigation({view:'settings',section}).catch(()=>setNavigationFailed(true))}/></div>
    </div>
   </>}
  </div>
  {preview&&<SettingsDialog surfaceClassName="settings-window" portalTarget={document.body} title="文件预览" onClose={()=>setPreview(null)}><pre className="web-preview">{preview.text}</pre>{preview.truncated&&<p>仅显示开头部分，可下载完整文件。</p>}<button onClick={()=>void desktop('SaveArtifactAs',preview.id)}>下载文件</button></SettingsDialog>}
  {notice&&<SettingsDialog surfaceClassName="settings-window" portalTarget={document.body} title="文件" onClose={()=>setNotice('')}><p>{notice}</p></SettingsDialog>}
 </div></SurfaceBoundary></I18nProvider>;
}
