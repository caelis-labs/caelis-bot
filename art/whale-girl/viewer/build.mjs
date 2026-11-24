// Build an offline page using the exact finished character and the Bot lighting profile.
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';
import { build } from 'vite';
import { readManifest } from '../../../script/asset-pack.mjs';

// With one argument use the embedded edition. The two-argument form opens a
// previously exported Whale Girl package without changing its displayed version.
const [source, output] = process.argv.slice(2);
const destination = output || source;
if (!destination) throw new Error('Expected [Whale Girl .caelispack] output.html');
const local = file => new URL(file,import.meta.url);
let version, model;
if (output) {
  const {unzipSync, strFromU8} = await import('three/addons/libs/fflate.module.js');
  const files = unzipSync(await readFile(source));
  const pack = JSON.parse(strFromU8(files['manifest.json']));
  if (pack.id !== 'caelis.whale-girl') throw new Error('Expected the Whale Girl character pack');
  version = pack.version;
  model = files[pack.characters[0].variants[0].model];
} else {
  const pack = readManifest(fileURLToPath(local('../../../')));
  const variant = pack.characters.find(c => c.id === 'whale-girl').variants.find(v => v.id === 'maid');
  version = variant.version;
  model = await readFile(local('../../../'+variant.model));
}
const artifact = await build({
  configFile: false, publicDir: false,
  build: { write:false, minify:true, lib:{entry:fileURLToPath(local('viewer.js')),name:'WhaleGirlViewer',formats:['iife']} },
});
const [chunk] = artifact[0].output.filter(file => file.type==='chunk');
const [template,style,reference,license,appLicense] = await Promise.all([
  readFile(local('page.html'),'utf8'),readFile(local('style.css'),'utf8'),readFile(local('outfit-detail.webp')),
  readFile(local('../../../node_modules/three/LICENSE'),'utf8'),readFile(local('../../../LICENSE'),'utf8'),
]);
const escapeText = s => s.replaceAll('&','&amp;').replaceAll('<','&lt;');
const page = template.replaceAll('<!-- VERSION -->',version)
  .replace('THIRD_PARTY_NOTICES',() => escapeText('Three.js\n'+license+'\nCaelis Bot\n'+appLicense))
  .replace('INSERT_REFERENCE',() => reference.toString('base64'))
  .replace('/* VIEWER_STYLE */',() => style)
  .replace('INSERT_MODEL',() => gzipSync(model,{level:9}).toString('base64'))
  .replace('/* VIEWER_SCRIPT */',() => chunk.code.replace(/<\/script/gi,'<\\/script'));
await writeFile(destination,page);
