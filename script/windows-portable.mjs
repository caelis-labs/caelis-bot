import {createHash} from 'node:crypto';
import {readFileSync,writeFileSync,statSync} from 'node:fs';
import {join} from 'node:path';
import {execFileSync} from 'node:child_process';
import {installerInventory} from './windows-installer.mjs';
const [directory,identity,version,output]=process.argv.slice(2);
if(!['dev.caelis.bot.preview','dev.caelis.bot.dev'].includes(identity))throw Error('Unsigned ZIP is preview/development only');
const inventory=installerInventory(directory);
if(inventory.files.includes('portable.json')||inventory.files.includes('installation.ini')||inventory.files.includes('Uninstall.exe'))throw Error('Mixed package ownership');
writeFileSync(join(directory,'portable.json'),JSON.stringify({schema:1,identity,version,files:inventory.files.map(name=>{const bytes=readFileSync(join(directory,name));return {name,sha256:createHash('sha256').update(bytes).digest('hex'),length:bytes.length};})},null,2)+'\n');
// PowerShell uses literal arguments through a short fixed script; no interpolated
// archive paths enter shell source. Zip entries have a flat product root.
execFileSync('pwsh.exe',['-NoProfile','-NonInteractive','-File','script/windows-zip-archive.ps1',directory,output],{stdio:'inherit'});
