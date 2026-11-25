import {useEffect,useRef,useState} from 'react';
import {Box3,OrthographicCamera,Scene,Vector3,WebGLRenderer} from 'three';
import type {Appearance} from './appearance';
import {observeCharacterCanvas} from './character/canvas-resolution';
import {characterSource} from './character/assets';
import {loadCharacter,type LoadedCharacter} from './character/load';
import {characterProfile} from './character/profile';
import {useI18n} from './i18n';

// A still preview: no desktop events, hit-mask uploads or perpetual animation loop.
export function CharacterPreview({appearance}:{appearance:Appearance}) {
 const {t}=useI18n(),surface=useRef<HTMLDivElement>(null);
 const {key,model}=appearance;
 const [status,setStatus]=useState<'loading'|'ready'|'failed'>('loading');
 useEffect(()=>{
  let disposed=false,character:LoadedCharacter|undefined,profile:ReturnType<typeof characterProfile>|undefined;
  // A lost WebGL context cannot be reused, even by a new renderer. Each effect
  // owns its canvas as well as its renderer, including StrictMode effect replay.
  const canvas=document.createElement('canvas');surface.current!.append(canvas);
  let renderer:WebGLRenderer;
  try{renderer=new WebGLRenderer({canvas,alpha:true,antialias:true});}catch{canvas.remove();setStatus('failed');return;}
  const scene=new Scene(),camera=new OrthographicCamera(-1,1,1,-1,.1,50);
  renderer.setClearColor(0,0);setStatus('loading');
  const draw=()=>{
   const root=character?.root;if(!root||disposed)return;
   const bounds=new Box3().setFromObject(root),size=bounds.getSize(new Vector3()),center=bounds.getCenter(new Vector3());
   const width=canvas.clientWidth,height=canvas.clientHeight;
   if(!width||!height)return;
   const half=Math.max(size.y/2,size.x/2/(width/height))*1.12;
   camera.left=-half*width/height;camera.right=-camera.left;camera.top=half;camera.bottom=-half;
   camera.position.set(center.x,center.y,center.z+Math.max(size.z,1)+5);camera.lookAt(center);camera.updateProjectionMatrix();
   renderer.shadowMap.needsUpdate=true;renderer.render(scene,camera);
  };
  const stopSizing=observeCharacterCanvas(renderer,draw);
  void loadCharacter(characterSource(key,model)).then(loaded=>{
   if(disposed){loaded.dispose();return;}
   character=loaded;
   const root=loaded.root,size=new Box3().setFromObject(root).getSize(new Vector3());
   if(!Number.isFinite(size.length())||size.y<.001||size.x<.001)throw new Error('Invalid character bounds');
   scene.add(root);
   profile=characterProfile(renderer,scene,root,loaded.referenceLighting);draw();setStatus('ready');
  }).catch(()=>{if(!disposed)setStatus('failed');});
  return()=>{disposed=true;stopSizing();profile?.dispose();character?.dispose();renderer.dispose();renderer.forceContextLoss();canvas.remove();};
 },[key,model]);
 return <figure className="character-preview"><div ref={surface} className="character-preview-surface" role="img" aria-label={t('settings.characterPreview')} hidden={status==='failed'}/>{status!=='ready'&&<figcaption role="status">{t(status==='failed'?'settings.previewFailed':'common.loading')}</figcaption>}</figure>;
}
