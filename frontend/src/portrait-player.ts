// A bounded shared decoded cache. Leases survive LRU eviction without allowing
// a late image load to restore an evicted entry. No historical row loads atlases.
export class PortraitCache<T> {
 private entries=new Map<string,Promise<T>>();
 private load:(url:string)=>Promise<T>;
 private limit:number;
 constructor(load:(url:string)=>Promise<T>,limit=3){this.load=load;this.limit=limit;}
 get(url:string):Promise<T>{
  const known=this.entries.get(url);
  if(known){this.entries.delete(url);this.entries.set(url,known);return known;}
  const pending=this.load(url).catch(error=>{if(this.entries.get(url)===pending)this.entries.delete(url);throw error;});
  this.entries.set(url,pending);
  while(this.entries.size>this.limit)this.entries.delete(this.entries.keys().next().value!);
  return pending;
 }
 get size(){return this.entries.size;}
}

export type PortraitSheet={frameSize:number;columns:number;frameCount:number;fps:number};
export function portraitFrame(sheet:PortraitSheet,elapsed:number){
 const frame=Math.floor(Math.max(0,elapsed)*sheet.fps)%sheet.frameCount;
 return {frame,x:frame%sheet.columns*sheet.frameSize,y:Math.floor(frame/sheet.columns)*sheet.frameSize};
}

type Scheduler={request:(callback:(time:number)=>void)=>number;cancel:(id:number)=>void};
// Separate visible time from wall time: returning to a window never fast-forwards
// through missed gestures. Own exactly one RAF and cancel it on every exit.
export function portraitClock(draw:(elapsed:number)=>void,scheduler:Scheduler){
 let request=0,running=false,disposed=false,elapsed=0,last:number|undefined;
 const tick=(now:number)=>{
  if(!running||disposed)return;
  if(last!==undefined)elapsed+=Math.max(0,Math.min((now-last)/1000,.1));
  last=now;draw(elapsed);request=scheduler.request(tick);
 };
 return {
  setRunning(value:boolean){
   if(disposed||value===running)return;
   running=value;last=undefined;
   if(value)request=scheduler.request(tick);else scheduler.cancel(request);
  },
  dispose(){disposed=true;running=false;scheduler.cancel(request);},
 };
}
