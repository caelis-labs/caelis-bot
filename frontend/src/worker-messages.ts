import type {Artifact, Item, TaskConversation, TaskConversationMessage} from './backend/contract';

// Presentation protocol shared by both Worker surfaces. Only Runtime-provided
// message identities join history and observation; prose and TurnKey never do.
export type WorkerMessage = {
 id:string;
 role:'user'|'assistant';
 text:string;
 status?:string;
 artifacts:Artifact[];
};

// Existing Runtime observation bounds: Codex keeps 16,000 runes plus an
// ellipsis; Caelis keeps the final 128 KiB at a UTF-8 boundary. A full history
// row for the same native ID must not shrink back to that bounded projection.
export function historyCoversObservation(full:string,observed:string):boolean {
 if(full.startsWith(observed))return true;
 if(full.length>observed.length-1&&observed.endsWith('…')&&Array.from(observed).length===16001&&full.startsWith(observed.slice(0,-1)))return true;
 if(observed.length>=((128<<10)-3)/3&&full.endsWith(observed)){
  const bytes=new TextEncoder().encode(observed).length;
  return bytes>=(128<<10)-3&&bytes<=(128<<10);
 }
 return false;
}

function historical(message:TaskConversationMessage):WorkerMessage[] {
 return message.id && (message.role==='user'||message.role==='assistant')
  ?[{...message,role:message.role,status:'completed',artifacts:message.artifacts??[]}]:[];
}

export function workerMessages(conversation?:TaskConversation,items:Item[]=[]):WorkerMessage[] {
 const history=new Map<string,WorkerMessage>();
 for(const message of conversation?.messages??[])for(const row of historical(message))history.set(row.id,row);
 const live=new Map<string,WorkerMessage>();
 for(const item of items){
  if(!item.id||!['user','assistant'].includes(item.kind))continue;
  const id=item.historyId||`observed:${item.id}`;
  const row:WorkerMessage={id,role:item.kind as WorkerMessage['role'],text:item.text,status:item.status,artifacts:item.artifacts??[]};
  const final=history.get(id);
  // History supplies full completed text (the live window may be truncated).
  // A resumed native message may extend its previous completed history row.
  if(final){
   const growing=item.status==='inProgress'&&!historyCoversObservation(final.text,row.text);
   const extended=row.text!==final.text&&row.text.startsWith(final.text);
   if(!growing&&!extended)continue;
   row.artifacts=[...new Map([...final.artifacts,...row.artifacts].map(file=>[file.id,file])).values()];
  }
  live.set(id,row);
 }
 // Live messages can be interleaved with completed history (e.g. a steering
 // user input after an unfinished reply). Place them before their next exact
 // native anchor rather than appending every live row after history.
 const before=new Map<string,WorkerMessage[]>();
 const tail:WorkerMessage[]=[];
 let pending:WorkerMessage[]=[];
 for(const item of items){
  const id=item.historyId||`observed:${item.id}`;
  if(history.has(id)){
   if(pending.length){before.set(id,[...(before.get(id)??[]),...pending]);pending=[];}
  }else if(live.has(id)){pending.push(live.get(id)!);live.delete(id);}
 }
 tail.push(...pending);
 const rows:WorkerMessage[]=[];
 for(const [id,message] of history){rows.push(...(before.get(id)??[]),live.get(id)??message);}
 return [...rows,...tail];
}
