import assert from 'node:assert/strict';
import {readFileSync,readdirSync} from 'node:fs';
import {createHash} from 'node:crypto';
import {resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {pin,targetPin} from './desktop-world-pin.mjs';
const args=process.argv.slice(2);
const targetAt=args.indexOf('--target');
const target=targetAt<0?'darwin-arm64':args[targetAt+1];
if(targetAt>=0) args.splice(targetAt,2);
const [directory,binaryJSON]=args.filter(arg=>arg!=='--bundled');
const root=resolve(fileURLToPath(new URL('..',import.meta.url)));
const selected=targetPin(target);
const [os,arch]=target.split('-');
if(process.argv.includes('--bundled')) {
 assert.deepEqual(readdirSync(directory).sort(),['LICENSE','NOTICE','THIRD_PARTY_NOTICES.md','bin','manifest.json','source'],'unexpected helper payload');
 assert.deepEqual(readdirSync(resolve(directory,'bin')),[selected.binary.split('/')[1]],'only the native helper is bundled');
}
const manifest=JSON.parse(readFileSync(resolve(directory,'manifest.json'),'utf8'));
const expected={version:pin.version,'vcs.revision':pin.revision,'vcs.modified':'false',os,arch,protocol:'desktop-world/helper-v0.1',host_control:'desktop-world/host-control-v0.1',license:'MPL-2.0',signing:os==='darwin'?'ad-hoc':'unsigned'};
if(os==='darwin') { expected.minimum_macos=selected.minimum_macos; expected.notarized=false; }
for (const [key,value] of Object.entries(expected)) assert.equal(manifest[key],value,key);
const binary=readFileSync(resolve(directory,selected.binary));
const binaryDigest=createHash('sha256').update(binary).digest('hex');
// macOS code signing changes Mach-O bytes after the exact archive and raw
// binary have been verified at staging. The bundled helper is checked with
// codesign, architecture and dependency validation by verify-desktop-world.sh.
if(!(process.argv.includes('--bundled') && os==='darwin')) {
 assert.equal(binaryDigest,selected.binary_sha256,'pinned binary digest');
 if(manifest.binary_sha256) assert.equal(binaryDigest,manifest.binary_sha256,'manifest binary digest');
}
assert.match(readFileSync(resolve(directory,'NOTICE'),'utf8'),/Mozilla Public\s+License/);
assert.match(readFileSync(resolve(directory,'LICENSE'),'utf8'),/Mozilla Public License[\s\S]*Version 2\.0/);
assert.match(readFileSync(resolve(directory,'source','go.mod'),'utf8'),/^module github\.com\/caelis-labs\/desktop-world/m);
assert.match(readFileSync(resolve(directory,'source','host','client.go'),'utf8'),/func \(c \*Client\) Declare\(/);
assert.match(readFileSync(resolve(directory,'THIRD_PARTY_NOTICES.md'),'utf8'),/MIT License[\s\S]*Permission is hereby granted[\s\S]*THE SOFTWARE IS PROVIDED/);

if(binaryJSON) {
 const actual=JSON.parse(binaryJSON);
 for(const key of ['version','vcs.revision','vcs.modified','os','arch','protocol','host_control']) assert.equal(actual[key],manifest[key],`binary ${key}`);
}
assert.match(readFileSync(resolve(root,'go.mod'),'utf8'),new RegExp(`github.com/caelis-labs/desktop-world ${pin.version.replaceAll('.','\\.')}\\s`));
