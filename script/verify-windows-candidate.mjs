import {readFileSync,readdirSync,statSync} from 'node:fs';
import {join} from 'node:path';
import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
const [app,releases]=process.argv.slice(2);
const read=path=>JSON.parse(readFileSync(path,'utf8'));
const hash=path=>createHash('sha256').update(readFileSync(path)).digest('hex');
const build=read(join(app,'build.json'));
if(build.platform!=='windows-amd64'||build.identity!=='development'||build.releaseUpdates!==false||!build.version.endsWith('-local')||build.sha256!==hash(join(app,'caelis-bot.exe'))||build.source!==execFileSync('git',['rev-parse','HEAD'],{encoding:'utf8'}).trim())throw Error('Local candidate build identity mismatch');
for(const file of ['caelis-bot.exe','caelis-terminal.exe','Helpers/caelis.exe','Plugins/desktop-world/bin/dtw.exe','Resources/LICENSE','Resources/ASSET-LICENSE.md'])if(!statSync(join(app,file)).isFile())throw Error('Candidate payload missing');
for(const [format,suffix] of [['nsis','-Setup.exe'],['zip','.zip']]){
 const files=readdirSync(releases).filter(file=>file.endsWith(suffix));
 if(files.length!==1)throw Error('Expected exactly one candidate per format');
 const file=join(releases,files[0]),meta=read(file+'.build.json');
 if(meta.version!==build.version||meta.source!==build.source||meta.format!==format||meta.channel!==build.channel||meta.qualified!==false||meta.signed!==false||meta.sha256!==hash(file)||statSync(file).size===0)throw Error('Candidate package identity mismatch');
}
console.log('Local channel EXE and exact Setup/ZIP receipts verified; GUI acceptance remains separate.');
