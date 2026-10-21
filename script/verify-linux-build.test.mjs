import {test} from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {verifyLinuxBuild} from './verify-linux-build.mjs';

test('trimpath Linux executables retain verifiable linked release identity on both architectures',()=>{
 const root=mkdtempSync(join(tmpdir(),'bot-release-elf-')),repo=join(root,'source');
 mkdirSync(repo);
 try{
  for(const dir of ['internal/managedupdate','internal/updates','cmd/launcher'])mkdirSync(join(repo,dir),{recursive:true});
  const files={
   'go.mod':'module github.com/caelis-labs/caelis-bot\n\ngo 1.26.8\n',
   'main.go':'package main\nimport("fmt";"github.com/caelis-labs/caelis-bot/internal/managedupdate";"github.com/caelis-labs/caelis-bot/internal/updates")\nfunc main(){fmt.Println(managedupdate.Source,managedupdate.BuildKind,updates.Version)}\n',
   'internal/managedupdate/identity.go':'package managedupdate\nvar Source="unknown"\nvar BuildKind="development"\n',
   'internal/updates/version.go':'package updates\nvar Version="development"\n',
   'cmd/launcher/main.go':'package main\nfunc main(){}\n',
  };
  for(const [name,body] of Object.entries(files))writeFileSync(join(repo,name),body);
  const git=args=>execFileSync('git',args,{cwd:repo,encoding:'utf8'}).trim();
  git(['init','-q']);git(['config','user.name','Fixture']);git(['config','user.email','fixture@example.invalid']);git(['add','.']);git(['commit','-qm','fixture']);
  const source=git(['rev-parse','HEAD']),version='0.18.0',prefix='github.com/caelis-labs/caelis-bot/';
  for(const arch of ['amd64','arm64']){
   const directory=join(root,arch);mkdirSync(join(directory,'package'),{recursive:true});
   const env={...process.env,GOWORK:'off',CGO_ENABLED:'0',GOOS:'linux',GOARCH:arch};
   const build=(revision=source,releaseVersion=version,kind='release')=>execFileSync('go',['build','-trimpath','-ldflags',`-X ${prefix}internal/managedupdate.Source=${revision} -X ${prefix}internal/managedupdate.BuildKind=${kind} -X ${prefix}internal/updates.Version=${releaseVersion}`,'-o',join(directory,'package/caelisbot'),'.'],{cwd:repo,env});
   build();
   execFileSync('go',['build','-trimpath','-o',join(directory,`caelisbot-launcher-${version}-linux-${arch}`),'./cmd/launcher'],{cwd:repo,env});
   writeFileSync(join(directory,`linux-${arch}-update.json`),JSON.stringify({source,version,buildKind:'release'}));
   const info=execFileSync('go',['version','-m',join(directory,'package/caelisbot')],{encoding:'utf8'});
   assert.doesNotMatch(info,/build\s+-ldflags=/);
   verifyLinuxBuild(directory,arch,source,version);
   for(const [revision,releaseVersion,kind] of [['a'.repeat(40),version,'release'],[source,'9.9.9','release'],[source,version,'qa']]){
    build(revision,releaseVersion,kind);
    assert.throws(()=>verifyLinuxBuild(directory,arch,source,version),/Bot release identity differs/);
   }
  }
 }finally{rmSync(root,{recursive:true,force:true});}
});
