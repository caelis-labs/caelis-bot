import { Box3, Group, Mesh, Texture, Vector3, type Material, type Object3D } from 'three';
import { GLTFLoader, type GLTF } from 'three/addons/loaders/GLTFLoader.js';
import { CharacterAnimation } from './animation';
import type { CharacterSource, Presentation } from './assets';
import { pruneEmptyViewTargets } from './view-correctives';

export const loadCharacterModel = (url:string) => new GLTFLoader().loadAsync(url);

export function releaseModel(root:Object3D) {
 const materials=new Set<Material>(),textures=new Set<Texture>();
 root.traverse(node=>{
  if(!(node instanceof Mesh))return;
  node.geometry.dispose();
  for(const material of Array.isArray(node.material)?node.material:[node.material])materials.add(material);
  if('skeleton' in node)(node.skeleton as {dispose():void}).dispose();
 });
 for(const material of materials){
  for(const value of Object.values(material))if(value instanceof Texture)textures.add(value);
  material.dispose();
 }
 textures.forEach(texture=>texture.dispose());
}

export type LoadedCharacter={
 root:Object3D;player:CharacterAnimation;presentation:Presentation;referenceLighting:boolean;dispose():void;
};

export function prepareCharacter(gltf:GLTF,source:CharacterSource):LoadedCharacter {
 const {presentation}=source;
 const rig=presentation.rig==='desktop-pet-v1';
 const scene=gltf.scene;
 const referenceLighting=!rig||scene.userData.desktopPetProfile==='reference-v1';
 let root:Object3D=scene;
 if(!rig){
  // Keep imported metadata outside procedural posing. Turn the centered,
  // floor-aligned parent while the mixer addresses the unmodified source rig.
  const bounds=new Box3().setFromObject(scene),size=bounds.getSize(new Vector3()),center=bounds.getCenter(new Vector3());
  if(!Number.isFinite(size.length())||size.y<.001||size.x<.001)throw new Error('Invalid character bounds');
  const pivot=new Group(),offset=new Group();
  offset.position.set(-center.x,-bounds.min.y,-center.z);
  pivot.scale.setScalar(Math.min(2.2/size.y,1.65/size.x));
  pivot.add(offset);offset.add(scene);root=pivot;
 }
 if(rig)pruneEmptyViewTargets(scene);
 const player=new CharacterAnimation(scene,gltf.animations,presentation,{strict:source.bundled,quietIdle:rig});
 return {root,player,presentation,referenceLighting,dispose(){player.dispose();releaseModel(root);}};
}

export async function loadCharacter(source:CharacterSource):Promise<LoadedCharacter> {
 const gltf=await loadCharacterModel(source.url);
 try{return prepareCharacter(gltf,source);}
 catch(error){releaseModel(gltf.scene);throw error;}
}
