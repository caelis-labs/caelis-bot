import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {existsSync,readFileSync,readdirSync} from 'node:fs';
import {readManifest} from './asset-pack.mjs';
const paths=execFileSync('git',['ls-files','--cached','--others','--exclude-standard','-z'],{encoding:'utf8'}).split('\0').filter(p=>p&&existsSync(p));
// These Python files package/verify the product; character authoring lives under art/.
const packagingPython=new Set(['script/caelis-runtime.py','script/test_caelis_runtime.py','script/dmg-settings.py','script/verify-dmg-layout.py','script/test-managed-update-linux.py','script/test_linux_installer.py']);
// Product interaction fixtures are public; authoring previews live under art/.
const runtimePreviews=new Set(['frontend/runtime-preview.html','frontend/settings-extras-preview.html','frontend/desktop-settings-preview.html','frontend/i18n-preview.html']);
function forbidden(path) {
  if (path.startsWith('art/') && path.endsWith('.blend')) return false;
  return /^(characters\/|resources\/brand\/|docs\/verification\/|script\/humanoid\/)/.test(path) ||
    /\.(blend\d*|blend\.gz|bundle|fbx|psd|kra)$/i.test(path) ||
    (path.startsWith('script/') && path.endsWith('.py') && !packagingPython.has(path)) ||
    (/^frontend\/.*-preview\.html$/.test(path) && !runtimePreviews.has(path));
}
assert.deepEqual(paths.filter(forbidden),[],'maintain editable models under art/');
const retiredDesktop = /^(resources\/computer-use\/|experiments\/desktop-control-axorcist\/|script\/(?:computer-use-runtime|sign-computer-use|verify-computer-use)\.sh$|experiments\/desktop-control\/(?:.*\.mjs|package(?:-lock)?\.json)$)/;
assert.deepEqual(paths.filter(p=>retiredDesktop.test(p)),[],'retired desktop drivers and payloads must not return');
const pack=readManifest();
assert.deepEqual(readdirSync('frontend/public/models').sort(),pack.files.filter(f=>f.path.startsWith('frontend/public/models/')).map(f=>f.path.split('/').at(-1)).sort(),'unlisted model');
for(const p of paths.filter(p=>p.endsWith('.md'))){
 const text=readFileSync(p,'utf8');
 assert.ok(!/\]\([^)]*(?:\.\.?\/)*characters\//.test(text),`obsolete character source link: ${p}`);
}
console.log('Verified authoring paths and finished runtime assets.');
