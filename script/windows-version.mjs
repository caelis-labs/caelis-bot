import {readFileSync} from 'node:fs';
import {pathToFileURL} from 'node:url';

// Windows qualification advances independently; never relax macOS tag parsing.
export function windowsVersion(base,channel,tag='',revision=1){
 if(!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(base))throw Error('Invalid product base version');
 if(!['development','preview'].includes(channel))throw Error('Unsigned Windows builds are development/preview only');
 if(!Number.isSafeInteger(revision)||revision<1)throw Error('Invalid Windows preview revision');
 if(channel==='development'){
  if(tag)throw Error('Development builds cannot publish a release tag');
  return {version:`${base}-dev`,channel:'development',identity:'development',releaseUpdates:false};
 }
 const version=`${base}-windows.preview.${revision}`;
 if(tag&&tag!==`v${version}`)throw Error('Windows release tag must match product base and preview revision');
 return {version:tag?version:`${version}-local`,channel:'preview',identity:tag?'windows-preview':'development',releaseUpdates:!!tag};
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href){
 const pkg=JSON.parse(readFileSync(new URL('../package.json',import.meta.url),'utf8'));
 console.log(JSON.stringify(windowsVersion(pkg.version,process.argv[2]||'development',process.env.BOT_RELEASE_TAG||'',Number(process.argv[3]||1))));
}
