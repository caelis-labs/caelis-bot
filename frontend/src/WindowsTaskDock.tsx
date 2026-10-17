import {useEffect,useRef,useState} from 'react';
import type {PointerEvent} from 'react';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import type {MessageKey} from './i18n/catalogs';
import {preserveTaskOrder} from './windows-task-order';

type Task={id:string;prompt:string;status:string;locked:boolean;targetLabel?:string};
export function WindowsTaskDock(){
 const {t}=useI18n();const [tasks,setTasks]=useState<Task[]>([]),[notice,setNotice]=useState(''),[busy,setBusy]=useState('');
 const [images,setImages]=useState<Record<string,string>>({});
 const [expanded,setExpanded]=useState(false);
 const hover=useRef<ReturnType<typeof setTimeout>|null>(null);
 const hovering=useRef(false);
 const cancelHover=()=>{if(hover.current)clearTimeout(hover.current);hover.current=null;};
 const expand=(value:boolean)=>{cancelHover();void desktop('SetWindowsTaskDockExpanded',value).catch(()=>{});};
 const hoverDock=(inside:boolean)=>{hovering.current=inside;cancelHover();hover.current=setTimeout(()=>{if(!drag.current)expand(inside);},inside?160:420);};
 useEffect(()=>()=>cancelHover(),[]);
 const drag=useRef<{id:string;x:number;y:number;pointer:number;moved:boolean}|null>(null);
 const cards=useRef(new Map<string,HTMLLIElement>());
 const [dragging,setDragging]=useState(''),[before,setBefore]=useState('');
 const dropBefore=(y:number,id:string)=>tasks.find(task=>task.id!==id&&y<(cards.current.get(task.id)?.getBoundingClientRect().top??Infinity)+(cards.current.get(task.id)?.getBoundingClientRect().height??0)/2)?.id??'';
 const cancelDrag=()=>{drag.current=null;setDragging('');setBefore('');};
 useEffect(()=>{const cancel=(e:KeyboardEvent)=>{if(e.key==='Escape'){cancelDrag();expand(false);}};window.addEventListener('keydown',cancel);window.addEventListener('blur',cancelDrag);return()=>{window.removeEventListener('keydown',cancel);window.removeEventListener('blur',cancelDrag);};},[]);
 useEffect(()=>{let active=true;void Promise.all(tasks.map(async task=>[task.id,await desktop<string>('WindowsTaskThumbnail',task.id)] as const)).then(v=>{if(active)setImages(Object.fromEntries(v));}).catch(()=>{});return()=>{active=false;};},[tasks]);
 useEffect(()=>{let active=true;const load=()=>void Promise.all([desktop<Task[]>('TaskDockState'),desktop<boolean>('WindowsTaskDockExpanded')]).then(([v,open])=>{if(active){setExpanded(open);setTasks(previous=>preserveTaskOrder(previous,v??[],hovering.current||!!drag.current));}}).catch(()=>{if(active)setNotice(t('native.taskListChangeFailed'));});const message=(e:Event)=>setNotice((e as CustomEvent<string>).detail);load();window.addEventListener('tasks-changed',load);window.addEventListener('task-notice',message);const timer=setInterval(load,2000);return()=>{active=false;clearInterval(timer);window.removeEventListener('tasks-changed',load);window.removeEventListener('task-notice',message);};},[t]);
 const run=(id:string,method:string,...args:unknown[])=>{if(busy)return;setBusy(id);setNotice('');void desktop(method,id,...args).catch(()=>setNotice(t(method==='DismissTaskTerminal'?'native.taskCloseFailed':'native.taskTerminalUnsettled'))).finally(()=>setBusy(''));};
 const startDrag=(e:PointerEvent,id:string)=>{if(busy||e.button!==0)return;e.currentTarget.setPointerCapture(e.pointerId);drag.current={id,x:e.clientX,y:e.clientY,pointer:e.pointerId,moved:false};};
 const moveDrag=(e:PointerEvent)=>{const current=drag.current;if(!current||current.pointer!==e.pointerId)return;if(Math.hypot(e.clientX-current.x,e.clientY-current.y)>6)current.moved=true;if(current.moved){setDragging(current.id);setBefore(dropBefore(e.clientY,current.id));}};
 const finishDrag=(e:PointerEvent)=>{const current=drag.current;cancelDrag();if(!current||!current.moved||current.pointer!==e.pointerId)return;
  // Only a completed captured pointer release can open a terminal. Escape,
  // lost capture and blur cannot turn a cancelled gesture into execution.
  if(e.clientX<0||e.clientY<0||e.clientX>innerWidth||e.clientY>innerHeight){run(current.id,'OpenTaskTerminal');expand(false);}
  else run(current.id,'MoveTaskCard',dropBefore(e.clientY,current.id));
 };
 const label=(status:string):MessageKey=>({working:'native.taskRunning',running:'native.taskRunning',approval:'native.taskApproval',waiting_approval:'native.taskApproval',completed:'native.taskFinished',unknown:'native.taskUnknown',unavailable:'native.taskUnavailable',failed:'native.taskFailed',interrupted:'native.taskInterrupted'} as Record<string,MessageKey>)[status]??'native.taskIdle';
 if(!expanded)return <button className="windows-task-compact" data-running={tasks.some(task=>task.status==='running'||task.status==='working')} onMouseEnter={()=>hoverDock(true)} onMouseLeave={()=>{hovering.current=false;cancelHover();}} onFocus={()=>expand(true)} onClick={()=>expand(true)} title={t('native.expandTasks')} aria-label={`${t('native.taskBackgroundTitle')} · ${tasks.length}`}><span aria-hidden="true">●</span><span aria-hidden="true">●</span><span aria-hidden="true">●</span></button>;
 return <main className="windows-task-dock" onMouseEnter={()=>{hovering.current=true;cancelHover();}} onMouseLeave={()=>hoverDock(false)}><header><h2>{t('native.taskBackgroundTitle')}</h2><button disabled={!!busy||!tasks.some(task=>!task.locked)} title={t('native.clearTasksHelp')} onClick={()=>{if(busy)return;setBusy('*');setNotice('');void desktop('ClearTaskCards').catch(()=>setNotice(t('native.taskListChangeFailed'))).finally(()=>setBusy(''));}}>{t('native.clearTasks')}</button></header>{tasks.length===0&&<p>{t('native.taskIdle')}</p>}<ol>{tasks.map((task,index)=><li key={task.id} ref={element=>{if(element)cards.current.set(task.id,element);else cards.current.delete(task.id);}} className={[dragging===task.id?'dragging':'',dragging&&before===task.id?'drop-before':''].join(' ')}>
 <button className="task-drag-handle" disabled={!!busy} title={t('native.taskDragHelp')} aria-label={t('native.taskDragHelp')} onPointerDown={e=>startDrag(e,task.id)} onPointerMove={moveDrag} onPointerUp={finishDrag} onPointerCancel={cancelDrag} onLostPointerCapture={cancelDrag} onKeyDown={e=>{if(e.key==='ArrowUp'&&index>0){e.preventDefault();run(task.id,'MoveTaskCard',tasks[index-1].id);}else if(e.key==='ArrowDown'&&index<tasks.length-1){e.preventDefault();run(task.id,'MoveTaskCard',tasks[index+2]?.id??'');}}}>⠿</button>
 <button disabled={!!busy} title={t('native.openTaskInTerminal')} onClick={()=>run(task.id,'OpenTaskTerminal')}><strong>{task.prompt||t('native.taskPromptMissing')}</strong><span>{task.targetLabel} {t(label(task.status))}</span>{images[task.id]&&<img draggable={false} className="task-thumbnail" src={images[task.id]} alt={t('native.openTaskInTerminal')}/>}</button><div><button disabled={!!busy} onClick={()=>run(task.id,'LockTaskCard',!task.locked)}>{t(task.locked?'native.unlockTask':'native.lockTask')}</button><button disabled={!!busy||task.locked} onClick={()=>run(task.id,'DismissTaskTerminal')}>{t('native.removeTaskPin')}</button></div></li>)}</ol>{notice&&<p role="status">{notice}</p>}</main>;
}
