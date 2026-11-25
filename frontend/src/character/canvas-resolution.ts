import type { WebGLRenderer } from 'three';

export const petWidth=180,petHeight=240;

// Size the visible WebGL buffer in display pixels. The native hit mask has
// its own fixed coordinate system and render target.
export function observeCharacterCanvas(renderer: WebGLRenderer, redraw: () => void) {
  const canvas = renderer.domElement;
  function resize() {
    const width = canvas.clientWidth, height = canvas.clientHeight;
    if (!width || !height) return;
    renderer.setDrawingBufferSize(width, height, window.devicePixelRatio || 1);
    redraw();
  }
  const displayDensity = () => matchMedia(`(resolution: ${window.devicePixelRatio || 1}dppx)`);
  let density = displayDensity();
  function changeDisplay() {
    density = displayDensity();
    density.addEventListener('change', changeDisplay, {once: true});
    resize();
  }
  density.addEventListener('change', changeDisplay, {once: true});
  const observer = new ResizeObserver(resize);
  observer.observe(canvas);
  resize();
  return () => {
    observer.disconnect();
    density.removeEventListener('change', changeDisplay);
  };
}
