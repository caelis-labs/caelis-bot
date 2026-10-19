// Production renderers with synthetic native facts. No user data or model calls.
import React,{useEffect,useRef} from 'react';
import {createRoot} from 'react-dom/client';
import {PortraitAvatar,builtinPortrait} from '../frontend/src/PortraitAvatar';
import {builtinAppearance} from '../frontend/src/appearance';
import {mountPet} from '../frontend/src/character/pet';
import {observeStatusMotion} from '../frontend/src/status-motion';
import '../frontend/src/application.css';
Object.assign(window,{acceptanceNative:async(name:string)=>{
 const method=name.split('.').at(-1);
 if(method==='Appearance')return builtinAppearance;
 if(method==='Placement')return {visible:true,scale:1};
 if(method==='CharacterActivity')return 'idle';
 if(method==='DesktopContext')return undefined;
 if(method==='SetPackedHitMask'||method==='FinishPlane')return;
 throw Error('Unexpected fixture native call: '+method);
}});
function Pet(){
 const ref=useRef<HTMLCanvasElement>(null);
 useEffect(()=>mountPet(ref.current!,()=>{document.body.dataset.petError='true';}),[]);
 return <canvas id="pet" ref={ref} width={180} height={240} style={{width:180,height:240}}/>;
}
observeStatusMotion(document.body);
createRoot(document.getElementById('root')!).render(<main style={{padding:30,height:2000,background:'#eee',color:'#222'}}>
 <h1>Reduced motion feedback fixture</h1><Pet/>
 <div id="current" style={{display:'flex'}}><PortraitAvatar portrait={builtinPortrait!} clip="thinking" animate/></div>
 <div id="history" style={{display:'flex'}}><PortraitAvatar portrait={builtinPortrait!} clip="thinking" animate={false}/></div>
 <div style={{display:'flex',gap:25,padding:20}}>
  <span className="composer-progress"/><span className="activity-spinner"/>
  <div className="working-message is-active"><span className="loading-dots"><i/><i/><i/></span></div>
  <span className="setup-spinner"/>
  <div className="runtime-install-progress" style={{width:120}}><span/></div>
  <div className="windows-task-compact" data-running="true" style={{height:30,width:60,background:'#555'}}><span>●</span><span>●</span><span>●</span></div>
 </div>
</main>);
