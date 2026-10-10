import type { Reference } from './backend/contract';

// The resident core guide is already supplied to the Bot. Selecting it again
// would only duplicate context. Identical skill entries from multiple roots
// share one menu row; the Runtime's underlying references remain untouched.
export function attachmentMenuReferences(references: Reference[]) {
 const skills:Reference[]=[],plugins:Reference[]=[];
 const seen=new Set<string>();
 for(const reference of references) {
  if(reference.kind!=='plugin'&&reference.name.toLowerCase()==='bot-core')continue;
  const key=attachmentReferenceKey(reference);
  if(seen.has(key))continue;
  seen.add(key);
  (reference.kind==='plugin'?plugins:skills).push(reference);
 }
 return {skills,plugins};
}

export function attachmentReferenceKey(reference:Reference) {
 return [reference.kind,reference.name.trim().toLowerCase(),reference.description.trim()].join('\u0000');
}

export function attachmentMenuDescription(description:string) {
 const text=description.replace(/\s+/g,' ').trim();
 const first=text.match(/^.*?[.!?。！？](?=\s|$)/)?.[0]??text;
 const characters=Array.from(first);
 return characters.length>72?characters.slice(0,71).join('').trimEnd()+'…':first;
}
