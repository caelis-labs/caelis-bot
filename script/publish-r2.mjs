import {execFileSync} from 'node:child_process';
import {mkdtempSync, readFileSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {digest, verifyDirectory, validateManifest} from './update-manifest.mjs';
import {validateTag} from './release-version.mjs';

const prefix = 'caelis-bot/'; // Fixed ownership boundary in the shared Caelis bucket.
export function cleanupKeys(keys, tag) {
  validateTag(tag);
  const owned = /^caelis-bot\/releases\/(v\d+\.\d+\.\d+)\/(Caelis-Bot-\d+\.\d+\.\d+-macos-arm64\.dmg(?:\.sha256)?|latest\.json(?:\.sig)?)$/;
  // Preflight the entire listing before deleting anything. Ignore unrelated data.
  const releases = keys.filter(key => key.startsWith(`${prefix}releases/`));
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

export function publish(directory, env = process.env, run = (cmd,args) => execFileSync(cmd,args,{encoding:'utf8',stdio:['ignore','pipe','pipe'],env,maxBuffer:16*1024*1024})) {
  const tag=env.BOT_RELEASE_TAG;
  if(validateTag(tag).includes('-')) return 'Preview release: R2 unchanged';
  if(env.GITHUB_REPOSITORY !== 'caelis-labs/caelis-bot') throw new Error('Unexpected repository');
  const bucket=env.R2_BUCKET || 'caelis-releases', endpoint=new URL(env.R2_ENDPOINT);
  if (!/^[a-z0-9][a-z0-9.-]+$/.test(bucket) || endpoint.protocol !== 'https:' || endpoint.username || endpoint.password) throw new Error('Invalid R2 destination');
  const manifest=verifyDirectory(directory, tag, env.BOT_SPARKLE_PUBLIC_KEY);
  const latest=()=>JSON.parse(run('gh',['api',`repos/${env.GITHUB_REPOSITORY}/releases/latest`]));
  const isLatest=()=>{ const v=latest(); return v.tag_name===tag && !v.draft && !v.prerelease; };
  if(!isLatest()) return 'Not GitHub latest: R2 unchanged';
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
    const existing=list();
    cleanupKeys(existing,tag);
    if(existing.includes(`${prefix}latest.json`)) {
      const oldFile=join(temporary,'previous.json');
      r2('s3api','get-object','--bucket',bucket,'--key',`${prefix}latest.json`,oldFile);
      const old=JSON.parse(readFileSync(oldFile));
      validateManifest(old,old.tag);
      if(compareStable(old.tag,tag)>0) return 'Newer R2 release exists: R2 unchanged';
      if(old.tag===tag && (old.sha256!==manifest.sha256 || old.appcastSHA256!==manifest.appcastSHA256)) throw new Error('Refusing to replace published version bytes');
    }
    const versioned=`${prefix}releases/${tag}/`;
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
    for(const file of [manifest.file,`${manifest.file}.sha256`,'latest.json','latest.json.sig']) {
      put(file,versioned+file,file.endsWith('.dmg')?'application/x-apple-diskimage':'text/plain',immutable);
    }
    // Recheck after uploading. Shared CI concurrency serializes this repository's
    // publishers; an older build finishing later cannot regress the pointer.
    if(!isLatest()) return 'GitHub latest changed: feed and previous release retained';
    put('appcast.xml',`${prefix}appcast.xml`,'application/rss+xml',mutable);
    put('latest.json',`${prefix}latest.json`,'application/json',mutable);
    put('latest.json.sig',`${prefix}latest.json.sig`,'text/plain',mutable);
    // Every upload and readback, including the active signed feed, succeeded.
    for(const key of cleanupKeys(list(),tag)) r2('s3api','delete-object','--bucket',bucket,'--key',key);
    return `Published ${tag}; R2 retains only the latest Bot release`;
  } finally { rmSync(temporary,{recursive:true,force:true}); }
}
if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href) {
  try { console.log(publish(resolve(process.argv[2] || 'release'))); }
  catch(error) { console.error(error.message); process.exitCode=1; }
}
