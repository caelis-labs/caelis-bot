import { desktop } from './desktop';
const mounted=new Set<string>();
export function surfaceMounted(surface:string){mounted.add(surface);}
let pending: {surface:string;token:number}|undefined;
(window as unknown as {prepareSurface:(surface:string,token:number)=>void}).prepareSurface=(surface,token)=>{
 pending={surface,token};
 const open=()=>{if(pending?.token!==token)return;if(!mounted.has(surface)){setTimeout(open,25);return;}
 window.dispatchEvent(new CustomEvent(surface==='chat'?'history-open':'settings-open',{detail:{preservePosition:true}}));};
 open();
};
export function surfaceReady(surface:string) {
 const request=pending;
 if(!request||request.surface!==surface)return;
 // Hidden WKWebViews suspend animation frames. React effects run after the
 // commit; force layout, then acknowledge on a task without waiting for paint
 // in a window which cannot paint until AppKit presents it.
 window.setTimeout(()=>{
  if(pending!==request)return;
  document.documentElement.getBoundingClientRect();
  pending=undefined;
  void desktop('SurfaceReady',surface,request.token);
 },0);
}
