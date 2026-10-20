import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
const [directory,arch,source,version]=process.argv.slice(2);
if(!/^[a-f0-9]{40}$/.test(source)||!['amd64','arm64'].includes(arch))throw Error('Exact source/platform required');
for(const name of ['package/caelisbot',`caelisbot-launcher-${version}-linux-${arch}`]){
 const file=join(directory,name),info=execFileSync('go',['version','-m',file],{encoding:'utf8'});
 if(!info.includes(`vcs.revision=${source}`)||!info.includes('vcs.modified=false')||!info.includes(`GOARCH=${arch}`)||!info.includes('GOOS=linux')||info.includes('managed_update_qa'))throw Error('Release executable provenance differs: '+name);
 if(name==='package/caelisbot'&&(!info.includes(`managedupdate.Source=${source}`)||!info.includes(`updates.Version=${version}`)||!info.includes('managedupdate.BuildKind=release')))throw Error('Bot release identity missing');
}
const m=JSON.parse(readFileSync(join(directory,`linux-${arch}-update.json`)));
if(m.source!==source||m.version!==version||m.buildKind!=='release')throw Error('Manifest source differs');
