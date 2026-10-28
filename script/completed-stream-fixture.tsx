// Mounted production surfaces, with controlled frame suspension and synthetic DTOs.
import {useState} from 'react';
import {ContextWindow} from '../frontend/src/ContextWindow';
import {Bubble} from '../frontend/src/Bubble';
import {MessageContent} from '../frontend/src/MessageContent';
import {WebTasks} from '../frontend/src/WebTasks';
import type {WebClient} from '../frontend/src/web-transport';
const w=window as any;
w.streamFixtureModule=import.meta.url;
const delay=(ms=20)=>new Promise(r=>setTimeout(r,ms));
const wait=async(test:()=>boolean)=>{const end=Date.now()+6000;while(!test()){if(Date.now()>end)throw Error('stream fixture timeout');await delay();}};
const check=(value:unknown,label:string)=>{if(!value)throw Error(label)};
const text='完整正文 👩🏽‍💻 e\u0301 🇨🇳 '+ '顺序不变，没有丢字或重复。'.repeat(65)+' END';
let serial=0,mountSerial=0,held=false,nextFrame=0;
const frames=new Map<number,{callback:FrameRequestCallback;native?:number}>();
const nativeFrame=requestAnimationFrame.bind(window),nativeCancel=cancelAnimationFrame.bind(window);
function schedule(id:number){const frame=frames.get(id);if(frame&&!held)frame.native=nativeFrame(now=>{frames.delete(id);frame.callback(now)});}
if(new URLSearchParams(location.search).get('view')==='streaming')window.requestAnimationFrame=callback=>{const id=++nextFrame;frames.set(id,{callback});schedule(id);return id;};
if(new URLSearchParams(location.search).get('view')==='streaming')window.cancelAnimationFrame=id=>{const frame=frames.get(id);if(frame?.native!==undefined)nativeCancel(frame.native);frames.delete(id);};
const hold=(value:boolean)=>{held=value;for(const [id,frame] of frames){if(frame.native!==undefined){nativeCancel(frame.native);delete frame.native;}if(!held)schedule(id);}};
const step=(now=performance.now())=>{for(const [id,frame] of [...frames]){if(frame.native!==undefined)nativeCancel(frame.native);frames.delete(id);frame.callback(now);}};
const workerClient={taskConversation:async()=>{w.workerReads++;return structuredClone(w.workerValue)},
 taskObservation:async()=>({task:{id:'worker',status:w.workerValue.status},items:[],revision:'fixture'})} as unknown as WebClient;
