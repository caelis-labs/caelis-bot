type Viewport = Pick<HTMLElement, 'scrollTop' | 'scrollHeight' | 'clientHeight'>;

// Remember intent across layout changes: mounting the composer, restoring a
// multiline draft and resizing a window all change the available reading area.
export class ChatScroll {
 following = true;
 private height = 0;
 private viewport = 0;
 private readonly element: Viewport;

 constructor(element: Viewport) { this.element = element; }

 latest() {
  this.following = true;
  this.layout();
 }

 layout() {
  if (this.following) this.element.scrollTop = this.element.scrollHeight;
  this.height = this.element.scrollHeight;
  this.viewport = this.element.clientHeight;
 }

 scrolled() {
  // A resize/scroll-anchor event must not turn bottom-following into a manual
  // history read before ResizeObserver has applied the new layout.
  if (this.height !== this.element.scrollHeight || this.viewport !== this.element.clientHeight) {
   this.layout();
  } else {
   this.following = this.element.scrollHeight - this.element.scrollTop - this.element.clientHeight < 56;
  }
  return this.following;
 }
}
