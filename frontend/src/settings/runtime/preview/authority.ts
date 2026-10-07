import type {RuntimeSettings, SetupState} from '../../../backend/contract';
import type {backend} from '../../../desktop';

// Development transport with the production backend's default-provider shape.
export function createAuthorityPreview(scenario:string):typeof backend {
 const selected=scenario.includes('-selected-')&&!scenario.includes('-unselected-');
 const connection=scenario.match(/(?:offline|connecting|unknown|ready)$/)?.[0]??'offline';
 const snapshotFails=scenario.endsWith('-error');
 (window as Window & {__authorityFacts?:object}).__authorityFacts={active:'codex',hasRuntimeChoice:selected,snapshotFails};
 const profile:RuntimeSettings={runtime:'codex',cliPath:'',caelisStore:''};
 const setup:SetupState={serviceUpdateAvailable:false,serviceVersion:'',serviceState:'unknown',settings:profile,selectedModel:'fixture/model',installation:{installed:true,path:'/fixture/codex',version:'fixture',message:'',latestVersion:'',updateState:''},state:'ready',message:'',models:[{value:'fixture/model',label:'Fixture model',noAuth:false,current:true}],loginPending:false,accountType:'chatgpt'};
 return async<T>(method:string):Promise<T>=>{
  let result:unknown;
  switch(method){
   case 'RuntimeSettings':case 'SetupProfile':result=profile;break;
   case 'SetupOverview':result={active:'codex',hasRuntimeChoice:selected,pending:'',onboarding:false};break;
   case 'InspectSetup':result=setup;break;
   case 'ComposerSnapshot':
    if(snapshotFails)throw Error('Synthetic ComposerSnapshot read failure');
    result={connection,connectionIssue:!selected&&connection!=='ready'?'setup_required':''};break;
   default:throw Error(`Synthetic ${method} read failure`);
  }
  return result as T;
 };
}