export function StreamingFixture(){
 const [surface,setSurface]=useState('chat'),[generation,setGeneration]=useState(0),[probe,setProbe]=useState({text:'',animate:false});
 w.streamMount=(value:string)=>{const generation=++mountSerial;setSurface(value);setGeneration(generation);return generation;};
 w.streamProbe=(value:typeof probe)=>{setProbe(value);};
 return <><div key={generation} data-stream-surface={surface} data-stream-generation={generation}>{surface==='chat'?<ContextWindow/>:surface==='bubble'?<Bubble/>:surface==='worker'?<WebTasks client={workerClient} tasks={[{id:'worker',title:'Synthetic worker',status:w.workerValue.status}]} listError={false} onRetryList={()=>{}} onClose={()=>{}} onSelect={()=>{}}/>:<MessageContent text={probe.text} animate={probe.animate} report={()=>{}}/>}</div><button style={{position:'fixed',top:3,left:3,zIndex:200,fontSize:10}} onClick={()=>void w.runStreamingRegression()}>Run streaming regression</button></>;
}
const selector=(surface:string,id:string)=>surface==='bubble'?'.bubble-copy .markdown-body':surface==='worker'?'.worker-message.assistant .markdown-body':surface==='probe'? '[data-stream-surface=probe] .markdown-body':`[data-message-id="${id}"] .markdown-body`;
const visible=(surface:string,id:string)=>document.querySelector(selector(surface,id))?.textContent??'';
const publish=async(surface:string,id:string,value:string,status:string)=>{
 const revision=w.fixtureStreamSnapshot({id,text:value,status});
 await wait(()=>w.fixtureStreamObserved?.revision===revision);
 await wait(()=>!!document.querySelector(selector(surface,id)));
 await delay(40); // React commit; deliberately no animation frame is released.
 return {source:w.fixtureStreamObserved,shown:visible(surface,id),expected:value};
};
async function mount(surface:string){
 hold(false);w.fixtureStreamSnapshot({id:'history',text:'history',status:'completed'});const generation=w.streamMount(surface);
 await wait(()=>document.querySelector('[data-stream-surface]')?.getAttribute('data-stream-generation')===String(generation));
 if(surface==='chat'||surface==='bubble')await wait(()=>visible(surface,'history')==='history');
}
function lifecycle(hidden:boolean){Object.defineProperty(document,'hidden',{configurable:true,value:hidden});document.dispatchEvent(new Event('visibilitychange'));}
w.streamHold=hold;w.streamStep=step;
w.streamLifecycle=[];
for(const event of ['focus','blur','pageshow','visibilitychange'])window.addEventListener(event,e=>w.streamLifecycle.push({event,hidden:document.hidden,trusted:e.isTrusted}));
w.runStreamingRegression=async()=>{
 await wait(()=>!!w.fixtureStreamSnapshot&&!!w.streamMount);
 const results:any[]=[];
 const test=async(name:string,run:()=>Promise<unknown>)=>{try{results.push({name,ok:true,evidence:await run()})}catch(error){results.push({name,ok:false,error:String(error)});}finally{delete (document as any).hidden;hold(false);}};
 for(const surface of ['chat','bubble']){
  await test(surface+' completed arrival without frames',async()=>{await mount(surface);hold(true);const sample=await publish(surface,'final-'+(++serial),text,'completed');check(sample.shown===text,JSON.stringify({status:sample.source.status,shown:sample.shown.length,expected:sample.expected.length}));return {sourceStatus:sample.source.status,length:sample.shown.length};});
  await test(surface+' same-text completion drains queued tail',async()=>{await mount(surface);const id='stream-'+(++serial);hold(true);const started=await publish(surface,id,text,'inProgress');check(started.shown.length>0&&started.shown.length<text.length,'live stream was not paced');const sample=await publish(surface,id,text,'completed');check(sample.shown===text,JSON.stringify({status:sample.source.status,shown:sample.shown.length,expected:sample.expected.length}));return {before:started.shown.length,after:sample.shown.length};});
  await test(surface+' final append while hidden then visible',async()=>{await mount(surface);const id='hidden-'+(++serial);hold(true);await publish(surface,id,text.slice(0,120),'inProgress');lifecycle(true);const sample=await publish(surface,id,text,'completed');check(sample.shown===text,JSON.stringify({status:sample.source.status,shown:sample.shown.length,expected:sample.expected.length}));lifecycle(false);await delay(40);check(visible(surface,id)===text,'visible recovery replayed');step(performance.now()+5000);await delay();check(visible(surface,id)===text,'late frame regressed completion');return {hiddenComplete:true,lateFrame:true};});
  await test(surface+' completed row remount and history reopen',async()=>{await mount(surface);hold(true);const id='restore-'+(++serial);await publish(surface,id,text,'completed');const generation=w.streamMount(surface);await wait(()=>document.querySelector('[data-stream-surface]')?.getAttribute('data-stream-generation')===String(generation));await wait(()=>!!document.querySelector(selector(surface,id)));await delay(40);check(visible(surface,id)===text,'completed first mount replayed');if(surface==='chat'){window.dispatchEvent(new Event('history-close'));await delay();window.dispatchEvent(new Event('history-open'));await delay(550);check(visible(surface,id)===text,'history reopen replayed');}return {remounted:true,full:true};});
  await test(surface+' ongoing deltas retain order until completion',async()=>{await mount(surface);hold(true);const id='ongoing-'+(++serial);const first=await publish(surface,id,text.slice(0,80),'inProgress');step(performance.now()+16);await delay();const prior=visible(surface,id);const next=await publish(surface,id,text.slice(0,240),'inProgress');check(next.shown.startsWith(prior)&&text.startsWith(next.shown)&&next.shown.length<240,'ongoing delta lost prefix/order');const done=await publish(surface,id,text,'completed');check(done.shown===text,JSON.stringify({status:done.source.status,shown:done.shown.length,expected:done.expected.length}));return {first:first.shown.length,delta:next.shown.length,final:done.shown.length};});
 }
 await test('hook focus resumes current prefix and rejects cancelled frame',async()=>{
  await mount('probe');hold(true);w.streamProbe({text,animate:true});await delay(40);const pending=[...frames.values()].map(frame=>frame.callback);check(visible('probe','').length<text.length,'probe not streaming');
  window.dispatchEvent(new Event('focus'));await delay(40);check(visible('probe','')===text,'focus retained frame backlog');
  const final=text+' final';w.streamProbe({text:final,animate:false});await delay(40);for(const callback of pending)callback(performance.now()+5000);await delay();check(visible('probe','')===final,'cancelled frame rewound terminal text');check(frames.size===0,'terminal reveal kept scheduling frames');return {cancelledFrames:pending.length,full:true};
 });
 await test('hook completion before delayed visibility effects cannot rewind',async()=>{await mount('probe');hold(true);w.streamProbe({text,animate:true});await delay(40);lifecycle(true);lifecycle(false);w.streamProbe({text:text+' race',animate:false});await delay(40);step();await delay();check(visible('probe','')===text+' race','visibility/terminal race lost text');return {full:true};});
 await test('worker completed history mounts immediately without reveal frames',async()=>{hold(true);w.workerReads=0;w.workerValue={status:'completed',messages:[{id:'worker-message',role:'assistant',text}]};w.streamMount('worker');await wait(()=>visible('worker','')===text);check(w.workerReads===1,'completed worker history unexpectedly repolled');return {full:true,reads:w.workerReads};});
 await test('worker ongoing history preserves native text updates',async()=>{hold(true);w.workerReads=0;w.workerValue={status:'running',messages:[{id:'worker-message',role:'assistant',text:text.slice(0,80)}]};w.streamMount('worker');await wait(()=>visible('worker','')===text.slice(0,80));w.workerValue={status:'completed',messages:[{id:'worker-message',role:'assistant',text}]};await wait(()=>visible('worker','')===text);return {full:true,reads:w.workerReads};});
 hold(false);return w.streamingResult={ok:results.every(r=>r.ok),results,lifecycle:w.streamLifecycle};
};

w.prepareNativeStreaming=async()=>{await wait(()=>!!w.streamMount&&!!w.fixtureStreamSnapshot);await mount('chat');hold(true);w.nativeStreamID='native-window-'+(++serial);const sample=await publish('chat',w.nativeStreamID,text.slice(0,100),'inProgress');return {length:sample.shown.length,sourceStatus:sample.source.status,hidden:document.hidden};};
w.completeNativeStreaming=async()=>{const sample=await publish('chat',w.nativeStreamID,text,'completed');check(sample.shown===text,'native hidden completion remained partial');return {length:sample.shown.length,expected:text.length,sourceStatus:sample.source.status,hidden:document.hidden};};
w.readNativeStreaming=()=>{const shown=visible('chat',w.nativeStreamID);hold(false);return {ok:shown===text&&!document.hidden,length:shown.length,expected:text.length,hidden:document.hidden,lifecycle:w.streamLifecycle};};
