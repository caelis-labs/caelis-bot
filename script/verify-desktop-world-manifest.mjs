import assert from 'node:assert/strict';
import {readFileSync,readdirSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
const [directory,binaryJSON]=process.argv.slice(2).filter(arg=>arg!=='--bundled');
const root=resolve(fileURLToPath(new URL('..',import.meta.url)));
const pinned=JSON.parse(readFileSync(resolve(root,'resources/desktop-world/release.json'),'utf8'));
if(process.argv.includes('--bundled')) {
 assert.deepEqual(readdirSync(directory).sort(),['NOTICE','bin','manifest.json'],'unexpected helper payload');
 assert.deepEqual(readdirSync(resolve(directory,'bin')),['desktop-world'],'only the native helper is bundled');
}
const manifest=JSON.parse(readFileSync(resolve(directory,'manifest.json'),'utf8'));
for (const [key,value] of Object.entries({version:pinned.version,'vcs.revision':pinned.revision,'vcs.modified':'false',os:'darwin',arch:'arm64',protocol:'desktop-world/helper-v0.1',host_control:'desktop-world/host-control-v0.1',minimum_macos:pinned.minimum_macos,license:'no-open-source-license-granted'})) assert.equal(manifest[key],value,key);
assert.match(readFileSync(resolve(directory,'NOTICE'),'utf8'),/No open-source license is granted/);

if(binaryJSON) {
 const actual=JSON.parse(binaryJSON);
 for(const key of ['version','vcs.revision','vcs.modified','os','arch','protocol','host_control']) assert.equal(actual[key],manifest[key],`binary ${key}`);
}
assert.match(readFileSync(resolve(root,'go.mod'),'utf8'),new RegExp(`github.com/caelis-labs/desktop-world ${pinned.version.replaceAll('.','\\.')}\\s`));
