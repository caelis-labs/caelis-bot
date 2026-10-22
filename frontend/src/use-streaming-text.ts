import {useEffect, useLayoutEffect, useRef, useState} from 'react';
import {TextReveal} from './streaming-text';

export function useStreamingText(text: string, animate: boolean) {
 const [immediate,setImmediate] = useState(() => typeof window !== 'undefined' && (document.hidden || matchMedia('(prefers-reduced-motion: reduce)').matches));
 const reveal = useRef<TextReveal|null>(null);
 if (!reveal.current) {
  reveal.current = new TextReveal();
  reveal.current.update(text, animate, performance.now(), immediate);
 }
 const frame=useRef(0);
 const [shown,setShown] = useState(() => reveal.current!.value(performance.now()));
 useEffect(() => {
  const reduced = matchMedia('(prefers-reduced-motion: reduce)');
  const flush = () => {
   cancelAnimationFrame(frame.current);
   const state=reveal.current!;
   state.flush(performance.now());
   setShown(state.value(performance.now()));
  };
  const sync = () => { const direct=document.hidden || reduced.matches;if(direct)flush();setImmediate(direct); };
  // Native WebKit can suspend frames without changing document.hidden. Resume
  // from the latest received prefix instead of spending a wall-clock backlog.
  const resume = () => {flush();sync();};
  sync();
  reduced.addEventListener('change', sync);
  document.addEventListener('visibilitychange', resume);
  window.addEventListener('focus',resume);window.addEventListener('pageshow',resume);
  return () => { reduced.removeEventListener('change', sync); document.removeEventListener('visibilitychange', resume);window.removeEventListener('focus',resume);window.removeEventListener('pageshow',resume); };
 }, []);
 useLayoutEffect(() => {
  const state = reveal.current!;
  state.update(text, animate, performance.now(), immediate);
  let active=true;
  const paint = (now: number) => {
   if(!active)return;
   setShown(state.value(now));
   if (state.pending) frame.current = requestAnimationFrame(paint);
  };
  paint(performance.now());
  return () => {active=false;cancelAnimationFrame(frame.current);};
 }, [text, animate, immediate]);
 // Terminal/history text is complete in this render, before any layout effect
 // or resumed frame can run. The effect also clears the old reveal queue.
 return !animate||immediate?text:shown;
}
