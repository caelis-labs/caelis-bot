// Presentation-only buffering. Native text, receipts and approvals remain immediate.
const segmenter = new Intl.Segmenter(undefined, {granularity:'grapheme'});
export class TextReveal {
 private target = '';
 private displayed = '';
 private ends: number[] = [];
 private count = 0;
 private last = 0;
 private credit = 0;
 private animate = false;

 update(text: string, animate: boolean, now: number, immediate = false) {
  this.value(now);
  if (!immediate && text === this.target && animate === this.animate) return;
  const pending = this.pending;
  const append = text.startsWith(this.target);
  this.animate = animate;
  this.target = text;
  // History, corrections, reduced motion and hidden surfaces never replay text.
  if (immediate || !animate || !append) {
   this.displayed = text;
   this.ends = [];
   this.count = 0;
   this.credit = 0;
   this.last = now;
   return;
  }
  this.ends = Array.from(segmenter.segment(text), part => part.index + part.segment.length);
  // A delta may complete a previously displayed grapheme (accent, ZWJ,
  // flag). Keep that visible prefix and reveal the rest of its cluster now.
  this.count = this.displayed.length === 0 ? 0 : this.ends.findIndex(end => end >= this.displayed.length) + 1;
  this.displayed = text.slice(0, this.count ? this.ends[this.count-1] : 0);
  // Keep the fractional character budget across deltas and completion. Starting
  // a new deadline on every snapshot causes bursts, pauses and lost progress.
  if (!pending) this.credit = 0;
  this.last = now;
 }
 value(now: number): string {
  if (this.pending) {
   // 60–120 graphemes/second: larger queues catch up without dumping whole
   // paragraphs. A delayed frame must not spend an entire wall-clock backlog.
   const rate = Math.min(120, Math.max(60, (this.ends.length-this.count)/0.8));
   this.credit += Math.max(0, Math.min(48, now-this.last))*rate/1000;
   const step = Math.floor(this.credit);
   this.credit -= step;
   this.count = Math.min(this.ends.length, this.count+step);
   this.displayed = this.target.slice(0, this.count ? this.ends[this.count-1] : 0);
   if (!this.pending) this.credit = 0;
  }
  this.last = now;
  return this.displayed;
 }
 get pending() { return this.displayed !== this.target; }
}
