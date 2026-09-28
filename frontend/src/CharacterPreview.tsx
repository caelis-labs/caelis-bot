import {useEffect,useRef,useState} from 'react';
import {Box3,Mesh,OrthographicCamera,Scene,Texture,Vector3,WebGLRenderer,type Material,type Object3D} from 'three';
import type {Appearance} from './appearance';
import {characterAssets} from './character/assets';
import {loadCharacterModel} from './character/load';
import {characterProfile} from './character/profile';
import {CharacterAnimation} from './character/animation';
import {useI18n} from './i18n';

function releaseModel(root:Object3D) {
 const materials=new Set<Material>(),textures=new Set<Texture>();
 root.traverse(node=>{if(node instanceof Mesh){node.geometry.dispose();for(const material of Array.isArray(node.material)?node.material:[node.material])materials.add(material);if('skeleton' in node)(node.skeleton as {dispose():void}).dispose();}});
 for(const material of materials){for(const value of Object.values(material))if(value instanceof Texture)textures.add(value);material.dispose();}
 textures.forEach(texture=>texture.dispose());
}

// A still preview: no desktop events, hit-mask uploads or perpetual animation loop.
export function CharacterPreview({appearance}:{appearance:Appearance}) {
 const {t}=useI18n(),canvas=useRef<HTMLCanvasElement>(null);
 const [status,setStatus]=useState<'loading'|'ready'|'failed'>('loading');
 useEffect(()=>{
  let disposed=false,root:Object3D|undefined,profile:ReturnType<typeof characterProfile>|undefined,player:CharacterAnimation|undefined;
  let renderer:WebGLRenderer;
  try{renderer=new WebGLRenderer({canvas:canvas.current!,alpha:true,antialias:true});}catch{setStatus('failed');return;}
  const scene=new Scene(),camera=new OrthographicCamera(-1,1,1,-1,.1,50);
  renderer.setClearColor(0,0);renderer.setPixelRatio(Math.min(devicePixelRatio,2));setStatus('loading');
  const draw=()=>{
   if(!root||disposed)return;
   const bounds=new Box3().setFromObject(root),size=bounds.getSize(new Vector3()),center=bounds.getCenter(new Vector3());
   const width=canvas.current!.clientWidth,height=canvas.current!.clientHeight;
   if(!width||!height)return;
   const half=Math.max(size.y/2,size.x/2/(width/height))*1.12;
   camera.left=-half*width/height;camera.right=-camera.left;camera.top=half;camera.bottom=-half;
   camera.position.set(center.x,center.y,center.z+Math.max(size.z,1)+5);camera.lookAt(center);camera.updateProjectionMatrix();
   renderer.setSize(width,height,false);renderer.shadowMap.needsUpdate=true;renderer.render(scene,camera);
  };
  const resize=new ResizeObserver(draw);resize.observe(canvas.current!);
  void loadCharacterModel(appearance.model||(appearance.basic?characterAssets.fallback:characterAssets.model),!appearance.basic).then(gltf=>{
   if(disposed){releaseModel(gltf.scene);return;}
   root=gltf.scene;scene.add(root);player=new CharacterAnimation(root,gltf.animations,!appearance.basic);
   const bounds=new Box3().setFromObject(root),size=bounds.getSize(new Vector3());
   if(!Number.isFinite(size.length())||size.y<.001||size.x<.001)throw new Error('Invalid character bounds');
   profile=characterProfile(renderer,scene,root);draw();setStatus('ready');
  }).catch(()=>{if(!disposed)setStatus('failed');});
  return()=>{disposed=true;resize.disconnect();player?.dispose();profile?.dispose();if(root)releaseModel(root);renderer.dispose();renderer.forceContextLoss();};
 },[appearance.model,appearance.basic,appearance.key]);
 return <figure className="character-preview"><canvas ref={canvas} role="img" aria-label={t('settings.characterPreview')} hidden={status==='failed'}/>{status!=='ready'&&<figcaption role="status">{t(status==='failed'?'settings.previewFailed':'common.loading')}</figcaption>}</figure>;
}
