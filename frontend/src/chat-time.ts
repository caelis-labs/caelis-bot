// Backend SeenAt values are Unix microseconds. Missing timestamps from older
// records stay undated instead of receiving a fabricated migration time.
const groupGapMicros=5*60*1_000_000;

export function chatTimeDivider(previous:number|undefined,current:number|undefined):boolean {
 if(!current||!Number.isFinite(current)||!Number.isFinite(new Date(current/1000).getTime()))return false;
 if(!previous||!Number.isFinite(previous)||!Number.isFinite(new Date(previous/1000).getTime()))return true;
 const before=new Date(previous/1000),after=new Date(current/1000);
 return before.getFullYear()!==after.getFullYear()||before.getMonth()!==after.getMonth()||before.getDate()!==after.getDate()||Math.abs(current-previous)>=groupGapMicros;
}

export function chatTimeLabel(seenAt:number,locale:string,now=new Date()):string {
 const date=new Date(seenAt/1000);
 const language=locale==='zh-CN'?'zh-CN':'en';
 const time=new Intl.DateTimeFormat(language,{hour:'2-digit',minute:'2-digit',hour12:false}).format(date);
 const day=new Date(date.getFullYear(),date.getMonth(),date.getDate());
 const today=new Date(now.getFullYear(),now.getMonth(),now.getDate());
 const dayDifference=Math.round((today.getTime()-day.getTime())/86_400_000);
 if(dayDifference===0)return time;
 if(dayDifference===1)return `${new Intl.RelativeTimeFormat(language,{numeric:'auto'}).format(-1,'day')} ${time}`;
 const dateLabel=new Intl.DateTimeFormat(language,{year:date.getFullYear()===now.getFullYear()?undefined:'numeric',month:'numeric',day:'numeric'}).format(date);
 return `${dateLabel} ${time}`;
}
