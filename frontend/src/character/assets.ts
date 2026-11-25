import pack from '../../../resources/character-pack.json';
import type { ClipMapping } from './animation';

export type Presentation = ClipMapping & {rig?:'desktop-pet-v1';turn?:number};
export type CharacterSource = {url:string;presentation:Presentation;bundled:boolean};
const publicURL = (path:string) => '/' + path.replace(/^frontend\/public\//, '');
export const defaultCharacterKey = `builtin:${pack.defaultCharacter}`;
export const fallbackCharacter:CharacterSource = {
 url:publicURL(pack.fallbackModel),presentation:{idle:[]},bundled:true,
};
// Older built-in manifests and community packages keep their original clip contract.
export const legacyClips:ClipMapping = {
 idle:['idle'], activities:{working:'working'},
 feedback:{waiting:'attention',attention:'attention',nod:'nod',celebrate:'celebrate'},
};
function shippedPresentation(variant:{capabilities:string[];presentation?:unknown}):Presentation {
 if(variant.presentation)return variant.presentation as Presentation;
 return variant.capabilities.includes('body-v1')?{...legacyClips,rig:'desktop-pet-v1'}:legacyClips;
}
const bundled = new Map<string,CharacterSource>(pack.characters.flatMap(character=>character.variants.map(variant=>{
 const isDefault=character.id===pack.defaultCharacter&&variant.id===pack.defaultVariant;
 return [isDefault?defaultCharacterKey:`builtin:${character.id}/${variant.id}`,{
  url:publicURL(variant.model),presentation:shippedPresentation(variant),bundled:true,
 }];
})));
export const characterAssets = {
 paperPlane:publicURL(pack.props.paperPlane), avatar:publicURL(pack.branding.avatar),
};

/** The native catalog controls selection. The shipped configuration also serves developer previews. */
export function characterSource(key:string,model:string):CharacterSource {
 return bundled.get(key)??{url:model,presentation:legacyClips,bundled:false};
}
