import type {CaptureRect} from './capture-editor';
export type CaptureDocument={id:string;image:string;width:number;height:number;window:CaptureRect;screen:boolean;outcome:string;imageCapability:string;message:string;presentation:number;diagnostics?:boolean};

function wait<T>(work:Promise<T>,signal:AbortSignal):Promise<T>{
 return new Promise((resolve,reject)=>{
  const abort=()=>reject(signal.reason);
  work.then(value=>{if(!signal.aborted)resolve(value);},reject).finally(()=>signal.removeEventListener('abort',abort));
  if(signal.aborted){abort();return;}
  signal.addEventListener('abort',abort,{once:true});
 });
}
function pause(ms:number,signal:AbortSignal){
 return new Promise<void>((resolve,reject)=>{
  const abort=()=>{clearTimeout(timer);reject(signal.reason);};
  const timer=setTimeout(()=>{signal.removeEventListener('abort',abort);resolve();},ms);
  signal.addEventListener('abort',abort,{once:true});
  if(signal.aborted)abort();
 });
}
// Only capture preparation is safe here: reads, decoding and the native
// idempotent (id, presentation) Ready claim. Never pass export/send/save.
async function prepare<T>(operation:()=>Promise<T>,signal:AbortSignal):Promise<T>{
 for(let attempt=0;attempt<3;attempt++){
  signal.throwIfAborted();
  try{const value=await wait(operation(),signal);signal.throwIfAborted();return value;}
  catch{signal.throwIfAborted();if(attempt===2)throw new Error('capture_unavailable');}
  await pause(attempt===0?300:1200,signal);
 }
 throw new Error('capture_unavailable');
}
export function captureDocumentReader(id:string,read:()=>Promise<CaptureDocument>,accept:(doc:CaptureDocument)=>void,failed:(value:boolean)=>void){
 const controller=new AbortController(),signal=controller.signal;
 let work:Promise<void>|undefined,revision=0,presentation=0,exhausted=false;
 async function run(){
  try{
   let current:number;
   do{
    current=revision;
    const doc=await prepare(async()=>{const d=await read();if(d.id!==id)throw new Error('capture_identity');return d;},signal);
    signal.throwIfAborted();
    // A newer receipt or send reconciliation invalidates an in-flight read.
    if(current!==revision)continue;
    if(doc.presentation>=presentation){presentation=doc.presentation;accept(doc);failed(false);}
   }while(current!==revision);
  }catch{
   if(!signal.aborted){exhausted=true;failed(true);}
   throw new Error('capture_unavailable');
  }
 }
 return {
  refresh(){
   if(signal.aborted||exhausted)return Promise.reject(new Error('capture_unavailable'));
   revision++;
   if(!work)work=run().finally(()=>{work=undefined;});
   return work;
  },
  stop(){controller.abort();},
 };
}
export function captureImage(url:string,signal:AbortSignal):Promise<HTMLImageElement>{
 return prepare(()=>new Promise<HTMLImageElement>((resolve,reject)=>{
  const image=new Image();
  const finish=(ok:boolean)=>{clearTimeout(timer);signal.removeEventListener('abort',abort);image.onload=null;image.onerror=null;if(ok)resolve(image);else{image.src='';reject(new Error('capture_image'));}};
  const abort=()=>finish(false),timer=setTimeout(()=>finish(false),3000);
  image.onload=()=>finish(true);image.onerror=()=>finish(false);
  signal.addEventListener('abort',abort,{once:true});image.src=url;
 }),signal);
}
export function confirmCapture(ready:()=>Promise<unknown>,signal:AbortSignal){return prepare(ready,signal);}
