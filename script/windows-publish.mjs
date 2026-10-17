import {execFileSync} from 'node:child_process';
import {writeFileSync} from 'node:fs';
import {join} from 'node:path';
import {verifyPublication} from './windows-update-manifest.mjs';
const directory=process.argv[2],tag=process.env.WINDOWS_TAG,source=process.env.WINDOWS_SOURCE;
const manifest=verifyPublication(directory,tag,source);
const gh=args=>execFileSync('gh',args,{encoding:'utf8',windowsHide:true});
// Release creation intentionally fails if this tag already has a release;
// re-running must never overwrite previously published bytes.
const notes=join(directory,'windows-release-notes.md');
writeFileSync(notes,`Windows preview ${manifest.version}\n\nUnsigned Setup.exe and portable ZIP. Windows 10 22H2 x64 or later with WebView2; Windows 11 is the primary acceptance system. See the Windows adaptation documentation for tested limits.\n\nThe installer is not Authenticode signed; Windows or organization policy may warn or block it. The automatic updater independently verifies an Ed25519 signature.\n\nSource: ${source}\n`);
gh(['release','create',tag,'--verify-tag','--draft','--prerelease','--latest=false','--title',`Windows preview ${manifest.version}`,'--notes-file',notes]);
const assets=manifest.artifacts.flatMap(a=>[a.file,a.file+'.sha256',a.file+'.build.json']).concat(['windows-update.json','windows-update.json.sig']);
gh(['release','upload',tag,...assets.map(name=>join(directory,name))]);
gh(['release','edit',tag,'--draft=false','--prerelease','--latest=false']);
console.log(`Published Windows prerelease ${tag}`);
