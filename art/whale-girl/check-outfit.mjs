// Verify the removable costume boundary in the actual packaged model.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const bytes=readFileSync(process.argv[2]);
const gltf = JSON.parse(bytes.toString('utf8', 20, 20 + bytes.readUInt32LE(12)));
const meshes = gltf.nodes.filter(n => n.mesh !== undefined);
const garment = meshes.filter(n => n.name.startsWith('maid_'));
const figure = meshes.filter(n => !n.name.startsWith('maid_'));
assert.deepEqual(garment.map(n => n.name).sort(),
  ['maid_apron', 'maid_dress', 'maid_footwear', 'maid_headpiece', 'maid_ink']);
assert.equal(new Set(meshes.map(n => n.skin)).size, 1, 'one shared armature');
assert.equal(gltf.skins.length, 1);
const materials = nodes => new Set(nodes.flatMap(n =>
  gltf.meshes[n.mesh].primitives.map(p => gltf.materials[p.material].name)));
const bodyMaterials = materials(figure);
for (const mat of materials(garment)) {
  assert.ok(!bodyMaterials.has(mat), 'body and costume share '+mat);
}
assert.ok(materials(figure.filter(n => n.name === 'body')).has(process.argv.includes('--continuous')?'Caelis / soft hand skin':'Fitting jersey'));
assert.ok(materials(figure.filter(n => n.name === 'whale_tail')).has('Cel / Indigo whale with blue belly'));
for (const layer of ['maid_apron','maid_dress']) {
  const node = garment.find(n => n.name === layer);
  assert.ok(gltf.meshes[node.mesh].primitives.some(p => p.attributes.COLOR_0 !== undefined),
    layer+' is missing its painted folds');
}
console.log('Whale Girl costume layers, skin, fold paint and material independence passed');
