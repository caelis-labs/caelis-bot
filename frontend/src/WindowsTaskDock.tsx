import {useEffect,useRef,useState} from 'react';
import type {PointerEvent} from 'react';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import type {MessageKey} from './i18n/catalogs';
import {preserveTaskOrder} from './windows-task-order';
import {taskCardFrames} from './windows-task-layout';
import type {TaskLayout} from './windows-task-layout';

type Task={id:string;prompt:string;status:string;locked:boolean;targetLabel?:string};
function DockIcon({kind}:{kind:'terminal'|'lock'|'unlock'|'close'|'clear'}) {
 return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{kind==='terminal'?<><rect x="3" y="4" width="18" height="16" rx="3"/><path d="m7 9 3 3-3 3m6 0h4"/></>:kind==='close'?<path d="m6 6 12 12M6 18 18 6"/>:kind==='clear'?<><path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v5m4-5v5"/></>:<><rect x="5" y="10" width="14" height="11" rx="3"/><path d={kind==='lock'?'M8 10V6a4 4 0 0 1 8 0v4':'M8 10V6a4 4 0 0 1 8 0'} /><path d="M12 14v3"/></>}</svg>;
}
export function WindowsTaskDock(){
 const {t}=useI18n();const [tasks,setTasks]=useState<Task[]>([]),[notice,setNotice]=useState(''),[busy,setBusy]=useState('');
 const [images,setImages]=useState<Record<string,string>>({}),[expanded,setExpanded]=useState(false),[selected,setSelected]=useState('');
 const [layout,setLayout]=useState<TaskLayout>({width:256,height:196,vertical:false,reverse:false});
 const hover=useRef<ReturnType<typeof setTimeout>|null>(null),hovering=useRef(false);
 const cancelHover=()=>{if(hover.current)clearTimeout(hover.current);hover.current=null;};
 const expand=(value:boolean)=>{cancelHover();void desktop('SetWindowsTaskDockExpanded',value).catch(()=>{});};
 const hoverDock=(inside:boolean)=>{hovering.current=inside;cancelHover();hover.current=setTimeout(()=>{if(!drag.current)expand(inside);},inside?160:420);};
 useEffect(()=>()=>cancelHover(),[]);
 const drag=useRef<{id:string;x:number;y:number;pointer:number;moved:boolean}|null>(null),suppressClick=useRef(false);
 const [dragging,setDragging]=useState(''),[before,setBefore]=useState('');
 const frames=taskCardFrames(layout,tasks.length);
 const dropBefore=(x:number,y:number,id:string)=>tasks.find((task,index)=>task.id!==id&&(layout.vertical?(layout.reverse?y>frames[index].top+frames[index].height/2:y<frames[index].top+frames[index].height/2):x<frames[index].left+frames[index].width/2))?.id??'';
 const cancelDrag=()=>{drag.current=null;setDragging('');setBefore('');};
 useEffect(()=>{const cancel=(e:KeyboardEvent)=>{if(e.key==='Escape'){suppressClick.current=true;cancelDrag();expand(false);}};const blur=()=>{suppressClick.current=true;cancelDrag();};window.addEventListener('keydown',cancel);window.addEventListener('blur',blur);return()=>{window.removeEventListener('keydown',cancel);window.removeEventListener('blur',blur);};},[]);
 useEffect(()=>{let active=true;void Promise.all(tasks.map(async task=>[task.id,await desktop<string>('WindowsTaskThumbnail',task.id)] as const)).then(v=>{if(active)setImages(Object.fromEntries(v));}).catch(()=>{});return()=>{active=false;};},[tasks]);
 useEffect(()=>{let active=true;const load=()=>void Promise.all([desktop<Task[]>('TaskDockState'),desktop<boolean>('WindowsTaskDockExpanded'),desktop<TaskLayout>('WindowsTaskDockLayout')]).then(([v,open,geometry])=>{if(active){setExpanded(open);setLayout(geometry);setTasks(previous=>preserveTaskOrder(previous,v??[],hovering.current||!!drag.current));}}).catch(()=>{});const message=(e:Event)=>setNotice((e as CustomEvent<string>).detail);load();window.addEventListener('tasks-changed',load);window.addEventListener('task-notice',message);const timer=setInterval(load,2000);return()=>{active=false;clearInterval(timer);window.removeEventListener('tasks-changed',load);window.removeEventListener('task-notice',message);};},[]);
 const run=(id:string,method:string,...args:unknown[])=>{if(busy)return;setBusy(id);setNotice('');void desktop(method,id,...args).catch(()=>console.warn('Task card action could not finish')).finally(()=>setBusy(''));};
 const startDrag=(e:PointerEvent,id:string)=>{if(busy||e.button!==0)return;suppressClick.current=false;e.currentTarget.setPointerCapture(e.pointerId);drag.current={id,x:e.clientX,y:e.clientY,pointer:e.pointerId,moved:false};};
 const moveDrag=(e:PointerEvent)=>{const current=drag.current;if(!current||current.pointer!==e.pointerId)return;if(Math.hypot(e.clientX-current.x,e.clientY-current.y)>6)current.moved=true;if(current.moved){suppressClick.current=true;setDragging(current.id);setBefore(dropBefore(e.clientX,e.clientY,current.id));}};
 const finishDrag=(e:PointerEvent)=>{const current=drag.current;cancelDrag();if(!current||!current.moved||current.pointer!==e.pointerId)return;
  if(e.clientX<0||e.clientY<0||e.clientX>innerWidth||e.clientY>innerHeight){run(current.id,'OpenTaskTerminal');expand(false);}
  else run(current.id,'MoveTaskCard',dropBefore(e.clientX,e.clientY,current.id));
 };
 const label=(status:string):MessageKey=>({working:'native.taskRunning',running:'native.taskRunning',approval:'native.taskApproval',waiting_approval:'native.taskApproval',awaiting_approval:'native.taskApproval',completed:'native.taskFinished',unknown:'native.taskUnknown',unavailable:'native.taskUnknown',failed:'native.taskFailed',interrupted:'native.taskInterrupted'} as Record<string,MessageKey>)[status]??'native.taskIdle';
 const status=(task:Task)=>[t(label(task.status)),task.targetLabel].filter(Boolean).join(' · ');
 const activeIndex=Math.max(0,tasks.findIndex(task=>task.id===selected)),activeTask=tasks[activeIndex];
 if(!expanded)return <button className="windows-task-compact" data-running={tasks.some(task=>task.status==='running'||task.status==='working')} onMouseEnter={()=>hoverDock(true)} onMouseLeave={()=>{hovering.current=false;cancelHover();}} onFocus={()=>expand(true)} onClick={()=>expand(true)} title={t('native.expandTasks')} aria-label={`${t('native.taskBackgroundTitle')} · ${tasks.length}`}><span aria-hidden="true">●</span><span aria-hidden="true">●</span><span aria-hidden="true">●</span></button>;
 return <main className="windows-task-dock" data-vertical={layout.vertical||undefined} aria-label={t('native.taskBackgroundTitle')} onMouseEnter={()=>{hovering.current=true;cancelHover();}} onMouseLeave={()=>hoverDock(false)}>
  <ol>{tasks.map((task,index)=>{
   const frame=frames[index],prominent=index===activeIndex,neighbor=frames[index<activeIndex?index+1:index-1];
   let clipPath:string|undefined;
   if(!prominent&&neighbor){if(layout.vertical){clipPath=frame.top<neighbor.top?`inset(0 0 ${Math.max(0,frame.height-(neighbor.top-frame.top+6))}px 0)`:`inset(${Math.max(0,neighbor.top+neighbor.height-frame.top-6)}px 0 0 0)`;}else{clipPath=frame.left<neighbor.left?`inset(0 ${Math.max(0,frame.width-(neighbor.left-frame.left+3))}px 0 0)`:`inset(0 0 0 ${Math.max(0,neighbor.left+neighbor.width-frame.left-3)}px)`;}}
   return <li key={task.id} style={{...frame,clipPath,zIndex:prominent?tasks.length+1:tasks.length-Math.abs(index-activeIndex)}} data-prominent={prominent||undefined} className={[dragging===task.id?'dragging':'',dragging&&before===task.id?'drop-before':''].join(' ')} onMouseEnter={()=>setSelected(task.id)} onFocus={()=>setSelected(task.id)}>
    <button className="task-preview" disabled={!!busy} title={t('native.openTaskInTerminal')} aria-label={`${task.prompt||t('native.taskPromptMissing')} · ${status(task)}`} onPointerDown={e=>startDrag(e,task.id)} onPointerMove={moveDrag} onPointerUp={finishDrag} onPointerCancel={()=>{suppressClick.current=true;cancelDrag();}} onLostPointerCapture={cancelDrag} onClick={e=>{if(e.detail===0||!suppressClick.current)run(task.id,'OpenTaskTerminal')}} onKeyDown={e=>{if(e.altKey&&e.key==='ArrowUp'&&index>0){e.preventDefault();run(task.id,'MoveTaskCard',tasks[index-1].id);}else if(e.altKey&&e.key==='ArrowDown'&&index<tasks.length-1){e.preventDefault();run(task.id,'MoveTaskCard',tasks[index+2]?.id??'');}}}>
     <span className="task-preview-image">{images[task.id]?<img draggable={false} src={images[task.id]} alt=""/>:<DockIcon kind="terminal"/>}</span>
     {!layout.vertical&&<span className="task-caption"><strong>{task.prompt||t('native.taskPromptMissing')}</strong><small>{status(task)}</small></span>}
    </button>
    <div className="task-card-actions"><button disabled={!!busy} title={t(task.locked?'native.unlockTask':'native.lockTask')} aria-label={t(task.locked?'native.unlockTask':'native.lockTask')} aria-pressed={task.locked} onClick={()=>run(task.id,'LockTaskCard',!task.locked)}><DockIcon kind={task.locked?'lock':'unlock'}/></button><button disabled={!!busy||task.locked} title={t('native.removeTaskPin')} aria-label={t('native.removeTaskPin')} onClick={()=>run(task.id,'DismissTaskTerminal')}><DockIcon kind="close"/></button></div>
   </li>;
  })}</ol>
  {layout.vertical&&activeTask&&<div className="task-stack-caption task-caption"><strong>{activeTask.prompt||t('native.taskPromptMissing')}</strong><small>{status(activeTask)}</small></div>}
  {!tasks.length&&<p className="task-empty">{t('native.taskIdle')}</p>}
  {tasks.length>0&&<button className="task-clear" disabled={!!busy||!tasks.some(task=>!task.locked)} title={t('native.clearTasksHelp')} aria-label={t('native.clearTasks')} onClick={()=>{if(busy)return;setBusy('*');void desktop('ClearTaskCards').catch(()=>console.warn('Task cards could not be cleared')).finally(()=>setBusy(''));}}><DockIcon kind="clear"/></button>}
  {notice&&<p className="task-dock-notice" role="status">{notice}</p>}
 </main>;
}
