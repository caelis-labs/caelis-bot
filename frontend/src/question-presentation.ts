export function questionStatusKey(state:string) {
 switch(state){
  case 'answered':case 'accepted':return 'chat.questionAnswered' as const;
  case 'dismissed':return 'chat.questionDismissed' as const;
  case 'expired':return 'chat.questionExpired' as const;
  case 'superseded':return 'chat.questionSuperseded' as const;
  default:return 'chat.questionUnknown' as const;
 }
}

// Typography only: partition at the original question mark, preserving every
// character. Never generate or accept a semantic alias for a native answer.
export function questionCopy(title:string,context='') {
 if(context)return {title,context};
 const end=title.search(/[？?]/);
 return end>=0&&end<title.length-1 ? {title:title.slice(0,end+1),context:title.slice(end+1)} : {title,context};
}
export function displayUnits(text:string) {return Array.from(text).reduce((n,c)=>n+(/[^\x00-\xff]/.test(c)?2:1),0);}
export function optionWords(text:string,locale:string) {
 return Array.from(new Intl.Segmenter(locale,{granularity:'word'}).segment(text),part=>part.segment);
}
