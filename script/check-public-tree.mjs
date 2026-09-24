import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {existsSync,readFileSync,readdirSync} from 'node:fs';
import {readManifest} from './asset-pack.mjs';
const paths=execFileSync('git',['ls-files','--cached','--others','--exclude-standard','-z'],{encoding:'utf8'}).split('\0').filter(p=>p&&existsSync(p));
// These Python files package the macOS product; character authoring remains private.
const packagingPython=new Set(['script/dmg-settings.py','script/verify-dmg-layout.py']);
// Product interaction fixtures are public; character authoring previews are not.
const runtimePreviews=new Set(['frontend/runtime-preview.html','frontend/runtime-settings-preview.html','frontend/i18n-preview.html']);
const forbidden=p=>/^(characters\/|resources\/brand\/|docs\/verification\/|script\/humanoid\/)/.test(p)||/\.(blend\d*|blend\.gz|bundle|fbx|psd|kra)$/i.test(p)||p.startsWith('script/')&&p.endsWith('.py')&&!packagingPython.has(p)||/^frontend\/.*-preview\.html$/.test(p)&&!runtimePreviews.has(p);
assert.deepEqual(paths.filter(forbidden),[],'authoring files must stay in the private asset repository');
const pack=readManifest();
assert.deepEqual(readdirSync('frontend/public/models').sort(),pack.files.filter(f=>f.path.startsWith('frontend/public/models/')).map(f=>f.path.split('/').at(-1)).sort(),'unlisted model');
for(const p of paths.filter(p=>p.endsWith('.md'))){
 const text=readFileSync(p,'utf8');
 assert.ok(!/\]\([^)]*(?:\.\.?\/)*characters\//.test(text),`broken private asset link: ${p}`);
}
console.log('Public tree contains only product code, documentation and approved finished assets.');
