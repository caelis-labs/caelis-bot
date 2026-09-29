import {useEffect, useLayoutEffect, useRef, useState} from 'react';
import {TextReveal} from './streaming-text';

export function useStreamingText(text: string, animate: boolean) {
 const [immediate,setImmediate] = useState(() => typeof window !== 'undefined' && (document.hidden || matchMedia('(prefers-reduced-motion: reduce)').matches));
 const reveal = useRef<TextReveal|null>(null);
 if (!reveal.current) {
  reveal.current = new TextReveal();
  reveal.current.update(text, animate, performance.now(), immediate);
 }
 const [shown,setShown] = useState(() => reveal.current!.value(performance.now()));
 useEffect(() => {
  const reduced = matchMedia('(prefers-reduced-motion: reduce)');
  const sync = () => setImmediate(document.hidden || reduced.matches);
  sync();
  reduced.addEventListener('change', sync);
  document.addEventListener('visibilitychange', sync);
  return () => { reduced.removeEventListener('change', sync); document.removeEventListener('visibilitychange', sync); };
 }, []);
 useLayoutEffect(() => {
  const state = reveal.current!;
  state.update(text, animate, performance.now(), immediate);
  let frame = 0;
  const paint = (now: number) => {
   setShown(state.value(now));
   if (state.pending) frame = requestAnimationFrame(paint);
  };
  paint(performance.now());
  return () => cancelAnimationFrame(frame);
 }, [text, animate, immediate]);
 return shown;
}
