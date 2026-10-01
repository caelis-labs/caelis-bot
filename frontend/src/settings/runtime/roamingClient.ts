import {backend} from '../../desktop';
import type {NodeRoamingState,NodeRoamingRequest,NodeRoamingPlan} from '../../backend/contract';

type Invoke=<T>(method:string,...args:unknown[])=>Promise<T>;
const actions=new Set(['prepare-coordinator','prepare-node','connect-outgoing','stop-source','start-bot']);
const phases=new Set(['disabled','enabling','waiting','ready','unavailable','disabling','unknown']);
const valid=(value:NodeRoamingState)=>value&&typeof value.available==='boolean'&&typeof value.enabled==='boolean'&&phases.has(value.state)&&typeof value.operationId==='string'&&typeof value.coordinatorNodeId==='string'&&typeof value.activeBotNodeId==='string'&&['','accepted','rejected','unknown'].includes(value.outcome);

// One renderer owner retains the original action across view selection and
// connection failures. State lookup never sends the mutation again.
export function createNodeRoamingClient(invoke:Invoke=backend) {
 let value:NodeRoamingState|null=null,pending='',writing=false,reading=false,preparing=false,failed=false,generation=0;
 const listeners=new Set<()=>void>();
 const publish=()=>listeners.forEach(listener=>listener());
 const accept=(next:NodeRoamingState)=>{
  if(!valid(next))throw new Error('Unavailable node roaming state');
  if(pending&&next.operationId===pending&&(next.outcome==='accepted'||next.outcome==='rejected'))pending='';
  if(!pending&&next.operationId&&next.outcome==='unknown')pending=next.operationId;
  value=next;failed=false;
 };
 return {
  subscribe:(listener:()=>void)=>{listeners.add(listener);return()=>{listeners.delete(listener);};},
  snapshot:()=>({value,pending,busy:writing||reading||preparing,failed}),
  async read(){
   if(writing||reading||preparing)return;reading=true;const epoch=++generation;publish();
   try{const next=await invoke<NodeRoamingState>('NodeRoamingState');if(epoch===generation)accept(next);}
   catch{if(epoch===generation)failed=true;}
   finally{reading=false;publish();}
  },
  async prepare(expectedCatalogRevision:string){
   if(writing||reading||preparing||pending||failed||!value?.available)throw new Error('Roaming preparation unavailable');
   preparing=true;publish();
   const request:NodeRoamingRequest={id:crypto.randomUUID(),expectedCatalogRevision,reviewedPlanId:'',allowPersistentExecution:false};
   try{
    const plan=await invoke<NodeRoamingPlan>('PrepareNodeRoaming',request);
    if(!plan?.id||!plan.coordinatorNodeId||!Array.isArray(plan.actions)||typeof plan.requiresConfirmation!=='boolean'||!plan.actions.length||!plan.actions.every(action=>action&&typeof action.nodeId==='string'&&typeof action.label==='string'&&actions.has(action.action)))throw new Error('Roaming plan unavailable');
    return {request,plan};
   }finally{preparing=false;publish();}
  },
  async change(enable:boolean,expectedCatalogRevision:string,reviewed?:{request:NodeRoamingRequest;plan:NodeRoamingPlan}){
   if(writing||reading||preparing||pending||failed||!value?.available||['enabling','disabling','unknown'].includes(value.state)||enable&&!reviewed)return false;
   const request:NodeRoamingRequest=enable?{...reviewed!.request,reviewedPlanId:reviewed!.plan.id,allowPersistentExecution:true}:{id:crypto.randomUUID(),expectedCatalogRevision,reviewedPlanId:'',allowPersistentExecution:false};
   if(request.expectedCatalogRevision!==expectedCatalogRevision)return false;
   writing=true;pending=request.id;const id=pending;const epoch=++generation;publish();
   try{
    const next=await invoke<NodeRoamingState>(enable?'EnableNodeRoaming':'DisableNodeRoaming',request);
    if(epoch!==generation)return false;
    if(!valid(next)||next.operationId!==id)throw new Error('Original operation not confirmed');
    accept(next);return next.outcome==='accepted';
   }catch{if(epoch===generation)failed=true;return false;}
   finally{writing=false;publish();}
  },
 };
}
export type NodeRoamingClient=ReturnType<typeof createNodeRoamingClient>;
