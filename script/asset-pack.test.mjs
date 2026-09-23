import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,mkdirSync,copyFileSync,writeFileSync,rmSync,symlinkSync,readFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,dirname} from 'node:path';
import {readManifest,validateManifest,verifyPack,importPack,manifestPath} from './asset-pack.mjs';

test('untrusted pack cannot overwrite code, select missing resources or change contract silently',()=>{
 const mutate=f=>{const m=readManifest();f(m);return()=>validateManifest(m);};
 assert.throws(mutate(m=>m.files[0].path='../../package.json'));
 assert.throws(mutate(m=>m.files[0].path='.github/workflows/ci.yml'));
 assert.throws(mutate(m=>m.contractVersion=99));
 assert.throws(mutate(m=>m.defaultVariant='missing'));
 assert.throws(mutate(m=>m.files.push(m.files[0])));
 assert.throws(mutate(m=>m.sourcePath='/private/authoring.blend'));
});
test('verified import is exact, rejects extra source files, symlinks and corruption before modifying product',async()=>{
 const dir=mkdtempSync(join(tmpdir(),'caelis-pack-')),source=join(dir,'release'),dest=join(dir,'product');
 try{
  const m=readManifest();mkdirSync(dest,{recursive:true});
  for(const p of [manifestPath,...m.files.map(f=>f.path)]){mkdirSync(dirname(join(source,p)),{recursive:true});copyFileSync(p,join(source,p));}
  await verifyPack(source,{strict:true});await importPack(source,dest);assert.deepEqual(readManifest(dest),m);
  const original=readFileSync(join(dest,m.files[0].path));
  writeFileSync(join(source,'authoring.blend'),'private');await assert.rejects(importPack(source,dest),/unlisted/);rmSync(join(source,'authoring.blend'));
  const file=join(source,m.files[0].path);rmSync(file);symlinkSync(join(dest,m.files[0].path),file);await assert.rejects(importPack(source,dest),/symlink/);rmSync(file);
  writeFileSync(file,'bad');await assert.rejects(importPack(source,dest),/hash mismatch/);
  assert.deepEqual(readFileSync(join(dest,m.files[0].path)),original);
 }finally{rmSync(dir,{recursive:true,force:true});}
});
