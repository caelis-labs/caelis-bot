// Build real B and explicitly unhealthy BAD from a previously built QA bundle.
import {execFileSync} from 'node:child_process';
import {cpSync,mkdirSync,readFileSync,writeFileSync,chmodSync} from 'node:fs';
import {join} from 'node:path';
import {create} from './linux-update-manifest.mjs';
import {prepareLinux,linuxNames} from './linux-release.mjs';
const [a,b,bad,arch,privateKey,publicKey]=process.argv.slice(2),source=JSON.parse(readFileSync(join(a,`linux-${arch}-update.json`))).source;
for(const [dir,version,failed] of [[b,'0.17.1-qa.2',false],[bad,'0.17.1-qa.3',true]]){
 mkdirSync(dir);cpSync(join(a,'package'),join(dir,'package'),{recursive:true});
 const n=linuxNames(version,arch);
 cpSync(join(a,linuxNames('0.17.1-qa.1',arch).launcher),join(dir,n.launcher));
 const ldflags=`-X github.com/caelis-labs/caelis-bot/internal/updates.Version=${version} -X github.com/caelis-labs/caelis-bot/internal/managedupdate.Source=${source} -X github.com/caelis-labs/caelis-bot/internal/managedupdate.BuildKind=qa`;
 if(failed)writeFileSync(join(dir,'bad.go'),'package main\nimport "os"\nfunc main(){os.Exit(42)}\n');
 execFileSync('go',['build','-tags','managed_update_qa','-trimpath','-ldflags',ldflags,'-o',join(dir,'package/caelisbot'),failed?join(dir,'bad.go'):'.'],{env:{...process.env,CGO_ENABLED:'0',GOOS:'linux',GOARCH:arch},stdio:'inherit'});
 chmodSync(join(dir,'package/caelisbot'),0o755);
 execFileSync('python3',['-c',`import pathlib,tarfile,sys\nr=pathlib.Path(sys.argv[1])\nwith tarfile.open(sys.argv[2],'w:gz',format=tarfile.USTAR_FORMAT) as tar:\n for p in sorted(r.rglob('*')):\n  if p.is_file():\n   i=tar.gettarinfo(str(p),p.relative_to(r).as_posix());i.uid=i.gid=0;i.uname=i.gname=''\n   with p.open('rb') as f:tar.addfile(i,f)`,join(dir,'package'),join(dir,n.archive)]);
 writeFileSync(join(dir,n.manifest),JSON.stringify(create(join(dir,'package'),join(dir,n.archive),version,source,n.platform,'qa'),null,2)+'\n');
 prepareLinux(dir,arch,privateKey,publicKey);
}
