import type {ConnectInput,ConnectionFlow,RuntimeSettingsClient} from './types';
const terminal=(flow:ConnectionFlow)=>['complete','failed','unknown'].includes(flow.stage);
// Host connections have individual receipts. Advance only after confirmed completion;
// never retry an ambiguous write or store credentials for a later retry.
export async function connectModels(client:RuntimeSettingsClient,input:ConnectInput,models:string[],signal:AbortSignal,progress:(flow:ConnectionFlow)=>void,completed:(model:string)=>void) {
 let last:ConnectionFlow|undefined;
 for(const model of models){
  signal.throwIfAborted();
  last=await new Promise<ConnectionFlow>((resolve,reject)=>{
   let settled=false;
   const stop=()=>{if(settled)return;settled=true;reject(signal.reason);};
   const observe=(flow:ConnectionFlow)=>{if(settled)return;progress(flow);if(terminal(flow)){settled=true;signal.removeEventListener('abort',stop);resolve(flow);}};
   signal.addEventListener('abort',stop,{once:true});
   void client.startConnection({...input,model},signal,observe).then(observe,e=>{if(!settled){settled=true;signal.removeEventListener('abort',stop);reject(e);}});
  });
  if(last.stage!=='complete')return last;
  completed(model);
 }
 return last!;
}
