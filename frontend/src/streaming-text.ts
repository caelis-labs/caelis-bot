// Presentation-only buffering. Native text, receipts and approvals remain immediate.
const segmenter = new Intl.Segmenter(undefined, {granularity:'grapheme'});
export class TextReveal {
 private target = '';
 private displayed = '';
 private ends: number[] = [];
 private from = 0;
 private started = 0;
 private duration = 0;
 private streaming = false;

 update(text: string, streaming: boolean, now: number, immediate = false) {
  this.value(now);
  if (!immediate && text === this.target && streaming === this.streaming) return;
  const wasStreaming = this.streaming;
  const pending = this.pending;
  const append = text.startsWith(this.target);
  this.streaming = streaming;
  this.target = text;
  // History, corrections, reduced motion and hidden surfaces never replay text.
  if (immediate || !append || (!streaming && !wasStreaming && !pending)) {
   this.displayed = text;
   this.duration = 0;
   return;
  }
  this.ends = Array.from(segmenter.segment(text), part => part.index + part.segment.length);
  this.from = this.ends.filter(end => end <= this.displayed.length).length;
  this.started = now;
  // Catch up within one snapshot interval, even after a large provider chunk.
  // Completion drains briefly instead of dumping the final chunk in one frame.
  this.duration = streaming ? Math.min(450, Math.max(100, (this.ends.length-this.from)*18)) : 120;
 }
 value(now: number): string {
  if (this.duration > 0) {
   const progress = Math.max(0, Math.min(1, (now-this.started)/this.duration));
   const count = Math.floor(this.from + (this.ends.length-this.from)*progress);
   this.displayed = this.target.slice(0, count ? this.ends[count-1] : 0);
   if (progress === 1) this.duration = 0;
  }
  return this.displayed;
 }
 get pending() { return this.displayed !== this.target; }
}
