import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import {pathToFileURL} from 'node:url';

// -trimpath intentionally omits linker flags from Go build info. Read the
// actual linked Go string variables instead, without executing a foreign arch.
export function linkedString(file, symbol) {
 const bytes=readFileSync(file);
 if(bytes.subarray(0,6).toString('hex')!=='7f454c460201')throw Error('Expected little-endian ELF64');
 const symbols=execFileSync('go',['tool','nm',file],{encoding:'utf8',maxBuffer:32*1024*1024});
 const rows=symbols.split('\n').map(line=>line.trim().split(/\s+/)).filter(row=>row[2]===symbol);
 if(rows.length!==1)throw Error('Release identity symbol missing: '+symbol);
 const offset=(address,length)=>{
  const start=Number(bytes.readBigUInt64LE(32)),size=bytes.readUInt16LE(54),count=bytes.readUInt16LE(56);
  for(let i=0;i<count;i++){
   const header=start+i*size;
   if(bytes.readUInt32LE(header)!==1)continue;
   const fileOffset=Number(bytes.readBigUInt64LE(header+8)),base=Number(bytes.readBigUInt64LE(header+16)),lengthOnDisk=Number(bytes.readBigUInt64LE(header+32));
   if(address>=base&&address+length<=base+lengthOnDisk&&fileOffset+address-base+length<=bytes.length)return fileOffset+address-base;
  }
  throw Error('Release identity outside ELF data');
 };
 const header=offset(Number.parseInt(rows[0][0],16),16),address=Number(bytes.readBigUInt64LE(header)),length=Number(bytes.readBigUInt64LE(header+8));
 if(!Number.isSafeInteger(length)||length<1||length>128)throw Error('Invalid release identity string');
 const start=offset(address,length);
 return bytes.subarray(start,start+length).toString('utf8');
}

export function verifyLinuxBuild(directory,arch,source,version){
if(!/^[a-f0-9]{40}$/.test(source)||!['amd64','arm64'].includes(arch))throw Error('Exact source/platform required');
for(const name of ['package/caelisbot',`caelisbot-launcher-${version}-linux-${arch}`]){
 const file=join(directory,name),info=execFileSync('go',['version','-m',file],{encoding:'utf8'});
 if(!info.includes(`vcs.revision=${source}`)||!info.includes('vcs.modified=false')||!info.includes(`GOARCH=${arch}`)||!info.includes('GOOS=linux')||info.includes('managed_update_qa'))throw Error('Release executable provenance differs: '+name);
 if(name==='package/caelisbot')for(const [symbol,value] of Object.entries({'internal/managedupdate.Source':source,'internal/updates.Version':version,'internal/managedupdate.BuildKind':'release'})){
  if(linkedString(file,'github.com/caelis-labs/caelis-bot/'+symbol)!==value)throw Error('Bot release identity differs: '+symbol);
 }
}
const m=JSON.parse(readFileSync(join(directory,`linux-${arch}-update.json`)));
if(m.source!==source||m.version!==version||m.buildKind!=='release')throw Error('Manifest source differs');
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href)verifyLinuxBuild(...process.argv.slice(2));
