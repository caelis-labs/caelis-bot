type Viewport = Pick<HTMLElement, 'scrollTop' | 'scrollHeight' | 'clientHeight'>;

// Remember intent across layout changes: mounting the composer, restoring a
// multiline draft and resizing a window all change the available reading area.
export class ChatScroll {
 following = true;
 private preferQuestions = true;
 private height = 0;
 private viewport = 0;
 private readonly element: Viewport;

 private readonly questionStart?:()=>number|undefined;
 constructor(element: Viewport, questionStart?:()=>number|undefined) { this.element = element; this.questionStart=questionStart; }

 private target() {
  const bottom=Math.max(0,this.element.scrollHeight-this.element.clientHeight);
  return Math.min(bottom,(this.preferQuestions?this.questionStart?.():undefined)??bottom);
 }

 latest(readQuestions=false) {
  this.preferQuestions=readQuestions;
  this.following = true;
  this.layout();
 }

 layout() {
  if (this.following) this.element.scrollTop = this.target();
  this.height = this.element.scrollHeight;
  this.viewport = this.element.clientHeight;
 }

 scrolled() {
  // A resize/scroll-anchor event must not turn bottom-following into a manual
  // history read before ResizeObserver has applied the new layout.
  if (this.height !== this.element.scrollHeight || this.viewport !== this.element.clientHeight) {
   this.layout();
  } else {
   this.following = Math.abs(this.target() - this.element.scrollTop) < 56;
  }
  return this.following;
 }
}
