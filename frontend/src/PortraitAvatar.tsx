import { useEffect, useRef } from 'react';
import pack from '../../resources/character-pack.json';
import { PortraitCache, portraitClock, portraitFrame, type PortraitSheet } from './portrait-player';
import type { PortraitClip } from './avatar-presentation';

type Portrait=PortraitSheet&{poster:string;clips:Record<string,string>};
// Official build-time contract only. Community packs keep their PNG contract.
const character=pack.characters.find(c=>c.id===pack.defaultCharacter);
export const builtinPortrait=(character?.variants.find(v=>v.id===pack.defaultVariant) as {portrait?:Portrait}|undefined)?.portrait;
const url=(path:string)=>`/${path.replace(/^frontend\/public\//,'')}`;
const cache=new PortraitCache<HTMLImageElement>(async path=>{
 const image=new Image();image.src=path;await image.decode();return image;
});

export function PortraitAvatar({portrait,clip,animate}:{portrait:Portrait;clip:PortraitClip;animate:boolean}){
 const canvas=useRef<HTMLCanvasElement>(null);
 const atlas=portrait.clips[clip]??portrait.clips.companion;
 useEffect(()=>{
  const element=canvas.current;
  if(!element||!animate||!atlas)return;
  const context=element.getContext('2d');if(!context)return;
  // Carry only a 128px snapshot across clip changes, not another decoded atlas.
  const previous=document.createElement('canvas');previous.width=previous.height=portrait.frameSize;
  previous.getContext('2d')?.drawImage(element,0,0);
  const hadFrame=element.dataset.frame!==undefined;
  let inView=false,disposed=false,epoch=0,clock:ReturnType<typeof portraitClock>|undefined;
  const reduced=matchMedia('(prefers-reduced-motion: reduce)');
  const showPoster=()=>{context.clearRect(0,0,element.width,element.height);delete element.dataset.ready;};
  const sync=()=>{
   const current=++epoch;clock?.dispose();clock=undefined;
   if(disposed||!inView||document.hidden||reduced.matches){showPoster();return;}
   void cache.get(url(atlas)).then(image=>{
    if(disposed||current!==epoch)return;
    if(image.naturalWidth!==portrait.columns*portrait.frameSize||image.naturalHeight!==Math.ceil(portrait.frameCount/portrait.columns)*portrait.frameSize){showPoster();return;}
    let lastFrame=-1;
    clock=portraitClock(elapsed=>{
     const {frame,x,y}=portraitFrame(portrait,elapsed);
     if(frame===lastFrame&&elapsed>.22)return;
     lastFrame=frame;context.clearRect(0,0,element.width,element.height);
     const opacity=hadFrame?Math.min(1,elapsed/.22):1;
     if(opacity<1){context.globalAlpha=1-opacity;context.drawImage(previous,0,0);}
     context.globalAlpha=opacity;
     context.drawImage(image,x,y,portrait.frameSize,portrait.frameSize,0,0,element.width,element.height);
     context.globalAlpha=1;element.dataset.frame=String(frame);element.dataset.ready='true';
    },{request:callback=>requestAnimationFrame(callback),cancel:id=>cancelAnimationFrame(id)});
    clock.setRunning(true);
   }).catch(()=>{if(!disposed&&current===epoch)showPoster();});
  };
  const observer=new IntersectionObserver(entries=>{inView=entries.some(entry=>entry.isIntersecting);sync();});
  observer.observe(element);document.addEventListener('visibilitychange',sync);reduced.addEventListener('change',sync);
  return()=>{disposed=true;epoch++;clock?.dispose();observer.disconnect();document.removeEventListener('visibilitychange',sync);reduced.removeEventListener('change',sync);};
 },[portrait,atlas,animate]);
 return <span className="bot-avatar portrait-avatar" role="img" aria-label="Caelis Bot" data-portrait={animate?clip:'still'}>
  <img src={url(portrait.poster)} alt="" draggable={false} onError={event=>{if(!event.currentTarget.dataset.fallback){event.currentTarget.dataset.fallback='true';event.currentTarget.src='/icons/caelis-avatar.png';}}}/>
  {animate&&<canvas ref={canvas} width={portrait.frameSize} height={portrait.frameSize} aria-hidden="true"/>}
 </span>;
}
