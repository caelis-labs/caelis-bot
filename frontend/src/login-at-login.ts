export type LoginAtLoginStatus = {
 supported:boolean;
 state:'on'|'off'|'registered'|'needsApproval'|'unavailable'|'unsupported';
 enabled:boolean;
 registered:boolean;
};

export function loginAtLoginMessage(status:LoginAtLoginStatus) {
 switch(status.state){
  case 'on':return 'settings.loginAtLoginOn';
  case 'off':return 'settings.loginAtLoginOff';
  case 'registered':return 'settings.loginAtLoginRegistered';
  case 'needsApproval':return 'settings.loginAtLoginNeedsApproval';
  default:return status.supported?'settings.loginAtLoginUnavailable':'settings.loginAtLoginUnsupported';
 }
}
