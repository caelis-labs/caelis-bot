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
 const chat=message.role==='user'||message.role==='assistant';
 return message.id && (chat||message.artifacts?.length)
  ?[{...message,role:chat?message.role as WorkerMessage['role']:'assistant',text:chat?message.text:'',status:'completed',artifacts:message.artifacts??[]}]:[];
}

export function workerMessages(conversation?:TaskConversation,items:Item[]=[]):WorkerMessage[] {
 const history=new Map<string,WorkerMessage>();
 for(const message of conversation?.messages??[])for(const row of historical(message))history.set(row.id,row);
 const live=new Map<string,WorkerMessage>();
 for(const item of items){
  const message=['user','assistant'].includes(item.kind);
  // Tool/activity prose stays hidden, but its host-validated files are still
  // task output. Native history does not necessarily include these items.
  if(!item.id||!message&&!item.artifacts?.length)continue;
  const id=item.historyId||`observed:${item.id}`;
  const row:WorkerMessage={id,role:message?item.kind as WorkerMessage['role']:'assistant',text:message?item.text:'',status:item.status,artifacts:item.artifacts??[]};
  const final=history.get(id);
  // History supplies full completed text (the live window may be truncated).
  // A resumed native message may extend its previous completed history row.
  if(final){
   const growing=item.status==='inProgress'&&!historyCoversObservation(final.text,row.text);
   const extended=row.text!==final.text&&row.text.startsWith(final.text);
   row.artifacts=[...new Map([...final.artifacts,...row.artifacts].map(file=>[file.id,file])).values()];
   if(!growing&&!extended){history.set(id,{...final,artifacts:row.artifacts});continue;}
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

// While reading history, appending output must not slide the start of the
// rendered window. The anchor is an original message ID, never its text/index.
export function workerMessageStart(messages:Pick<WorkerMessage,'id'>[],limit:number,startID?:string):number {
 const anchored=startID?messages.findIndex(message=>message.id===startID):-1;
 return anchored>=0?anchored:Math.max(0,messages.length-limit);
}

// The first observation is already history. Pace only newly received prefixes
// after that baseline; terminal output and corrections always render directly.
export function workerLiveReplies(previous:readonly WorkerMessage[]|undefined,next:readonly WorkerMessage[],live:ReadonlySet<string>):Set<string> {
 const result=new Set<string>();
 if(!previous)return result;
 const before=new Map(previous.map(message=>[message.id,message]));
 for(const message of next){
  if(message.role!=='assistant'||message.status!=='inProgress')continue;
  const old=before.get(message.id);
  if(live.has(message.id)||!old||message.text.length>old.text.length&&message.text.startsWith(old.text))result.add(message.id);
 }
 return result;
}
