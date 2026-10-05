export type TelegramStatus={enabled:boolean;bot:string;owner:string;paired:boolean;candidate:string;pairURL:string;issue:string};
export type TelegramPhase='loading'|'connecting'|'reconnecting'|'unconfigured'|'pairing'|'candidate'|'connected'|'paused'|'error';
const blocking=new Set(['invalid_token','webhook','occupied','keychain','storage','blocked','unavailable','pairing_failed','pairing_expired','telegram_error']);
export function telegramPhase(status:TelegramStatus|null,busy:boolean,issue:string):TelegramPhase {
 if(busy)return 'connecting';
 if(!status)return issue?'error':'loading';
 if(issue==='network')return status.enabled?'reconnecting':'error';
 if(issue&&blocking.has(issue))return 'error';
 if(!status.bot)return 'unconfigured';
 if(!status.enabled)return 'paused';
 if(status.paired)return 'connected';
 if(status.candidate)return 'candidate';
 if(status.pairURL)return 'pairing';
 return 'error';
}
