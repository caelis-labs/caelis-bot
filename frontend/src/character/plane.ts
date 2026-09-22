import { characterAssets } from './assets';
import { DirectionalLight, Euler, Quaternion, Group, HemisphereLight, Mesh, NeutralToneMapping, OrthographicCamera, Scene, SRGBColorSpace, Vector3, WebGLRenderer } from 'three';
import { loadCharacterModel } from './load';
import { desktop } from '../desktop';
export type Flight = {id:string;width:number;height:number;x:number;y:number;scale:number;direction:number};
export function disposePlane(root:Group){root.traverse(o=>{if(o instanceof Mesh){o.geometry.dispose();for(const material of Array.isArray(o.material)?o.material:[o.material])material.dispose();}});}

/** A closed, smooth arc in one bounded native prop surface. No per-frame IPC. */
export function flightPoint(f:Flight,t:number){
 const ease=(v:number)=>v*v*v*(v*(v*6-15)+10);
 const angle=ease(Math.max(0,Math.min(1,t)))*Math.PI*2;
 const reach=f.direction>0?f.width-f.x-30:f.x-30;
 const top=Math.max(0,f.height-f.y-35),lower=Math.max(0,f.y-35);
 // Always remains inside the work area, including on small/edge-positioned surfaces.
 return new Vector3(f.x+f.direction*reach*.46*(1-Math.cos(angle)),f.y+top*.52*Math.sin(angle/2)**2+Math.min(top*.35,lower*.6)*Math.sin(angle),0);
}
export function mountPlane(canvas:HTMLCanvasElement){
 let disposed=false,renderer:WebGLRenderer|undefined,scene:Scene|undefined,model:Group|undefined;
 let flight:Flight|undefined,t=0,previous=0,frame=0,timer=0;
 const reduced=matchMedia('(prefers-reduced-motion: reduce)');
 const orientation=new Quaternion(),rest=new Quaternion().setFromEuler(new Euler(1.05,0,-.12)),euler=new Euler();
 const camera=new OrthographicCamera(0,520,360,0,.1,100);camera.position.z=40;
 function stop(){clearTimeout(timer);cancelAnimationFrame(frame);frame=timer=0;flight=undefined;if(renderer)renderer.clear();}
 function finish(completed:boolean){const id=flight?.id;stop();if(id)void desktop('FinishPlane',id,completed);}
 function tick(now:number){
  if(!flight||!renderer||!scene||!model)return;
  const dt=Math.min(.1,(now-previous)/1000);t+=dt;previous=now;
  const progress=Math.min(1,t/7.5),position=flightPoint(flight,progress);
  const before=flightPoint(flight,Math.max(0,progress-.004)),after=flightPoint(flight,Math.min(1,progress+.004));
  model.position.copy(position);
  const heading=Math.atan2(after.y-before.y,after.x-before.x);
  orientation.setFromEuler(euler.set(1.05+Math.sin(progress*Math.PI*2)*.22,0,heading));
  const arrival=Math.max(0,Math.min(1,(progress-.88)/.12));
  orientation.slerp(rest,arrival*arrival*(3-2*arrival));
  model.quaternion.slerp(orientation,1-Math.exp(-dt/.14));
  renderer.render(scene,camera);
  if(progress>=1){finish(true);return;}
  timer=window.setTimeout(()=>{frame=requestAnimationFrame(tick);},1000/30-6);
 }
 const launch=(event:Event)=>{
  const value=(event as CustomEvent<Flight>).detail;
  stop();if(!renderer||!scene||!model||reduced.matches){void desktop('FinishPlane',value.id,false);return;}
  flight=value;t=0;previous=performance.now();
  camera.right=value.width;camera.top=value.height;camera.updateProjectionMatrix();
  renderer.setPixelRatio(Math.min(devicePixelRatio,1.5));renderer.setSize(value.width,value.height,false);
  model.scale.setScalar(24*value.scale);model.quaternion.copy(rest);model.position.copy(flightPoint(value,0));
  tick(previous);
 };
 const cancelled=(event:Event)=>{if((event as CustomEvent<{id:string}>).detail.id===flight?.id)stop();};
 const reduce=()=>{if(reduced.matches)finish(false);};
 const lost=(e:Event)=>{e.preventDefault();finish(false);void desktop('PlaneReady',false);};
 const restored=()=>{if(!disposed)void desktop('PlaneReady',true);};
 window.addEventListener('prop-flight',launch);window.addEventListener('prop-stop',cancelled);reduced.addEventListener('change',reduce);
 canvas.addEventListener('webglcontextlost',lost);canvas.addEventListener('webglcontextrestored',restored);
 void loadCharacterModel(characterAssets.paperPlane).then(gltf=>{
  if(disposed){disposePlane(gltf.scene);return;}
  model=gltf.scene;scene=new Scene();scene.add(model,new HemisphereLight(0xfff8f4,0xf1e7ee,1));
  const light=new DirectionalLight(0xfff7f2,2.6);light.position.set(-3,4,5);scene.add(light);
  renderer=new WebGLRenderer({canvas,alpha:true,antialias:true});renderer.setClearColor(0,0);
  renderer.outputColorSpace=SRGBColorSpace;renderer.toneMapping=NeutralToneMapping;renderer.toneMappingExposure=1.15;
  return desktop('PlaneReady',true);
 }).catch(()=>void desktop('PlaneReady',false));
 return()=>{disposed=true;finish(false);void desktop('PlaneReady',false);window.removeEventListener('prop-flight',launch);window.removeEventListener('prop-stop',cancelled);reduced.removeEventListener('change',reduce);canvas.removeEventListener('webglcontextlost',lost);canvas.removeEventListener('webglcontextrestored',restored);if(model)disposePlane(model);renderer?.dispose();renderer?.forceContextLoss();};
}
