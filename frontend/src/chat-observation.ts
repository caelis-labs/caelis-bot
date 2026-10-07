// A snapshot revision covers backend events, but outbox and status changes can
// share that revision. Fence both request order and the visible-surface lifetime.
export class ConversationOrder {
 private generation = 0;
 private sequence = 0;
 private acceptedSequence = 0;
 revision = 0;
 reset() { this.generation++; this.acceptedSequence = 0; this.revision = 0; }
 request() { return {generation:this.generation, sequence:++this.sequence}; }
 accept(ticket: ReturnType<ConversationOrder['request']>, revision: number) {
  if (ticket.generation !== this.generation || ticket.sequence < this.acceptedSequence || revision < this.revision) return false;
  this.acceptedSequence = ticket.sequence;
  this.revision = revision;
  return true;
 }
}

// A native selection event or direct picker result supersedes any older
// DraftFiles read, including one paired with a slow Draft read after a send.
export class FileObservationOrder {
 private sequence = 0;
 request() { return ++this.sequence; }
 accept(ticket: number) { return ticket === this.sequence; }
 reset() { this.sequence++; }
}

// Draft may finish an accepted cleanup. Read the native selection only after
// that operation so an older file list cannot reappear beside a cleared draft.
export async function readDraftAndFiles<D,F>(draft:()=>Promise<D>,files:()=>Promise<F>): Promise<[D,F]> {
 const next=await draft();
 return [next,await files()];
}

// Direct submission and polling can confirm the same request. Consume its draft
// once; a failed post-acceptance read must never turn an accepted send into unknown.
export class SubmissionProgress {
 outcome = 'sending';
 private completion: Promise<void> | null = null;
 observe(outcome: string) {
  if (this.outcome !== 'accepted') this.outcome = outcome || 'unknown';
  return this.outcome;
 }
 synchronize(action: () => Promise<void>) {
  if (this.outcome !== 'accepted') throw new Error('Submission is not accepted');
  if (!this.completion) {
   const attempt=Promise.resolve().then(action).catch(error=>{
    if(this.completion===attempt)this.completion=null;
    throw error;
   });
   this.completion=attempt;
  }
  return this.completion;
 }
}

// The backend journal either advances the draft or explicitly reports accepted
// cleanup pending with the exact consumed IDs. A concurrent DraftFiles result
// can be older than a native selection event and is never authority to resend.
export function acceptedDraftSettled(request: {text:string;fileIds:string[];referenceIds:string[]}, revision:number,
 next: {revision:number;text:string;referenceIds:string[];cleanupPending?:boolean;consumedFileIds?:string[]}): boolean {
 if(next.cleanupPending)return request.fileIds.every(id=>next.consumedFileIds?.includes(id));
 const sameDraft=next.text===request.text && next.referenceIds.length===request.referenceIds.length &&
  next.referenceIds.every((id,index)=>id===request.referenceIds[index]);
 return !sameDraft || next.revision>revision;
}

export function visibleDraftFiles<T extends {id:string}>(files: readonly T[], draft: {cleanupPending?:boolean;consumedFileIds?:string[]}): T[] {
 if(!draft.cleanupPending)return [...files];
 const consumed=new Set(draft.consumedFileIds??[]);
 return files.filter(file=>!consumed.has(file.id));
}

// Only retry observations. Cancellation fences late results, and success stops
// the loop. Native sends, approvals and draft writes do not use this helper.
export function retryRead<T>(read:()=>Promise<T>,accept:(value:T)=>void,failed:()=>void=()=>{},delay=3000) {
 let active=true,timer:ReturnType<typeof setTimeout>|undefined;
 const attempt=async()=>{
  try{const value=await read();if(active)accept(value);}
  catch{if(active){failed();timer=setTimeout(()=>void attempt(),delay);}}
 };
 void attempt();
 return()=>{active=false;if(timer!==undefined)clearTimeout(timer);};
}

// The native draft is a full replacement. While a write is in flight, only its
// latest successor is needed; sending still waits until that successor is saved.
export class DraftQueue {
 private pending: (() => Promise<void>) | null = null;
 private writing: Promise<void> | null = null;
 enqueue(write: () => Promise<void>) {
  this.pending = write;
  if (!this.writing) {
   this.writing = Promise.resolve().then(async()=>{
    try {
     while (this.pending) {
      const next=this.pending;this.pending=null;
      await next();
     }
    } finally { this.writing=null; }
   });
  }
  return this.writing;
 }
 flush() { return this.writing ?? Promise.resolve(); }
}
