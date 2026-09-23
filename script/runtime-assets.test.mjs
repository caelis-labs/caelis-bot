import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,readdirSync} from 'node:fs';
import {readManifest,sha256} from './asset-pack.mjs';
test('production bundle contains exactly the approved models and matching avatar',()=>{
 const pack=readManifest(),models=pack.files.filter(f=>f.path.startsWith('frontend/public/models/'));
 assert.deepEqual(readdirSync('frontend/dist/models').sort(),models.map(f=>f.path.split('/').at(-1)).sort());
 for(const f of pack.files.filter(f=>f.path.startsWith('frontend/public/')))assert.equal(sha256(readFileSync(f.path.replace('frontend/public/','frontend/dist/'))),f.sha256,f.path);
 if(pack.branding.animatedAvatar){
  const svg=pack.files.find(f=>f.path===pack.branding.animatedAvatar);
  const emitted=readdirSync('frontend/dist/assets').filter(name=>/^caelis-avatar-v1-.*\.svg$/.test(name));
  assert.equal(emitted.length,1);assert.equal(sha256(readFileSync(`frontend/dist/assets/${emitted[0]}`)),svg.sha256);
 }
});
