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
  return this.completion ??= Promise.resolve().then(action);
 }
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
