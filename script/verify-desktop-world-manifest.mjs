import assert from 'node:assert/strict';
import {readFileSync,readdirSync} from 'node:fs';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
const [directory,binaryJSON]=process.argv.slice(2).filter(arg=>arg!=='--bundled');
const root=resolve(fileURLToPath(new URL('..',import.meta.url)));
const pinned=JSON.parse(readFileSync(resolve(root,'resources/desktop-world/release.json'),'utf8'));
if(process.argv.includes('--bundled')) {
 assert.deepEqual(readdirSync(directory).sort(),['LICENSE','NOTICE','THIRD_PARTY_NOTICES.md','bin','manifest.json','source'],'unexpected helper payload');
 assert.deepEqual(readdirSync(resolve(directory,'bin')),['dtw'],'only the native helper is bundled');
}
const manifest=JSON.parse(readFileSync(resolve(directory,'manifest.json'),'utf8'));
for (const [key,value] of Object.entries({version:pinned.version,'vcs.revision':pinned.revision,'vcs.modified':'false',os:'darwin',arch:'arm64',protocol:'desktop-world/helper-v0.1',host_control:'desktop-world/host-control-v0.1',minimum_macos:pinned.minimum_macos,license:'MPL-2.0',signing:'ad-hoc',notarized:false})) assert.equal(manifest[key],value,key);
assert.match(readFileSync(resolve(directory,'NOTICE'),'utf8'),/Mozilla Public\s+License/);
assert.match(readFileSync(resolve(directory,'LICENSE'),'utf8'),/Mozilla Public License[\s\S]*Version 2\.0/);
assert.match(readFileSync(resolve(directory,'source','go.mod'),'utf8'),/^module github\.com\/caelis-labs\/desktop-world/m);
assert.match(readFileSync(resolve(directory,'source','host','client.go'),'utf8'),/func \(c \*Client\) Declare\(/);
assert.match(readFileSync(resolve(directory,'THIRD_PARTY_NOTICES.md'),'utf8'),/MIT License[\s\S]*Permission is hereby granted[\s\S]*THE SOFTWARE IS PROVIDED/);

if(binaryJSON) {
 const actual=JSON.parse(binaryJSON);
 for(const key of ['version','vcs.revision','vcs.modified','os','arch','protocol','host_control']) assert.equal(actual[key],manifest[key],`binary ${key}`);
}
assert.match(readFileSync(resolve(root,'go.mod'),'utf8'),new RegExp(`github.com/caelis-labs/desktop-world ${pinned.version.replaceAll('.','\\.')}\\s`));
