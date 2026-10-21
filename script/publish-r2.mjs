import {releaseCommand} from './release-network.mjs';
import {r2Client} from './r2-client.mjs';
import {copyFileSync, mkdtempSync, readFileSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {digest, verifyDirectory, validateManifest} from './update-manifest.mjs';
import {validateTag} from './release-version.mjs';
import {publicationKey, validateReceipt} from './publication.mjs';

const prefix = 'caelis-bot/'; // Fixed ownership boundary in the shared Caelis bucket.
const platformFeed = `${prefix}feeds/macos/arm64/stable/`;
export function cleanupKeys(keys, tag) {
  validateTag(tag);
  const owned = /^caelis-bot\/releases\/(v\d+\.\d+\.\d+)\/(Caelis-Bot-\d+\.\d+\.\d+-macos-arm64(?:\.dmg(?:\.sha256)?|-stable\.publication\.json)|latest\.json(?:\.sig)?|appcast\.xml)$/;
  // Preflight the entire listing before deleting anything. Ignore unrelated data.
  const releases = keys.filter(key => key.startsWith(`${prefix}releases/`) && !/\/(windows-amd64|linux-amd64|linux-arm64)\//.test(key));
  for (const key of releases) {
    const match = key.match(owned);
    if (!match || (match[2].startsWith('Caelis-Bot-') && !match[2].startsWith(`Caelis-Bot-${match[1].slice(1)}-`))) {
      throw new Error(`Unexpected object under Bot release prefix: ${key}`);
    }
  }
  return releases.filter(key => compareStable(key.match(owned)[1],tag)<0);
}
export function compareStable(a, b) {
  const parts = tag => validateTag(tag).split('.').map(BigInt);
  const aa=parts(a), bb=parts(b);
  for(let i=0;i<3;i++) { if(aa[i]!==bb[i]) return aa[i]>bb[i]?1:-1; }
  return 0;
}
export function publish(directory, env = process.env, run = (cmd,args) => releaseCommand(cmd,args,{encoding:'utf8',stdio:['ignore','pipe','pipe'],env,maxBuffer:16*1024*1024})) {
  const tag=env.BOT_RELEASE_TAG;
  validateTag(tag);
  if(env.GITHUB_REPOSITORY !== 'caelis-labs/caelis-bot') throw new Error('Unexpected repository');
  const bucket=env.R2_BUCKET || 'caelis-releases', endpoint=new URL(env.R2_ENDPOINT);
  if (!/^[a-z0-9][a-z0-9.-]+$/.test(bucket) || endpoint.protocol !== 'https:' || endpoint.username || endpoint.password) throw new Error('Invalid R2 destination');
  const manifest=verifyDirectory(directory, tag, env.BOT_SPARKLE_PUBLIC_KEY);
  const release=()=>JSON.parse(run('gh',['api',`repos/${env.GITHUB_REPOSITORY}/releases/tags/${tag}`]));
  const current=release();
  if(current.tag_name!==tag || current.draft || current.prerelease) throw new Error('Platform release is not published in its expected channel');
  const commit=JSON.parse(run('gh',['api',`repos/${env.GITHUB_REPOSITORY}/commits/${tag}`]));
  if(commit.sha !== manifest.source) throw new Error('Release source mismatch');
  const r2=(...args)=>run('aws',[...args,'--endpoint-url',endpoint.href]);
  const list=()=>{
    // aws CLI paginates all pages before producing JSON; errors are never an empty list.
    const v=JSON.parse(r2('s3api','list-objects-v2','--bucket',bucket,'--prefix',prefix,'--output','json'));
    return (v.Contents??[]).map(item=>item.Key);
  };
  const temporary=mkdtempSync(join(tmpdir(),'caelis-r2-'));
  try {
    const receiptName=publicationKey(tag,'macos','arm64','stable');
    if(!current.assets?.some(asset=>asset.name===receiptName)) throw new Error('Mac publication receipt missing');
    run('gh',['release','download',tag,'--repo',env.GITHUB_REPOSITORY,'--pattern',receiptName,'--dir',temporary,'--clobber']);
    const receipt=validateReceipt(JSON.parse(readFileSync(join(temporary,receiptName))));
    if(receipt.source!==manifest.source || receipt.assets[0]?.sha256!==manifest.sha256 ||
       receipt.assets.find(a=>a.name==='latest.json')?.sha256!==digest(readFileSync(join(directory,'latest.json'))) ||
       receipt.assets[2]?.sha256!==manifest.appcastSHA256 ||
       receipt.validation!=='developer-id-notarized-stapled-gatekeeper') {
      throw new Error('Mac publication receipt differs from verified feed');
    }
    copyFileSync(join(temporary,receiptName),join(directory,receiptName));
    const existing=list();
    cleanupKeys(existing,tag);
    const pointer=`${prefix}latest.json`;
    if(existing.includes(pointer)) {
      const oldFile=join(temporary,'previous.json');
      r2('s3api','get-object','--bucket',bucket,'--key',pointer,oldFile);
      const old=JSON.parse(readFileSync(oldFile));
      validateManifest(old,old.tag);
      if(compareStable(old.tag,tag)>0) return 'Newer R2 release exists: R2 unchanged';
      if(old.tag===tag && (old.source!==manifest.source || old.sha256!==manifest.sha256 || old.appcastSHA256!==manifest.appcastSHA256)) throw new Error('Refusing to replace published version bytes');
    }
    const versioned=`${prefix}releases/${tag}/`;
    const client=r2Client(directory,temporary,env,run);
    const put=(file,key,type,cache)=>client.put(file,key,type,cache.includes('immutable'),existing);
    const immutable='public, max-age=31536000, immutable', mutable='no-cache, max-age=0, must-revalidate';
    for(const file of [manifest.file,`${manifest.file}.sha256`,'appcast.xml','latest.json','latest.json.sig',receiptName]) {
      put(file,versioned+file,file.endsWith('.dmg')?'application/x-apple-diskimage':'text/plain',immutable);
    }
    // The platform feed is selected from its own signed pointer. GitHub's
    // global latest may represent another platform or a later arrival.
    if(release().draft) throw new Error('Release unpublished during R2 upload');
    for(const [file,type] of [['appcast.xml','application/rss+xml'],['latest.json','application/json'],['latest.json.sig','text/plain']]) {
      put(file,platformFeed+file,type,mutable);
    }
    put('appcast.xml',`${prefix}appcast.xml`,'application/rss+xml',mutable);
    put('latest.json',`${prefix}latest.json`,'application/json',mutable);
    put('latest.json.sig',`${prefix}latest.json.sig`,'text/plain',mutable);
    // Only this platform's older baselines are pruned, after every new object
    // and active pointer has passed readback. GitHub retains historical assets.
    const obsolete=cleanupKeys(list(),tag);
    for(const key of obsolete) r2('s3api','delete-object','--bucket',bucket,'--key',key);
    const remaining=new Set(list());
    if(obsolete.some(key=>remaining.has(key))) throw new Error('R2 retention cleanup not confirmed');
    return `Published ${tag}; retained latest macOS baseline, removed ${obsolete.length} older objects`;
  } finally { rmSync(temporary,{recursive:true,force:true}); }
}
if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href) {
  try { console.log(publish(resolve(process.argv[2] || 'release'))); }
  catch(error) { console.error(error.message); process.exitCode=1; }
}
