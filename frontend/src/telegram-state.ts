export type TelegramStatus={enabled:boolean;bot:string;owner:string;paired:boolean;candidate:string;pairURL:string;issue:string};
// Pairing must retain the complete short-lived link. A plain bot URL is only
// useful after account confirmation; it cannot substitute for a missing nonce.
export function telegramURL(status:TelegramStatus|null,pair:boolean):string {
 if(!pair)return 'https://t.me/BotFather';
 if(!status||!/^[A-Za-z0-9_]+$/.test(status.bot))return '';
 if(!status.pairURL)return status.paired?`https://t.me/${status.bot}`:'';
 try {
  const url=new URL(status.pairURL);
  return url.protocol==='https:'&&url.hostname==='t.me'&&!url.port&&!url.username&&!url.password&&url.pathname===`/${status.bot}`&&!!url.searchParams.get('start')?url.href:'';
 }catch{return '';}
}
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
