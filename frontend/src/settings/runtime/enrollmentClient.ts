import type {NodeAddRequest, NodeAddResult} from '../../backend/contract';

type Invoke = <T>(method:string,...args:unknown[])=>Promise<T>;
// The pending ID survives form cancellation and selection changes. Native
// catalogs rehydrate it after remount/restart; reads never call Add again.
export function createNodeEnrollmentClient(invoke:Invoke) {
 let pending='',busy=false,result:NodeAddResult|null=null;
 const terminal=new Set<string>(),listeners=new Set<()=>void>();
 const publish=()=>listeners.forEach(listener=>listener());
 const unknown=(id:string):NodeAddResult=>({operationId:id,outcome:'unknown',reason:'unknown',node:{id:'',label:'',os:'unknown',join:'ssh',runtimes:[]},joinInstructions:null});
 const accept=(next:NodeAddResult,id:string):NodeAddResult=>{
  // Another surface may have admitted the original intent first. The native
  // owner returns its journaled ID without dispatching this new request.
  if(next.outcome==='unknown'&&next.reason==='original-pending'&&/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(next.operationId??'')){id=next.operationId!;pending=id;}
  if(next.operationId!==id||!['committed','failed','unknown'].includes(next.outcome??'')||next.outcome==='committed'&&(!next.node?.id||!['ssh','outgoing'].includes(next.node.join)))next=unknown(id);
  result=next;
  if(next.outcome!=='unknown'){terminal.add(id);if(pending===id)pending='';}
  return next;
 };
 return {
  snapshot:()=>({pending,busy,result}),
  subscribe:(listener:()=>void)=>{listeners.add(listener);return()=>{listeners.delete(listener);};},
  restore(ids:string[]){
   if(!pending){const id=ids.find(id=>!terminal.has(id));if(id){pending=id;result=unknown(id);publish();}}
  },
  async add(request:Omit<NodeAddRequest,'operationId'>){
   if(busy||pending)return result??unknown(pending);
   const id=crypto.randomUUID();pending=id;busy=true;result=unknown(id);publish();
   try{return accept(await invoke<NodeAddResult>('AddNode',{...request,operationId:id}),id);}
   catch{return accept(unknown(id),id);}
   finally{busy=false;publish();}
  },
  async reconcile(){
   if(busy||!pending)return result;
   const id=pending;busy=true;publish();
   try{return accept(await invoke<NodeAddResult>('ReconcileNodeEnrollment',id),id);}
   catch{return accept(unknown(id),id);}
   finally{busy=false;publish();}
  },
 };
}
