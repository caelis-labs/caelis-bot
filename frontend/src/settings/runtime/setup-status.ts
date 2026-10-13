import type {SetupState} from '../../backend/contract';
import type {MessageKey} from '../../i18n/catalogs';

export type BotSession = {connection:string;issue?:string}|null|undefined;

// Installation and a running Host are prerequisites, not onboarding success.
// The native setup state is the only authority for advancing to chat.
export function nextOnboardingStep(state:SetupState, runtime:string): 'install'|'start'|'model'|'' {
 if(!state.installation.installed)return 'install';
 if(runtime==='caelis' && state.state==='service')return 'start';
 if(runtime==='caelis' && state.state==='models')return 'model';
 return '';
}

export function setupStatusKey(state:SetupState):MessageKey {
 if(!state.installation.installed)return 'runtime.notInstalled';
 if(state.loginPending)return 'runtime.waitingBrowserLogin';
 switch(state.state){
  case 'ready':return 'runtime.setupReady';
  case 'login':return 'runtime.loginCodex';
  case 'models':return 'runtime.noModelsConnected';
  case 'service':return 'runtime.notStarted';
  case 'incompatible':return 'runtime.incompatibleConnection';
  default:return 'runtime.unavailableConnection';
 }
}

export function botSessionStatusKey(state:SetupState,session:BotSession,selected:boolean):MessageKey {
 if(!selected)return 'runtime.chooseConnection';
 if(state.state!=='ready')return setupStatusKey(state);
 if(!session)return 'runtime.statusUnknown';
 switch(session.connection){
  case 'ready':return 'runtime.connected';
  case 'connecting':return 'runtime.connecting';
  case 'login':return 'runtime.loginCodex';
  case 'offline':return 'runtime.sessionUnavailable';
  default:return 'runtime.statusUnknown';
 }
}
