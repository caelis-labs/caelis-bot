import type {SetupState} from '../../backend/contract';
import type {MessageKey} from '../../i18n/catalogs';

export type BotSession = {connection:string;issue?:string}|null|undefined;

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
 if(!selected||session?.issue==='setup_required')return 'runtime.chooseConnection';
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
