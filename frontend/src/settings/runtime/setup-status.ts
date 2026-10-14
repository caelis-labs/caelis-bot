import type {SetupState} from '../../backend/contract';
import type {MessageKey} from '../../i18n/catalogs';

export type BotSession = {connection:string;issue?:string}|null|undefined;

export function setupFailureKind(message:string,cancelAccepted:boolean):'cancelled'|'unknown'|'failed' {
 if(cancelAccepted)return 'cancelled';
 return message.includes('The operation has not been confirmed yet.')?'unknown':'failed';
}

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
  case 'models':return state.models.some(model=>!model.noAuth)?state.models.filter(model=>!model.noAuth).every(model=>(model as typeof model & {chatEligibility?:string}).chatEligibility==='non-chat')?'runtime.noChatModel':'runtime.chatUnverified':'runtime.noModelsConnected';
  case 'service':return 'runtime.notStarted';
  case 'incompatible':return 'runtime.incompatibleConnection';
  default:return 'runtime.unavailableConnection';
 }
}

// An incompatible running Host is not evidence that the installed binary is
// newer. Offer activation only when the native setup state observed an actual
// newer installed service, or when a stopped service needs starting.
export function canApplyInstalledService(state:SetupState|null|undefined):boolean {
 return !!state?.installation.installed && (state.state==='service'||!!state.serviceUpdateAvailable);
}

export function botSessionStatusKey(state:SetupState,session:BotSession,selected:boolean):MessageKey {
 if(!selected)return 'runtime.chooseConnection';
 if(session?.connection==='ready')return 'runtime.connected';
 if(session?.connection==='connecting')return 'runtime.connecting';
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
