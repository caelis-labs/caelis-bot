import { useEffect, useRef, useState } from 'react';
import type { Snapshot } from './backend/contract';
import { activityPortrait, animatedReplyID, completedReply, type PortraitClip } from './avatar-presentation';

export function useAvatarPresentation(snapshot:Snapshot|null,active:boolean){
 const previous=useRef<Snapshot|null>(null);
 const [completion,setCompletion]=useState<string|null>(null);
 const projected=activityPortrait(snapshot);
 const [settled,setSettled]=useState<PortraitClip>(projected);
 useEffect(()=>{
  if(!active){previous.current=null;setCompletion(null);return;}
  if(snapshot){
   const reply=completedReply(previous.current,snapshot);
   if(reply)setCompletion(reply);
   else if(snapshot.phase!=='completed'||!animatedReplyID(snapshot,completion))setCompletion(null);
  }
  previous.current=snapshot;
 },[snapshot,active,completion]);
 useEffect(()=>{
  if(!completion)return;
  const timer=setTimeout(()=>setCompletion(null),6000);
  return()=>clearTimeout(timer);
 },[completion]);
 // Smooth short-lived activity changes; approvals and replying are immediate.
 const urgent=projected==='waiting'||projected==='listen'||projected==='companion';
 useEffect(()=>{
  if(urgent){setSettled(projected);return;}
  const timer=setTimeout(()=>setSettled(projected),350);
  return()=>clearTimeout(timer);
 },[projected,urgent]);
 return {clip:urgent?projected:settled,completion};
}
