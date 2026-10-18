import {execFileSync} from 'node:child_process';
import {writeFileSync} from 'node:fs';
import {join,resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {qualify,verifyPublication} from './windows-update-manifest.mjs';
export function publicationPlan(directory,tag,source,publishUpdates=false){
 const manifest=publishUpdates?verifyPublication(directory,tag,source):qualify(directory,tag,source);
 const assets=manifest.artifacts.flatMap(a=>[a.file,a.file+'.sha256',a.file+'.build.json']).concat(publishUpdates?['windows-update.json','windows-update.json.sig']:[]);
 return {manifest,assets};
}
if(process.argv[1]&&import.meta.url===pathToFileURL(resolve(process.argv[1])).href){
const directory=process.argv[2],tag=process.env.WINDOWS_TAG,source=process.env.WINDOWS_SOURCE;
const publishUpdates=process.env.WINDOWS_PUBLISH_UPDATES==='true';
const {manifest,assets}=publicationPlan(directory,tag,source,publishUpdates);
const gh=args=>execFileSync('gh',args,{encoding:'utf8',windowsHide:true});
// Release creation intentionally fails if this tag already has a release;
// re-running must never overwrite previously published bytes.
const notes=join(directory,'windows-release-notes.md');
writeFileSync(notes,`Windows preview ${manifest.version}\n\nUnsigned Setup.exe and portable ZIP. Windows 10 22H2 x64 or later with WebView2; Windows 11 is the primary development target. CI checks, build and package integrity passed; native GUI acceptance is not a publication gate. See the Windows adaptation documentation for tested limits.\n\nThe installer is not Authenticode signed; Windows or organization policy may warn or block it. ${publishUpdates?'The automatic updater independently verifies an Ed25519 signature.':'This release contains installable packages only. It is not offered through automatic updates; no unsigned update manifest is published.'}\n\nSource: ${source}\n`);
gh(['release','create',tag,'--verify-tag','--draft','--prerelease','--latest=false','--title',`Windows preview ${manifest.version}`,'--notes-file',notes]);
gh(['release','upload',tag,...assets.map(name=>join(directory,name))]);
gh(['release','edit',tag,'--draft=false','--prerelease','--latest=false']);
console.log(`Published Windows prerelease ${tag}`);
}
