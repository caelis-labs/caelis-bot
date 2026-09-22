import { ColorManagement, DirectionalLight, Group, HemisphereLight, Mesh, MeshPhysicalMaterial, MeshStandardMaterial, NeutralToneMapping, NoToneMapping, PCFShadowMap, SRGBColorSpace, type Material, type Object3D, type Scene, type WebGLRenderer } from 'three';

/** Asset-scoped profiles keep the original handoff and reviewed Blender source distinct. */
export function characterProfile(renderer:WebGLRenderer,scene:Scene,root:Object3D) {
 // Blender Standard / exposure 0 → linear lighting followed by sRGB output.
 // Broad area lights are approximated with a neutral fill and three bounded lights.
 const reference=root.userData.desktopPetProfile==='reference-v1';
 ColorManagement.enabled=true;
 renderer.outputColorSpace=SRGBColorSpace;renderer.toneMapping=reference?NoToneMapping:NeutralToneMapping;renderer.toneMappingExposure=reference?1:1.15;
 renderer.shadowMap.enabled=true;renderer.shadowMap.type=PCFShadowMap;renderer.shadowMap.autoUpdate=false;
 const lights=new Group();scene.add(lights);
 const key=new DirectionalLight(reference?0xffffff:0xfff7f2,reference?1.7:2.6);key.position.set(-3,reference?5:4.5,reference?4:5);key.target.position.set(0,1.1,0);key.castShadow=true;
 key.shadow.mapSize.set(1024,1024);Object.assign(key.shadow.camera,{left:-1.45,right:1.45,top:1.45,bottom:-1.45,near:.5,far:12});key.shadow.camera.updateProjectionMatrix();
 key.shadow.bias=-.00012;key.shadow.normalBias=.0015;key.shadow.radius=4;key.shadow.intensity=reference?.30:.45;
 const fill=new DirectionalLight(reference?0xffffff:0xf0f4ff,reference?.8:.65);fill.position.set(3,reference?3:2,reference?3:4);fill.target.position.set(0,1.1,0);
 const rim=new DirectionalLight(reference?0xffffff:0xffedf5,reference?.8:.3);rim.position.set(reference?0:1,reference?4:3,reference?-3:-4);rim.target.position.set(0,1.2,0);
 lights.add(new HemisphereLight(reference?0xffffff:0xfff8f4,reference?0xffffff:0xf1e7ee,reference?1.6:1),key,key.target,fill,fill.target,rim,rim.target);
 const originals=new Map<Mesh,Material|Material[]>(),styled:Material[]=[];
 root.traverse(o=>{
  if(!(o instanceof Mesh))return;
  originals.set(o,o.material);o.frustumCulled=false;
  const sources=Array.isArray(o.material)?o.material:[o.material];
  const materials=sources.map(source=>{
   const m=source.clone();styled.push(m);
   // Reference GLB already carries the reviewed skin, iris and cloth response.
   if(!reference&&m instanceof MeshStandardMaterial){
    if(/Warm ivory cloth|Ice blue folds|Powder blue borders|Periwinkle lining/.test(m.name))m.emissiveIntensity=.45;
    if(/Peach/.test(m.name))m.emissiveIntensity=.75;
    if(m.name==='Rose silk / painted vertex gradient'){m.roughness=.65;if(m instanceof MeshPhysicalMaterial)m.specularIntensity=.35;}
    if(m.name==='Amber iris gradient'){
     m.roughness=.48;if(m instanceof MeshPhysicalMaterial){m.clearcoat=.12;m.clearcoatRoughness=.35;}
     m.onBeforeCompile=shader=>{shader.fragmentShader=shader.fragmentShader.replace('#include <emissivemap_fragment>','#include <emissivemap_fragment>\n totalEmissiveRadiance += diffuseColor.rgb * 0.16;');};
     m.customProgramCacheKey=()=> 'desktop-pet-iris-emission-v1';
    }
   }
   return m;
  });
  const names=sources.map(m=>m.name).join('|');
  o.castShadow=!/iris|sclera|reflections|eyelashes|Smile/i.test(names);o.receiveShadow=!/iris|sclera|reflections|eyelashes/i.test(names);
  o.material=Array.isArray(o.material)?materials:materials[0];
 });
 return {dispose(){for(const [mesh,material]of originals)mesh.material=material;styled.forEach(m=>m.dispose());key.shadow.dispose();scene.remove(lights);}};
}
