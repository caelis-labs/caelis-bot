import {settingsDestination} from './settings-navigation.ts';

export type WebNavigation={view:'chat'|'settings'|'task';section?:string;task?:string};
export type SetupGate={onboarding:boolean;required:boolean;status:string};
export const setupRoute:WebNavigation={view:'settings',section:'setup'};
export function needsSetup(gate:SetupGate){return gate.onboarding||gate.required||['unknown','rejected'].includes(gate.status);}
export function parseWebRoute(hash:string):WebNavigation{
 const parts=hash.replace(/^#\/?/,'').split('/');
 if(parts[0]==='setup')return setupRoute;
 if(parts[0]==='settings'){
  const section=settingsDestination(parts[1]||'general');
  return {view:'settings',section:section&&!['permissions','extras'].includes(section)?section:'general'};
 }
 if(parts[0]==='task'&&/^[a-zA-Z0-9_-]{1,160}$/.test(parts[1]||''))return {view:'task',task:parts[1]};
 return {view:'chat'};
}
export function webRouteHash(route:WebNavigation){
 if(route.section==='setup')return '#/setup';
 if(route.view==='settings')return '#/settings/'+route.section;
 if(route.view==='task')return '#/task/'+route.task;
 return '#/chat';
}

// The server's durable setup state is the gate. URL/history/storage never grant
// access to another surface. A late read cannot overtake a newer navigation.
export function createNavigationGuard(read:()=>Promise<SetupGate>,commit:(route:WebNavigation,replace:boolean)=>void){
 let serial=0;
 return async(route:WebNavigation,replace=false)=>{
  const current=++serial;
  const required=needsSetup(await read());
  if(current!==serial)return;
  const next=required?setupRoute:route.section==='setup'?{view:'chat'} as WebNavigation:route;
  commit(next,replace||required);
 };
}
