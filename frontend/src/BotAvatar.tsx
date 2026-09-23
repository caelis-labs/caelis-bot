import { memo, useEffect, useId, useMemo, useRef } from 'react';
import { desktop } from './desktop';
import { useAppearance } from './appearance';
import { animateAvatar } from './avatar-motion';

// A v1 asset pack remains usable with its PNG; v2 adds the optional layered art.
const artwork=Object.values(import.meta.glob<string>('../assets/caelis-avatar-v1.svg',{eager:true,query:'?raw',import:'default'}))[0]??'';
const avatarURL=Object.values(import.meta.glob<string>('../assets/caelis-avatar-v1.svg',{eager:true,query:'?url&no-inline',import:'default'}))[0]??'/icons/caelis-avatar.png';
export function BotAvatar({animate=false}:{animate?:boolean}) {
 const appearance=useAppearance();
 if(appearance.avatar)return <img key={appearance.avatar} className="bot-avatar" src={appearance.avatar} alt="Caelis Bot" width="32" height="32" draggable={false} onError={e=>{if(e.currentTarget.dataset.fallback)return;e.currentTarget.dataset.fallback='true';e.currentTarget.src=avatarURL;void desktop('FallbackAppearance',appearance.revision).catch(()=>{});}}/>;
 return animate&&artwork?<MovingAvatar/>:<img className="bot-avatar" src={avatarURL} alt="Caelis Bot" width="32" height="32" draggable={false} onError={e=>{if(!e.currentTarget.src.endsWith('/caelis-avatar.png'))e.currentTarget.src='/icons/caelis-avatar.png';}}/>;
}

const MovingAvatar=memo(function MovingAvatar() {
 const host=useRef<HTMLSpanElement>(null),id=useId().replace(/[^a-zA-Z0-9-]/g,'');
 // Only a checked, bundled finished asset is inlined. Never inline model output
 // or remote SVG. Namespace gradients when more than one surface is mounted.
 const markup=useMemo(()=>({__html:artwork.replace(/id="([a-z][a-z0-9-]*)"/g,`id="avatar-${id}-$1"`).replace(/url\(#([a-z][a-z0-9-]*)\)/g,`url(#avatar-${id}-$1)`)}),[id]);
 useEffect(()=>{
  const element=host.current!;
  const part=(name:string)=>element.querySelector<SVGGElement>(`[data-avatar-part="${name}"]`)!;
  const head=part('head'),left=part('eye-left'),right=part('eye-right'),lookLeft=part('look-left'),lookRight=part('look-right');
  const set=(node:SVGGElement,value:string)=>{if(node.getAttribute('transform')!==value)node.setAttribute('transform',value);};
  const animator=animateAvatar(pose=>{
   // Translate/stretch the whole silhouette, not just facial features inside
   // an unmoving image box. Most tilt belongs to that same outer body.
   const body=`translate(${pose.offsetX.toFixed(2)}px, ${pose.offsetY.toFixed(2)}px) rotate(${(pose.tilt*.75).toFixed(2)}deg) scale(${pose.scaleX.toFixed(3)}, ${pose.scaleY.toFixed(3)})`;
   if(element.style.transform!==body)element.style.transform=body;
   if(element.dataset.gesture!==pose.gesture)element.dataset.gesture=pose.gesture;
   set(head,`rotate(${(pose.tilt*.25).toFixed(2)} 64 90)`);
   set(left,`translate(43 83) scale(1 ${pose.openness.toFixed(3)}) translate(-43 -83)`);
   set(right,`translate(86 83) scale(1 ${pose.openness.toFixed(3)}) translate(-86 -83)`);
   const gaze=`translate(${pose.gazeX.toFixed(2)} ${pose.gazeY.toFixed(2)})`;
   set(lookLeft,gaze);set(lookRight,gaze);
  },{request:callback=>window.requestAnimationFrame(callback),cancel:id=>window.cancelAnimationFrame(id)});
  const reduced=matchMedia('(prefers-reduced-motion: reduce)');let inView=false;
  const sync=()=>animator.setRunning(inView&&!document.hidden&&!reduced.matches);
  const observer=new IntersectionObserver(entries=>{inView=entries.some(entry=>entry.isIntersecting);sync();});
  observer.observe(element);document.addEventListener('visibilitychange',sync);reduced.addEventListener('change',sync);
  return()=>{observer.disconnect();document.removeEventListener('visibilitychange',sync);reduced.removeEventListener('change',sync);animator.dispose();};
 },[]);
 return <span ref={host} className="bot-avatar animated-avatar" role="img" aria-label="Caelis Bot" dangerouslySetInnerHTML={markup}/>;
});
