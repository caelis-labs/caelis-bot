import type {SetupState} from '../../backend/contract';
import type {MessageKey} from '../../i18n/catalogs';

export function setupStatusKey(state:SetupState):MessageKey {
 if(!state.installation.installed)return 'runtime.notInstalled';
 if(state.loginPending)return 'runtime.waitingBrowserLogin';
 switch(state.state){
  case 'ready':return 'runtime.connected';
  case 'login':return 'runtime.loginCodex';
  case 'models':return 'runtime.noModelsConnected';
  case 'service':return 'runtime.notStarted';
  case 'incompatible':return 'runtime.incompatibleConnection';
  default:return 'runtime.unavailableConnection';
 }
}
