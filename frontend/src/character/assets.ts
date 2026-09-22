import pack from '../../../resources/character-pack.json';
const character = pack.characters.find(item => item.id === pack.defaultCharacter)!;
const variant = character.variants.find(item => item.id === pack.defaultVariant)!;
const publicURL = (path: string) => '/' + path.replace(/^frontend\/public\//, '');
export const characterAssets = {
 model: publicURL(variant.model), fallback: publicURL(pack.fallbackModel),
 paperPlane: publicURL(pack.props.paperPlane), avatar: publicURL(pack.branding.avatar),
};
