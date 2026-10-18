import type {Item,QuestionCard} from './backend/contract';

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

export function questionAnswered(state?:string) {return state==='answered'||state==='accepted';}
export function questionIdentity(card:QuestionCard) {return JSON.stringify([card.shortId,card.binding]);}

// Display projection only. Keep the authoritative snapshot and durable terminal
// records untouched; ordinary answer messages already have their own chat rows.
export function visibleQuestionMessages(items:Item[],acknowledged:ReadonlySet<string>=new Set()):Item[] {
 return items.flatMap(item=>{
  if(!item.questionCards?.length)return [item];
  const cards=item.questionCards.filter(card=>!questionAnswered(card.state)&&!acknowledged.has(questionIdentity(card)));
  if(cards.length===item.questionCards.length)return [item];
  if(cards.length)return [{...item,questionCards:cards}];
  // Question text is a generated card fallback, not a separate assistant message.
  // Retain any real artifacts without resurrecting that fallback or an empty row.
  return item.artifacts?.length?[{...item,text:'',questionCards:[]}]:[];
 });
}
