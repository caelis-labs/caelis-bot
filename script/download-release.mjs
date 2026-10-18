import {releaseCommand} from './release-network.mjs';
import {windowsRelease} from './windows-version.mjs';
import {validateTag} from './release-version.mjs';
const [platform,tag,directory='release']=process.argv.slice(2);
if(!['macos','windows'].includes(platform))throw Error('Unknown download platform');
const repo='caelis-labs/caelis-bot';
const release=JSON.parse(releaseCommand('gh',['api',`repos/${repo}/releases/tags/${tag}`]));
const windows=platform==='windows'?windowsRelease(tag):null;
if(release.tag_name!==tag||release.draft||release.prerelease!==(windows?.channel==='preview'))throw Error('Unexpected published release channel');
let names;
if(windows){
 const base=`Caelis-Bot-${windows.version}-windows-amd64-${windows.channel}`;
 names=['-Setup.exe','.zip'].flatMap(s=>['','.sha256','.build.json'].map(sidecar=>base+s+sidecar));
 const metadata=['windows-update.json','windows-update.json.sig'];
 if(metadata.some(name=>release.assets.some(a=>a.name===name)))names.push(...metadata);
}else{
 const version=validateTag(tag),base=`Caelis-Bot-${version}-macos-arm64.dmg`;
 names=[base,base+'.sha256','appcast.xml','latest.json','latest.json.sig'];
}
if(names.some(name=>!release.assets.some(a=>a.name===name)))throw Error('Published platform assets incomplete');
for(const name of names)releaseCommand('gh',['release','download',tag,'--repo',repo,'--dir',directory,'--pattern',name,'--clobber']);
