import { pruneEmptyViewTargets } from './view-correctives';
import { GLTFLoader, type GLTF } from 'three/addons/loaders/GLTFLoader.js';

/** Load a model selected by the host. Character semantics are a separate layer. */
export function loadCharacterModel(url: string, builtInCorrections = true): Promise<GLTF> {
  return new GLTFLoader().loadAsync(url).then(gltf=>{if(builtInCorrections)pruneEmptyViewTargets(gltf.scene);return gltf;});
}
