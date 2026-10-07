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

// A receipt can be visible before the host finishes removing its selected
// files. Keep observing the original accepted send until that exact batch is
// absent; later attachments and draft edits belong to the next message.
export function acceptedDraftSettled(request: {text:string;fileIds:string[];referenceIds:string[]}, revision:number,
 next: {revision:number;text:string;referenceIds:string[]}, files: readonly {id:string}[]): boolean {
 const consumed=new Set(request.fileIds);
 if(files.some(file=>consumed.has(file.id)))return false;
 const sameDraft=next.text===request.text && next.referenceIds.length===request.referenceIds.length &&
  next.referenceIds.every((id,index)=>id===request.referenceIds[index]);
 return !sameDraft || next.revision>revision;
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
