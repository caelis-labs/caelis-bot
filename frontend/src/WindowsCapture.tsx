import {useEffect,useLayoutEffect,useRef,useState} from 'react';
import type {ComponentProps,CSSProperties} from 'react';
import {CursorIcon} from '@phosphor-icons/react/dist/csr/Cursor';
import {ArrowUpRightIcon} from '@phosphor-icons/react/dist/csr/ArrowUpRight';
import {RectangleIcon} from '@phosphor-icons/react/dist/csr/Rectangle';
import {TextTIcon} from '@phosphor-icons/react/dist/csr/TextT';
import {GridNineIcon} from '@phosphor-icons/react/dist/csr/GridNine';
import {SquareIcon} from '@phosphor-icons/react/dist/csr/Square';
import {PaletteIcon} from '@phosphor-icons/react/dist/csr/Palette';
import {ArrowUUpLeftIcon} from '@phosphor-icons/react/dist/csr/ArrowUUpLeft';
import {ArrowUUpRightIcon} from '@phosphor-icons/react/dist/csr/ArrowUUpRight';
import {CopyIcon} from '@phosphor-icons/react/dist/csr/Copy';
import {DownloadSimpleIcon} from '@phosphor-icons/react/dist/csr/DownloadSimple';
import {PushPinIcon} from '@phosphor-icons/react/dist/csr/PushPin';
import {XIcon} from '@phosphor-icons/react/dist/csr/X';
import {SparkleIcon} from '@phosphor-icons/react/dist/csr/Sparkle';
import {BrowserIcon} from '@phosphor-icons/react/dist/csr/Browser';
import {MonitorIcon} from '@phosphor-icons/react/dist/csr/Monitor';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import {addCaptureMark,captureCommandKey,captureDragRect,captureKeyAction,captureToolbarPosition,emptyHistory,moveCaptureSelection,paintCaptureMark,redoCaptureMark,undoCaptureMark} from './capture-editor';
import type {CaptureHistory,CaptureMark,CaptureRect,Point} from './capture-editor';
type Document={id:string;image:string;width:number;height:number;window:CaptureRect;context:boolean;contextAvailable:boolean;outcome:string;imageCapability:string;message:string;presentation:number;diagnostics?:boolean};
const clamp=(n:number,max:number)=>Math.max(0,Math.min(n,max));
function CaptureButton({label,children,...props}:ComponentProps<'button'>&{label:string}) {
 return <button type="button" className="capture-icon-button" title={label} aria-label={label} {...props}>{children}</button>;
}

