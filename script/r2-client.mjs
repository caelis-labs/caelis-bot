import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import {digest} from './update-manifest.mjs';

export function r2Client(directory, temporary, env, run) {
 const bucket=env.R2_BUCKET||'caelis-releases',endpoint=new URL(env.R2_ENDPOINT);
 if(!/^[a-z0-9][a-z0-9.-]+$/.test(bucket)||endpoint.protocol!=='https:'||endpoint.username||endpoint.password)throw Error('Invalid R2 destination');
 const call=(...args)=>run('aws',[...args,'--endpoint-url',endpoint.href]);
 const list=()=> (JSON.parse(call('s3api','list-objects-v2','--bucket',bucket,'--prefix','caelis-bot/','--output','json')).Contents??[]).map(o=>o.Key);
 const read=key=>{
  const target=join(temporary,'r2-readback');
  call('s3api','get-object','--bucket',bucket,'--key',key,target);
  return readFileSync(target);
 };
 const put=(file,key,type,immutable,existing)=>{
  const hash=digest(readFileSync(join(directory,file)));
  if(immutable&&existing.includes(key)){
   if(digest(read(key))!==hash)throw Error(`Refusing to replace immutable bytes: ${key}`);
   return;
  }
  let failure;
  try{call('s3','cp',join(directory,file),`s3://${bucket}/${key}`,'--content-type',type,'--cache-control',immutable?'public, max-age=31536000, immutable':'no-cache, max-age=0, must-revalidate','--only-show-errors');}catch(error){failure=error;}
  // A lost upload response may have committed. Reconcile this exact key and
  // digest before changing a pointer; never treat an uncertain write as success.
  let bytes;
  try{bytes=read(key);}catch(error){throw failure||error;}
  if(digest(bytes)!==hash)throw failure||Error(`R2 readback mismatch: ${key}`);
 };
 const remove=keys=>{
  for(const key of keys)call('s3api','delete-object','--bucket',bucket,'--key',key);
  const remaining=new Set(list());
  if(keys.some(key=>remaining.has(key)))throw Error('R2 retention cleanup not confirmed');
 };
 return {list,read,put,remove};
}
