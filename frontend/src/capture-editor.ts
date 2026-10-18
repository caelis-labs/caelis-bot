export type Point={x:number;y:number};
export type CaptureRect={x:number;y:number;width:number;height:number};
export type CaptureMark={kind:string;start:Point;end:Point;text?:string;ink:string;width:number};
export type CaptureHistory={marks:CaptureMark[];redo:CaptureMark[]};
// A click confirms the existing selection (initially the whole screen). Use
// CSS pixels for the drag threshold so high-DPI clicks behave identically.
export function captureDragRect(start:Point,end:Point,scale=1):CaptureRect|null {
 if(Math.max(Math.abs(end.x-start.x),Math.abs(end.y-start.y))*scale<3)return null;
 const width=Math.abs(end.x-start.x),height=Math.abs(end.y-start.y);
 if(!width||!height)return null;
 return{x:Math.min(start.x,end.x),y:Math.min(start.y,end.y),width,height};
}
export function captureKeyAction(key:string,ctrl:boolean):'copy'|'save'|'pin'|'all'|'send'|null {
 if(key==='Enter')return 'send';
 if(key==='F3'&&!ctrl)return 'pin';
 if(!ctrl)return null;
 return({c:'copy',s:'save',t:'pin',a:'all'} as const)[key.toLowerCase() as 'c'|'s'|'t'|'a']??null;
}
// Windows IMEs can report Process/229 for canvas shortcuts even when no text
// composition is active. Preserve real composition and editable-field IME
// handling, but use the physical key for commands on the non-editable canvas.
export function captureCommandKey(e:{key:string;code:string;keyCode:number;isComposing:boolean;repeat:boolean;ctrlKey:boolean},editing:boolean):string|null {
 if(e.repeat||e.isComposing||editing&&e.keyCode===229)return null;
 if(!editing&&(e.key==='Process'||e.key==='Unidentified'||e.keyCode===229)){
  if(e.code==='Enter'||e.code==='NumpadEnter')return 'Enter';
  if(e.code==='Escape'||e.code==='F3')return e.code;
  if(e.ctrlKey&&/^Key[ACSTYZ]$/.test(e.code))return e.code.slice(3).toLowerCase();
  return null;
 }
 return e.key;
}
// Rectangles here are CSS viewport pixels, after the image-to-canvas transform.
// Match the native Mac panel: align to the selection's left edge, prefer below,
// then above, and keep the complete card reachable on a small display.
export function captureToolbarPosition(selection:CaptureRect,viewport:{width:number;height:number},card:{width:number;height:number}):Point {
 const edge=8,gap=10;
 const limit=(n:number,size:number,total:number)=>Math.max(edge,Math.min(n,Math.max(edge,total-size-edge)));
 const below=selection.y+selection.height+gap;
 return{x:limit(selection.x,card.width,viewport.width),y:limit(below+card.height<=viewport.height-edge?below:selection.y-card.height-gap,card.height,viewport.height)};
}
export const emptyHistory=():CaptureHistory=>({marks:[],redo:[]});
export function addCaptureMark(h:CaptureHistory,m:CaptureMark):CaptureHistory{return{marks:[...h.marks,m],redo:[]};}
export function undoCaptureMark(h:CaptureHistory):CaptureHistory{return h.marks.length?{marks:h.marks.slice(0,-1),redo:[...h.redo,h.marks.at(-1)!]}:h;}
export function redoCaptureMark(h:CaptureHistory):CaptureHistory{return h.redo.length?{marks:[...h.marks,h.redo.at(-1)!],redo:h.redo.slice(0,-1)}:h;}
export function moveCaptureSelection(r:CaptureRect,dx:number,dy:number,width:number,height:number):CaptureRect{
 return{...r,x:Math.max(0,Math.min(width-r.width,r.x+dx)),y:Math.max(0,Math.min(height-r.height,r.y+dy))};
}
// Original image coordinates; callers clip and export the same flattened pixels.
export function paintCaptureMark(ctx:CanvasRenderingContext2D,m:CaptureMark){
 const x=Math.floor(Math.min(m.start.x,m.end.x)),y=Math.floor(Math.min(m.start.y,m.end.y));
 const w=Math.ceil(Math.abs(m.end.x-m.start.x)),h=Math.ceil(Math.abs(m.end.y-m.start.y));
 ctx.strokeStyle=m.ink;ctx.fillStyle=m.ink;ctx.lineWidth=m.width;
 if(m.kind==='redact'){ctx.fillStyle='#000';ctx.fillRect(x,y,w,h);}
 else if(m.kind==='mosaic'){
  const block=Math.max(8,Math.round(m.width*4));
  for(let by=y;by<y+h;by+=block)for(let bx=x;bx<x+w;bx+=block){
   const bw=Math.min(block,x+w-bx,ctx.canvas.width-bx),bh=Math.min(block,y+h-by,ctx.canvas.height-by);
   if(bx<0||by<0||bw<=0||bh<=0)continue;
   const data=ctx.getImageData(bx,by,bw,bh).data;let r=0,g=0,b=0;
   for(let i=0;i<data.length;i+=4){r+=data[i];g+=data[i+1];b+=data[i+2];}
   const count=data.length/4;ctx.fillStyle=`rgb(${Math.round(r/count)} ${Math.round(g/count)} ${Math.round(b/count)})`;ctx.fillRect(bx,by,bw,bh);
  }
 }else if(m.kind==='rectangle')ctx.strokeRect(x,y,w,h);
 else if(m.kind==='text'){ctx.font=`${Math.max(14,m.width*5)}px "Microsoft YaHei UI",sans-serif`;ctx.fillText(m.text||'',m.start.x,m.start.y);}
 else{
  ctx.beginPath();ctx.moveTo(m.start.x,m.start.y);ctx.lineTo(m.end.x,m.end.y);ctx.stroke();
  const a=Math.atan2(m.end.y-m.start.y,m.end.x-m.start.x),size=Math.max(12,m.width*4);
  ctx.beginPath();ctx.moveTo(m.end.x-size*Math.cos(a-.5),m.end.y-size*Math.sin(a-.5));ctx.lineTo(m.end.x,m.end.y);ctx.lineTo(m.end.x-size*Math.cos(a+.5),m.end.y-size*Math.sin(a+.5));ctx.stroke();
 }
}