export function WindowsCapture(){
 const params=new URLSearchParams(location.search),slot=params.get('slot')||'';
 const [id,setID]=useState(params.get('id')||'');
 useEffect(()=>{
  if(!slot)return;
  let live=true,received=false;
  const open=(event:Event)=>{received=true;setID((event as CustomEvent<string>).detail);};
  window.addEventListener('capture-open',open);
  void desktop<string>('WindowsCaptureSlot',slot).then(value=>{if(live&&!received)setID(value);}).catch(()=>{});
  return()=>{live=false;window.removeEventListener('capture-open',open);};
 },[slot]);
 return id?<WindowsCaptureEditor key={id} id={id}/>:null;
}
function WindowsCaptureEditor({id}:{id:string}){
 const {t}=useI18n();
 const [doc,setDoc]=useState<Document|null>(null),[selection,setSelection]=useState<CaptureRect|null>(null),[history,setHistory]=useState<CaptureHistory>(emptyHistory);
 const [kind,setKind]=useState('select'),[note,setNote]=useState(''),[context,setContext]=useState(false),[error,setError]=useState(''),[busy,setBusy]=useState(false);
 const [text,setText]=useState(''),[ink,setInk]=useState('#e63854'),[width,setWidth]=useState(3),[pointer,setPointer]=useState<Point|null>(null);
 const workspace=useRef<HTMLElement>(null),canvas=useRef<HTMLCanvasElement>(null),magnifier=useRef<HTMLCanvasElement>(null),source=useRef<HTMLImageElement|null>(null),drag=useRef<CaptureMark|null>(null);
 const initialized=useRef(false),performing=useRef(false),acknowledged=useRef(0);
 const [imageLoaded,setImageLoaded]=useState(false),[confirmed,setConfirmed]=useState(false);
 const toolbar=useRef<HTMLDivElement>(null);
 const [selecting,setSelecting]=useState(false),[toolbarPoint,setToolbarPoint]=useState<Point|null>(null),[widthOpen,setWidthOpen]=useState(false);
 const showToolbar=confirmed&&!!selection&&selection.width>0&&selection.height>0&&!selecting;
 const locked=busy||doc?.outcome==='unknown'||doc?.outcome==='accepted';
 const report=(phase:string)=>{if(doc?.diagnostics)void desktop('WindowsCaptureInput',id,phase).catch(()=>{});};
 useEffect(()=>{
  if(!doc?.diagnostics)return;
  const focus=()=>report('dom_focus'),blur=()=>report('dom_blur');
  window.addEventListener('focus',focus);window.addEventListener('blur',blur);
  return()=>{window.removeEventListener('focus',focus);window.removeEventListener('blur',blur);};
 },[doc?.diagnostics]);
 useEffect(()=>{
  const focus=()=>workspace.current?.focus({preventScroll:true});
  window.addEventListener('capture-focus',focus);
  return()=>window.removeEventListener('capture-focus',focus);
 },[]);
 useLayoutEffect(()=>{
  const panel=toolbar.current,c=canvas.current;
  if(!showToolbar||!panel||!c||!selection||!doc){setToolbarPoint(null);return;}
  const position=()=>{
   const r=c.getBoundingClientRect();
   setToolbarPoint(captureToolbarPosition({x:r.left+selection.x*r.width/doc.width,y:r.top+selection.y*r.height/doc.height,width:selection.width*r.width/doc.width,height:selection.height*r.height/doc.height},{width:window.innerWidth,height:window.innerHeight},{width:panel.offsetWidth,height:panel.offsetHeight}));
  };
  position();const observer=new ResizeObserver(position);observer.observe(panel);observer.observe(c);window.addEventListener('resize',position);
  return()=>{observer.disconnect();window.removeEventListener('resize',position);};
 },[showToolbar,selection,doc?.width,doc?.height,doc?.outcome]);
 const refresh=async()=>{
  const d=await desktop<Document>('WindowsCaptureDocument',id);setDoc(d);
  if(!initialized.current){setContext(d.context);setSelection({x:0,y:0,width:d.width,height:d.height});setConfirmed(d.outcome!=='draft');initialized.current=true;}
  if(source.current?.src!==new URL(d.image,location.href).href){setImageLoaded(false);const im=new Image();im.onload=()=>{source.current=im;setImageLoaded(true);if(d.diagnostics)void desktop('WindowsCaptureInput',id,'image_decoded').catch(()=>{});};im.onerror=()=>setError(t('native.capture.windowsImageFailed'));im.src=d.image;}
 };
 useEffect(()=>{
  const receipt=()=>void refresh().catch(()=>{if(!initialized.current)setError(t('native.capture.windowsImageFailed'));});receipt();window.addEventListener('capture-receipt',receipt);
  const timer=window.setInterval(()=>void desktop('RefreshWindowsCapture',id).catch(()=>{}),5000);
  return()=>{window.removeEventListener('capture-receipt',receipt);window.clearInterval(timer);};
 },[]);
 const paintSelection=(ctx:CanvasRenderingContext2D,r:CaptureRect)=>{
  ctx.save();ctx.beginPath();ctx.rect(r.x,r.y,r.width,r.height);ctx.clip();
  for(const m of history.marks)paintCaptureMark(ctx,m);
  if(drag.current&&drag.current.kind!=='select')paintCaptureMark(ctx,drag.current);ctx.restore();
 };
 const render=(d=doc)=>{
  const c=canvas.current,im=source.current;if(!c||!im||!d)return;c.width=d.width;c.height=d.height;
  const ctx=c.getContext('2d')!;ctx.drawImage(im,0,0,d.width,d.height);
  // The initial full-screen selection is only a keyboard default. Until the
  // user confirms or drags a region, dim the whole image to signal capture mode.
  const highlight=selecting?(drag.current&&dragRect(drag.current)):confirmed?selection:null;
  if(!highlight){ctx.fillStyle='#0006';ctx.fillRect(0,0,d.width,d.height);return;}
  if(selection){
   paintSelection(ctx,selection);ctx.fillStyle='#0006';ctx.fillRect(0,0,d.width,selection.y);ctx.fillRect(0,selection.y+selection.height,d.width,d.height-selection.y-selection.height);ctx.fillRect(0,selection.y,selection.x,selection.height);ctx.fillRect(selection.x+selection.width,selection.y,d.width-selection.x-selection.width,selection.height);
   // Keep the outline visible against both light and dark pixels and inside
   // the screen edges. Its width is stable in CSS pixels at every DPI.
   const unit=d.width/Math.max(1,c.getBoundingClientRect().width),inset=Math.min(unit*1.5,selection.width/2,selection.height/2);
   const r={x:selection.x+inset,y:selection.y+inset,width:Math.max(0,selection.width-inset*2),height:Math.max(0,selection.height-inset*2)};
   ctx.strokeStyle='#163b65';ctx.lineWidth=3*unit;ctx.strokeRect(r.x,r.y,r.width,r.height);
   ctx.strokeStyle='#70c8ff';ctx.lineWidth=unit;ctx.strokeRect(r.x,r.y,r.width,r.height);
  }
 };
 useLayoutEffect(()=>{
  if(!imageLoaded||!doc)return;
  render();
  if(acknowledged.current===doc.presentation)return;
  // Hidden WebViews may throttle requestAnimationFrame indefinitely. Commit
  // the decoded image and canvas before asking native code to reveal it.
  const timer=window.setTimeout(()=>{
   acknowledged.current=doc.presentation;
   void desktop('WindowsCaptureReady',id,doc.presentation).catch(()=>setError(t('native.capture.windowsImageFailed')));
  },0);
  return()=>window.clearTimeout(timer);
 },[doc,selection,history,imageLoaded,confirmed,selecting]);
 useEffect(()=>{
  const c=magnifier.current,im=source.current;if(!c||!im||!pointer||!doc)return;
  c.width=128;c.height=128;const ctx=c.getContext('2d')!;ctx.imageSmoothingEnabled=false;ctx.fillStyle='#151922';ctx.fillRect(0,0,128,128);
  ctx.drawImage(im,pointer.x-16,pointer.y-16,32,32,0,0,128,128);ctx.strokeStyle='#57aaff';ctx.lineWidth=1;
  ctx.beginPath();ctx.moveTo(64,0);ctx.lineTo(64,128);ctx.moveTo(0,64);ctx.lineTo(128,64);ctx.stroke();
 },[pointer,doc]);
 const point=(e:{currentTarget:HTMLCanvasElement;clientX:number;clientY:number})=>{const r=e.currentTarget.getBoundingClientRect();return{x:Math.round(clamp((e.clientX-r.left)*doc!.width/r.width,doc!.width)),y:Math.round(clamp((e.clientY-r.top)*doc!.height/r.height,doc!.height))};};
 const dragRect=(m:CaptureMark)=>captureDragRect(m.start,m.end,(canvas.current?.getBoundingClientRect().width||doc!.width)/doc!.width);
 const select=(r:CaptureRect)=>{setSelection(r);setConfirmed(true);setHistory(emptyHistory());setError('');};
 const perform=async(action:string)=>{
  if(!doc||!selection||selection.width<1||selection.height<1||performing.current||locked)return;
  if(action==='send'&&(doc.imageCapability!=='supported'||doc.outcome==='pinned')){report('send_unavailable');return;}
  report(`action_${action}`);
  performing.current=true;setBusy(true);setError('');
  try{
   if(doc.outcome==='pinned'){
    await desktop('ExportWindowsCapture',{id,selection,png:'',note:'',context:false,action});return;
   }
   // Mosaic reads absolute image pixels. Flatten at source coordinates before
   // cropping so its sampling and redaction match the preview at every offset.
   const flattened=document.createElement('canvas');flattened.width=doc.width;flattened.height=doc.height;
   const ctx=flattened.getContext('2d')!;ctx.drawImage(source.current!,0,0);paintSelection(ctx,selection);
   const crop=document.createElement('canvas');crop.width=selection.width;crop.height=selection.height;
   crop.getContext('2d')!.drawImage(flattened,selection.x,selection.y,selection.width,selection.height,0,0,selection.width,selection.height);
   await desktop('ExportWindowsCapture',{id,selection,png:crop.toDataURL('image/png').split(',')[1],note,context,action});
   if(action==='send')await refresh();
  }catch{
   setError(t(action==='save'?'native.capture.saveFailed':action==='send'?'native.capture.unknown':'native.capture.windowsActionUnconfirmed'));
   if(action==='send'){
    // A failed RPC is not evidence that dispatch failed. Keep this request
    // locked until its native receipt makes the outcome authoritative.
    setDoc(current=>current?{...current,outcome:'unknown'}:current);
    void refresh().catch(()=>{});
   }
  }finally{performing.current=false;setBusy(false);}
 };
 useEffect(()=>{
  if(doc?.outcome!=='pinned')return;
  const action=(event:Event)=>{
   const value=(event as CustomEvent<string>).detail;
   if(value==='copy'||value==='save')void perform(value);
   else if(value==='edit')void desktop('EditWindowsCapture',id).catch(()=>setError(t('native.capture.windowsActionUnconfirmed')));
   else if(value==='close')void desktop('CloseWindowsCapture',id);
  };
  window.addEventListener('capture-pin-action',action);return()=>window.removeEventListener('capture-pin-action',action);
 },[doc,selection,busy]);
 useEffect(()=>{
  const pin=()=>{if(doc?.outcome==='draft')void perform('pin');};
  window.addEventListener('capture-pin',pin);
  return()=>window.removeEventListener('capture-pin',pin);
 },[doc,selection,history,note,context,busy]);
 useEffect(()=>{
  const key=(e:KeyboardEvent)=>{
   const editing=e.target instanceof HTMLInputElement||e.target instanceof HTMLTextAreaElement||e.target instanceof HTMLSelectElement||(e.target instanceof HTMLElement&&e.target.isContentEditable);
   const commandKey=captureCommandKey(e,editing);
   const action=captureKeyAction(commandKey||e.key,e.ctrlKey);
   if(action&&action!=='all')report(`key_${action}`);
   else if(e.keyCode===229&&['KeyC','KeyS','Enter','F3'].includes(e.code))report('key_ime');
   if(commandKey===null)return;
   if(commandKey==='Escape'){e.preventDefault();void desktop('CloseWindowsCapture',id);return;}
   if(locked)return;
   if(editing){
    const input=e.target as HTMLInputElement;
    if(e.key==='Enter'&&input.dataset.captureNote==='true'){e.preventDefault();void perform('send');}
    else if(e.ctrlKey&&!e.altKey&&!e.shiftKey&&e.key.toLowerCase()==='s'){e.preventDefault();void perform('save');}
    else if(e.ctrlKey&&!e.altKey&&!e.shiftKey&&e.key.toLowerCase()==='c'&&input.selectionStart!==null&&input.selectionStart===input.selectionEnd){e.preventDefault();void perform('copy');}
    return;
   }
   if(e.target instanceof HTMLButtonElement&&(commandKey==='Enter'||commandKey===' '))return;
   if(e.ctrlKey&&commandKey.toLowerCase()==='z'){e.preventDefault();setHistory(e.shiftKey?redoCaptureMark:undoCaptureMark);}
   else if(e.ctrlKey&&commandKey.toLowerCase()==='y'){e.preventDefault();setHistory(redoCaptureMark);}
   else if(!e.altKey&&!e.shiftKey&&captureKeyAction(commandKey,e.ctrlKey)){
    e.preventDefault();const action=captureKeyAction(commandKey,e.ctrlKey)!;
    if(action==='all'&&doc)select({x:0,y:0,width:doc.width,height:doc.height});
    else if(action!=='all'&&(action!=='pin'||doc?.outcome!=='pinned'))void perform(action);
   }
   else if(e.key===' '&&doc?.outcome==='pinned'){e.preventDefault();void desktop('EditWindowsCapture',id);}
   else if(selection&&doc?.outcome==='draft'&&['ArrowLeft','ArrowRight','ArrowUp','ArrowDown'].includes(e.key)){
    e.preventDefault();const step=e.shiftKey?10:1;setSelection(moveCaptureSelection(selection,e.key==='ArrowLeft'?-step:e.key==='ArrowRight'?step:0,e.key==='ArrowUp'?-step:e.key==='ArrowDown'?step:0,doc.width,doc.height));
   }
  };window.addEventListener('keydown',key);return()=>window.removeEventListener('keydown',key);
 },[doc,selection,history,note,context,busy]);
 if(!doc)return error?<main className="capture-workspace"><p role="status">{error}</p></main>:null;
 const label=(key:Parameters<typeof t>[0])=>t(key).replace('⇧⌘','Ctrl+Shift+').replace('⌘','Ctrl+');
 const selectionHint=t(doc.contextAvailable?'native.capture.windowsSelect':'native.capture.windowsSelectImage');
 if(doc.outcome==='pinned')return <main ref={workspace} tabIndex={-1} className="capture-pin" style={{'--custom-contextmenu':id} as CSSProperties}
  onDoubleClick={()=>void desktop('CloseWindowsCapture',id)}
  onWheel={event=>{if(!busy&&event.deltaY)void desktop('ResizeWindowsPin',id,event.deltaY<0?1.08:1/1.08);}}>
  <img className="capture-pin-image" draggable={false} src={doc.image} alt={t('native.capture.pin')}/>
  {error&&<p className="capture-pin-error" role="alert">{error}</p>}
 </main>;
 return <main ref={workspace} tabIndex={-1} className="capture-workspace capture-editor" style={{visibility:imageLoaded?'visible':'hidden'}}>
 {showToolbar&&<div ref={toolbar} className="capture-palette" style={{left:toolbarPoint?.x??0,top:toolbarPoint?.y??0,visibility:toolbarPoint?'visible':'hidden'}}>
  <div className="capture-toolbar" role="toolbar" aria-label={t('native.capture.edit')}>
   {([['select',CursorIcon],['arrow',ArrowUpRightIcon],['rectangle',RectangleIcon],['text',TextTIcon],['mosaic',GridNineIcon],['redact',SquareIcon]] as const).map(([k,Icon])=><CaptureButton key={k} label={t(`native.capture.tool.${k}`)} disabled={locked} aria-pressed={kind===k} onClick={()=>{setKind(k);setWidthOpen(false);}}><Icon weight={k==='redact'?'fill':'regular'}/></CaptureButton>)}
   <span className="capture-separator"/>
   <label className="capture-color" title={t('native.capture.color')} style={{color:ink}}><PaletteIcon/><input type="color" aria-label={t('native.capture.color')} value={ink} disabled={locked} onChange={e=>setInk(e.target.value)}/></label>
   <CaptureButton label={t('native.capture.width')} disabled={locked} aria-expanded={widthOpen} onClick={()=>setWidthOpen(v=>!v)}><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M3 5h14" stroke="currentColor"/><path d="M3 10h14" stroke="currentColor" strokeWidth="2"/><path d="M3 16h14" stroke="currentColor" strokeWidth="3"/></svg></CaptureButton>
   <CaptureButton label={label('native.capture.undo')} disabled={!history.marks.length||locked} onClick={()=>setHistory(undoCaptureMark)}><ArrowUUpLeftIcon/></CaptureButton>
   <CaptureButton label={label('native.capture.redo')} disabled={!history.redo.length||locked} onClick={()=>setHistory(redoCaptureMark)}><ArrowUUpRightIcon/></CaptureButton>
   <span className="capture-separator"/>
   <CaptureButton label={label('native.capture.copy')} disabled={locked} onClick={()=>void perform('copy')}><CopyIcon/></CaptureButton>
   <CaptureButton label={label('native.capture.save')} disabled={locked} onClick={()=>void perform('save')}><DownloadSimpleIcon/></CaptureButton>
   <CaptureButton label={t('native.capture.windowsPin')} disabled={locked} onClick={()=>void perform('pin')}><PushPinIcon/></CaptureButton>
   <CaptureButton label={t('native.capture.windowsCancel')} onClick={()=>void desktop('CloseWindowsCapture',id)}><XIcon/></CaptureButton>
  </div>
  {widthOpen&&<div className="capture-options"><label>{t('native.capture.width')}<input type="range" min="1" max="16" aria-label={t('native.capture.width')} value={width} disabled={locked} onChange={e=>setWidth(Number(e.target.value))}/><output>{width}</output></label></div>}
  {kind==='text'&&<div className="capture-options"><input aria-label={t('native.capture.tool.text')} placeholder={t('native.capture.tool.text')} value={text} maxLength={200} disabled={locked} onChange={e=>setText(e.target.value)}/></div>}
  <div className="capture-composer"><input data-capture-note="true" aria-label={t('native.capture.note')} placeholder={t('native.capture.note')} value={note} disabled={locked} onChange={e=>setNote(e.target.value)}/>
   <button className="capture-ask" disabled={locked||doc.imageCapability!=='supported'} title={doc.imageCapability==='supported'?t('native.capture.windowsAsk'):t('native.capture.imageUnknown')} onClick={()=>void perform('send')}><SparkleIcon aria-hidden="true"/>{t('native.capture.askButton')}</button>
  </div>
  <div className="capture-scope"><label><input type="checkbox" checked={context} disabled={locked||!doc.contextAvailable} onChange={e=>setContext(e.target.checked)}/>{t('native.capture.includeBackground')}</label><span className="capture-dimensions">{selection.width} × {selection.height}</span>
   <CaptureButton label={t('native.capture.windowsWindow')} disabled={!doc.window.width||locked} onClick={()=>select(doc.window)}><BrowserIcon/></CaptureButton>
   <CaptureButton label={t(doc.contextAvailable?'native.capture.windowsDisplay':'native.capture.windowsImage')} disabled={locked} onClick={()=>select({x:0,y:0,width:doc.width,height:doc.height})}><MonitorIcon/></CaptureButton>
  </div>
  {(error||doc.message||doc.outcome==='unknown')&&<p className="capture-notice" role="status">{error||doc.message||t('native.capture.unknown')}</p>}
 </div>}
 <div className="capture-canvas-area"><canvas ref={canvas} aria-label={selectionHint}
  onDoubleClick={e=>{const p=point(e);if(kind==='select'&&selection&&p.x>=selection.x&&p.y>=selection.y&&p.x<selection.x+selection.width&&p.y<selection.y+selection.height)void perform('copy');}}
  onContextMenu={e=>{
   e.preventDefault();if(locked)return;drag.current=null;setSelecting(false);
   if(kind!=='select'){setKind('select');render();return;}
   if(confirmed){select({x:0,y:0,width:doc.width,height:doc.height});setConfirmed(false);}
   else void desktop('CloseWindowsCapture',id);
  }}
  onPointerDown={e=>{
   if(e.button===0)workspace.current?.focus({preventScroll:true});
   if(locked||e.button!==0)return;const p=point(e);if(kind!=='select'&&(!selection||p.x<selection.x||p.y<selection.y||p.x>selection.x+selection.width||p.y>selection.y+selection.height))return;
   e.currentTarget.setPointerCapture(e.pointerId);drag.current={kind,start:p,end:p,text,ink,width};
   if(kind==='select'){setSelecting(true);setWidthOpen(false);}
   if(kind==='text'){const mark=drag.current;setHistory(h=>addCaptureMark(h,mark));drag.current=null;}
  }} onPointerMove={e=>{
   const p=point(e);setPointer(p);if(!drag.current)return;drag.current.end=p;
   if(drag.current.kind==='select'){const r=dragRect(drag.current);if(r){setSelection(r);setHistory(emptyHistory());}}else render();
  }} onPointerLeave={()=>setPointer(null)} onPointerUp={e=>{
   if(!drag.current)return;const m=drag.current;m.end=point(e);drag.current=null;
   if(m.kind==='select'){
    const r=dragRect(m);if(r)select(r);
    setConfirmed(true);setSelecting(false);
   }else setHistory(h=>addCaptureMark(h,m));
  }} onPointerCancel={()=>{drag.current=null;setSelecting(false);render();}}/>{pointer&&kind==='select'&&(!selection||selecting)&&!locked&&<canvas className="capture-magnifier" ref={magnifier} aria-hidden="true"/>}</div>
  {!showToolbar&&<p className="capture-selection-hint" role="status">{error||selectionHint}</p>}
 </main>;
}
