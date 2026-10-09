import {execFileSync} from 'node:child_process';
import {mkdtempSync, readFileSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {digest, verifyDirectory, validateManifest} from './update-manifest.mjs';
import {validateTag, releaseChannel} from './release-version.mjs';
import {publicationKey, validateReceipt} from './publication.mjs';

const prefix = 'caelis-bot/'; // Fixed ownership boundary in the shared Caelis bucket.
const platformFeed = channel => `${prefix}feeds/macos/arm64/${channel}/`;
export function cleanupKeys(keys, tag) {
  validateTag(tag);
  const owned = /^caelis-bot\/releases\/(v\d+\.\d+\.\d+)\/(Caelis-Bot-\d+\.\d+\.\d+-macos-arm64\.dmg(?:\.sha256)?|latest\.json(?:\.sig)?)$/;
  // Preflight the entire listing before deleting anything. Ignore unrelated data.
  const releases = keys.filter(key => key.startsWith(`${prefix}releases/`) && !/\/windows-amd64\//.test(key));
  for (const key of releases) {
    const match = key.match(owned);
    if (!match || (match[2].startsWith('Caelis-Bot-') && !match[2].startsWith(`Caelis-Bot-${match[1].slice(1)}-`))) {
      throw new Error(`Unexpected object under Bot release prefix: ${key}`);
    }
  }
  return releases.filter(key => !key.startsWith(`${prefix}releases/${tag}/`));
}
export function compareStable(a, b) {
  const parts = tag => validateTag(tag).split('.').map(BigInt);
  const aa=parts(a), bb=parts(b);
  for(let i=0;i<3;i++) { if(aa[i]!==bb[i]) return aa[i]>bb[i]?1:-1; }
  return 0;
}
export function comparePreview(a, b) {
  const versionA=validateTag(a), versionB=validateTag(b);
  const boundaryA=versionA.indexOf('-'), boundaryB=versionB.indexOf('-');
  if(boundaryA<0 || boundaryB<0) throw new Error('Expected preview tags');
  const coreA=versionA.slice(0,boundaryA), suffixA=versionA.slice(boundaryA+1);
  const coreB=versionB.slice(0,boundaryB), suffixB=versionB.slice(boundaryB+1);
  const core=compareStable(`v${coreA}`,`v${coreB}`);
  if(core) return core;
  const aa=suffixA.split('.'), bb=suffixB.split('.');
  for(let i=0;i<Math.max(aa.length,bb.length);i++) {
    if(aa[i]===undefined || bb[i]===undefined) return aa[i]===undefined?-1:1;
    const numericA=/^\d+$/.test(aa[i]), numericB=/^\d+$/.test(bb[i]);
    if(numericA && numericB) {
      const x=BigInt(aa[i]), y=BigInt(bb[i]);
      if(x!==y) return x>y?1:-1;
    } else if(numericA!==numericB) return numericA?-1:1;
    else if(aa[i]!==bb[i]) return aa[i]>bb[i]?1:-1;
  }
  return 0;
}

export function publish(directory, env = process.env, run = (cmd,args) => execFileSync(cmd,args,{encoding:'utf8',stdio:['ignore','pipe','pipe'],env,maxBuffer:16*1024*1024})) {
  const tag=env.BOT_RELEASE_TAG;
  const channel=releaseChannel(tag);
  if(env.GITHUB_REPOSITORY !== 'caelis-labs/caelis-bot') throw new Error('Unexpected repository');
  const bucket=env.R2_BUCKET || 'caelis-releases', endpoint=new URL(env.R2_ENDPOINT);
  if (!/^[a-z0-9][a-z0-9.-]+$/.test(bucket) || endpoint.protocol !== 'https:' || endpoint.username || endpoint.password) throw new Error('Invalid R2 destination');
  const manifest=verifyDirectory(directory, tag, env.BOT_SPARKLE_PUBLIC_KEY);
  const release=()=>JSON.parse(run('gh',['api',`repos/${env.GITHUB_REPOSITORY}/releases/tags/${tag}`]));
  const current=release();
  if(current.tag_name!==tag || current.draft || current.prerelease!==(channel!=='stable')) throw new Error('Platform release is not published in its expected channel');
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
    const receiptName=publicationKey(tag,'macos','arm64',channel);
    if(!current.assets?.some(asset=>asset.name===receiptName)) throw new Error('Mac publication receipt missing');
    run('gh',['release','download',tag,'--repo',env.GITHUB_REPOSITORY,'--pattern',receiptName,'--dir',temporary]);
    const receipt=validateReceipt(JSON.parse(readFileSync(join(temporary,receiptName))));
    if(receipt.source!==manifest.source || receipt.assets[0]?.sha256!==manifest.sha256 ||
       receipt.assets.find(a=>a.name==='latest.json')?.sha256!==digest(readFileSync(join(directory,'latest.json'))) ||
       (channel!=='preview' && receipt.assets[2]?.sha256!==manifest.appcastSHA256) ||
       receipt.validation!=='developer-id-notarized-stapled-gatekeeper') {
      throw new Error('Mac publication receipt differs from verified feed');
    }
    const existing=list();
    if(channel==='stable') cleanupKeys(existing,tag);
    const pointer=channel==='stable'?`${prefix}latest.json`:`${platformFeed(channel)}latest.json`;
    if(existing.includes(pointer)) {
      const oldFile=join(temporary,'previous.json');
      r2('s3api','get-object','--bucket',bucket,'--key',pointer,oldFile);
      const old=JSON.parse(readFileSync(oldFile));
      validateManifest(old,old.tag);
      if((old.channel??'stable')!==channel) throw new Error('R2 channel pointer mismatch');
      if((channel==='stable'?compareStable(old.tag,tag):comparePreview(old.tag,tag))>0) return 'Newer R2 release exists: R2 unchanged';
      if(old.tag===tag && (old.source!==manifest.source || old.sha256!==manifest.sha256 || old.appcastSHA256!==manifest.appcastSHA256)) throw new Error('Refusing to replace published version bytes');
    }
    const versioned=channel==='stable'?`${prefix}releases/${tag}/`:`${prefix}${channel === 'dev' ? 'dev' : 'previews'}/macos/arm64/${tag}/`;
    const put=(file,key,type,cache)=>{
      if(cache.includes('immutable') && existing.includes(key)) {
        const prior=join(temporary,'immutable');
        r2('s3api','get-object','--bucket',bucket,'--key',key,prior);
        if(digest(readFileSync(prior))!==digest(readFileSync(join(directory,file)))) throw new Error(`Refusing to replace immutable bytes: ${key}`);
        return;
      }
      r2('s3','cp',join(directory,file),`s3://${bucket}/${key}`,'--content-type',type,'--cache-control',cache,'--only-show-errors');
      const check=join(temporary,'download');
      r2('s3api','get-object','--bucket',bucket,'--key',key,check);
      if(digest(readFileSync(check))!==digest(readFileSync(join(directory,file)))) throw new Error(`R2 readback mismatch: ${key}`);
    };
    const immutable='public, max-age=31536000, immutable', mutable='no-cache, max-age=0, must-revalidate';
    for(const file of [manifest.file,`${manifest.file}.sha256`,...(channel==='dev'?['appcast.xml']:[]),'latest.json','latest.json.sig']) {
      put(file,versioned+file,file.endsWith('.dmg')?'application/x-apple-diskimage':file==='appcast.xml'?'application/rss+xml':'text/plain',immutable);
    }
    // The platform feed is selected from its own signed pointer. GitHub's
    // global latest may represent another platform or a later arrival.
    if(release().draft) throw new Error('Release unpublished during R2 upload');
    if(channel!=='stable') {
      for(const [file,type] of [['latest.json.sig','text/plain'],['latest.json','application/json'],...(channel==='dev'?[['appcast.xml','application/rss+xml']]:[])] ) {
        put(file,platformFeed(channel)+file,type,mutable);
      }
      return `Published ${tag} to isolated macOS arm64 ${channel} feed; stable aliases unchanged`;
    }
    for(const [file,type] of [['appcast.xml','application/rss+xml'],['latest.json','application/json'],['latest.json.sig','text/plain']]) {
      put(file,platformFeed(channel)+file,type,mutable);
    }
    put('appcast.xml',`${prefix}appcast.xml`,'application/rss+xml',mutable);
    put('latest.json',`${prefix}latest.json`,'application/json',mutable);
    put('latest.json.sig',`${prefix}latest.json.sig`,'text/plain',mutable);
    // A cached older appcast still names its immutable versioned DMG. Retain
    // those URLs; any future retention job needs a separate reviewed cache
    // horizon and this platform's ownership allowlist.
    return `Published ${tag}; immutable macOS versions retained for cached clients`;
  } finally { rmSync(temporary,{recursive:true,force:true}); }
}
if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href) {
  try { console.log(publish(resolve(process.argv[2] || 'release'))); }
  catch(error) { console.error(error.message); process.exitCode=1; }
}
