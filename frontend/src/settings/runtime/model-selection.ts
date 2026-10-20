import type {ModelSelectionState,WorkExecutionSettings} from '../../backend/contract';

type Invoke = <T>(method:string,...args:unknown[])=>Promise<T>;
// Keep a not-yet-acknowledged choice across settings panel mounts. No credentials
// or native operations are retried here: SaveSelectedModel only journals intent.
export class ModelSelectionDelivery {
 private pending = new Map<string,{runtime:string;selection:WorkExecutionSettings}>();
 private running = false;
 private epoch = 0;
 version() { return this.epoch; }
 constructor(private invoke:Invoke,private pause:(ms:number)=>Promise<void> = ms=>new Promise(resolve=>setTimeout(resolve,ms))) {}
 save(scope:string,runtime:string,selection:WorkExecutionSettings) {
  this.epoch++;
  this.pending.set(scope,{runtime,selection});
  if(!this.running)void this.drain();
 }
 snapshot():ModelSelectionState[] {
  return [...this.pending].map(([scope,v])=>({scope,...v,revision:0,state:'saving'}));
 }
 private async drain() {
  this.running=true;
  let delay=500;
  try {
   while(this.pending.size) {
    let failed=false;
    for(const [scope,value] of [...this.pending]) {
     try {
      await this.invoke('SaveSelectedModel',scope,value.runtime,value.selection);
      if(this.pending.get(scope)===value){this.pending.delete(scope);this.epoch++;}
      delay=500;
     } catch { failed=true; }
    }
    if(failed){await this.pause(delay);delay=Math.min(delay*2,10000);}
   }
  } finally { this.running=false; }
 }
}
