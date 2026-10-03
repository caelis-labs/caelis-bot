import { MachineSettings } from '../MachineSettings';
import type { Machine, MachineInput } from '../../../backend/contract';
import type { backend } from '../../../desktop';
import { createPreviewClient } from './client';

// Visual fixtures only. Never makes an SSH call or replaces native evidence.
export function machinePreview(scenario:string) {
 let machine:Machine={id:'visual-fixture',name:'Fedora',address:'192.168.1.20',port:22,user:'developer',authentication:'agent',privateKey:'',remember:false,state:'setup',issue:'',fingerprint:'SHA256:visual-fixture',runtime:scenario.startsWith('caelis')?'caelis':'codex',available:['codex','caelis'],setup:{state:'auth',models:[],message:'',accountType:'',loginPending:false,serviceUpdateAvailable:false,serviceVersion:'',serviceState:'',selectedModel:'',settings:{runtime:'codex',cliPath:'',caelisStore:''},installation:{installed:true,path:'',version:'',message:'',latestVersion:'',updateState:''}},work:{model:'',effort:'',serviceTier:''},models:[],runtimeDefault:null,advancedIssue:''};
 if(scenario==='missing')machine.setup.state='missing';
 if(scenario==='offline'){machine.state='offline';machine.issue='ssh_authentication_or_connection';}
 if(scenario.startsWith('caelis')){machine.state='ready';machine.setup.state='ready';machine.setup.models=[{value:'example-model',label:'Example model',noAuth:false,current:false}];}
 const previewClient=createPreviewClient();
 if(machine.state==='ready'){const model={model:'example-model',name:'Example model',description:'',default:true,defaultEffort:'medium',efforts:['low','medium','high','xhigh'],serviceTiers:[{id:'fast',name:'Fast',description:''}],imageInput:false};machine.models=[model];machine.runtimeDefault={model:model.model,effort:'medium',serviceTier:''};}
 const call=(async (method:string,...args:unknown[])=>{
  if(method==='Machines')return [machine];
  if(method==='ReadMachineAdvanced'){
   if(scenario==='caelis-advanced-error')return {...machine,advancedIssue:'configuration_unavailable'};
   const view=await previewClient.read();machine={...machine,configuration:{revision:view.revision,team:{...view.team,roles:view.team.roles.map(role=>({...role,modelIds:role.modelIds??[],problem:role.problem??''})),sets:view.team.sets.map(set=>({...set,problem:set.problem??''}))},main:view.main!,models:view.team.models,connections:[],oauthAvailable:false}};return machine;
  }
  if(method==='ConnectMachine'){const draft=args[0] as MachineInput;machine={...machine,...draft,issue:scenario==='offline'?'ssh_authentication_or_connection':''};return machine;}
  if(method==='InspectMachine')return machine;
  if(method==='SaveMachineModel'){machine={...machine,work:(args[0] as {selection:Machine['work']}).selection};return machine;}
  if(method==='RemoveMachine')return undefined;
  throw new Error('Visual fixture only');
 }) as typeof backend;
 return <MachineSettings call={call} openTerminal={async()=>{throw new Error('Visual fixture only');}}/>;
}
