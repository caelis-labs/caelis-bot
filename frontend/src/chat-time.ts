// Backend SeenAt values are Unix microseconds. Missing timestamps from older
// records stay undated instead of receiving a fabricated migration time.
const groupGapMicros=5*60*1_000_000;
const validTime=(value:number|undefined):value is number=>!!value&&Number.isFinite(value)&&Number.isFinite(new Date(value/1000).getTime());

export function chatTimeDivider(previous:number|undefined,current:number|undefined):boolean {
 if(!validTime(previous)||!validTime(current))return false;
 const before=new Date(previous/1000),after=new Date(current/1000);
 return before.getFullYear()!==after.getFullYear()||before.getMonth()!==after.getMonth()||before.getDate()!==after.getDate()||Math.abs(current-previous)>=groupGapMicros;
}

type TimedMessage={id:string;requestId?:string;seenAt?:number};
export type ChatTimeAnchor={previousKey:string|undefined;seenAt:number|undefined;dividerAt:number|undefined};

// Decide a row's layout before rendering it. A native acknowledgement may replace
// an optimistic ID or fill in SeenAt later; neither should insert a new divider
// above an already visible message. This is presentation state, never a receipt.
export function chatTimeline<T extends TimedMessage>(items:readonly T[],previous:ReadonlyMap<string,ChatTimeAnchor>=new Map()) {
 const anchors=new Map<string,ChatTimeAnchor>();
 let before:ChatTimeAnchor|undefined,previousKey:string|undefined;
 const rows=items.map(item=>{
  const key=item.requestId?`request:${item.requestId}`:`item:${item.id}`;
  const prior=previous.get(key);
  const seenAt=prior?.seenAt??(validTime(item.seenAt)?item.seenAt:undefined);
  // Loading an earlier page changes adjacency and may introduce a boundary.
  // Timestamp-only updates retain the decision made in the first visible frame.
  const dividerAt=prior&&prior.previousKey===previousKey?prior.dividerAt:
   chatTimeDivider(before?.seenAt,seenAt)?seenAt:undefined;
  const anchor={previousKey,seenAt,dividerAt};
  anchors.set(key,anchor);before=anchor;previousKey=key;
  return {key,item,dividerAt};
 });
 // Keep only visible rows, so pagination/reloads cannot grow this cache forever.
 return {rows,anchors};
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
